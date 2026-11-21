// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gatewaycommon

import (
	"fmt"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"

	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/clustertrustbundle"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
)

func IsClusterTrustBundleRef(group, kind string) bool {
	return group == clustertrustbundle.GVK.Group && kind == clustertrustbundle.GVK.Kind
}

// ClusterTrustBundleOutcome is the result of resolving a ClusterTrustBundle reference.
type ClusterTrustBundleOutcome int

const (
	// ClusterTrustBundleResolved means the bundle exists and contains valid CA certificates.
	ClusterTrustBundleResolved ClusterTrustBundleOutcome = iota
	// ClusterTrustBundleDisabled means support is turned off by the feature flag.
	ClusterTrustBundleDisabled
	// ClusterTrustBundleNotServed means the cluster does not serve the certificates.k8s.io/v1 ClusterTrustBundle API.
	ClusterTrustBundleNotServed
	// ClusterTrustBundleInvalid means the bundle is missing, empty or unparsable.
	ClusterTrustBundleInvalid
	// ClusterTrustBundleUnreadable means the API is served but istiod cannot read bundles, typically for lack of RBAC.
	// Unlike NotServed, the reference itself is the problem, so it is reported as an invalid reference.
	ClusterTrustBundleUnreadable
)

// ClusterTrustBundleResolution leaves status reason selection to the caller.
type ClusterTrustBundleResolution struct {
	Outcome ClusterTrustBundleOutcome
	// CredentialName is the SDS name for the bundle. Only set when Outcome is ClusterTrustBundleResolved.
	CredentialName string
	// Message is a user-facing explanation. Only set when Outcome is not ClusterTrustBundleResolved.
	Message string
}

// Unsupported distinguishes an unsupported kind from an invalid reference.
func (r ClusterTrustBundleResolution) Unsupported() bool {
	return r.Outcome == ClusterTrustBundleDisabled || r.Outcome == ClusterTrustBundleNotServed
}

// ClusterTrustBundleAPIState is what is known about reading ClusterTrustBundles from the cluster.
type ClusterTrustBundleAPIState int

const (
	// ClusterTrustBundleAPINotServed means the cluster does not serve the API, or this has not been determined yet.
	ClusterTrustBundleAPINotServed ClusterTrustBundleAPIState = iota
	// ClusterTrustBundleAPIReady means the API is served and the bundle informer has synced.
	ClusterTrustBundleAPIReady
	// ClusterTrustBundleAPIUnreadable means the API is served but the informer did not sync in time.
	ClusterTrustBundleAPIUnreadable
)

// ClusterTrustBundles resolves references in krt transformations. Its zero value disables support.
type ClusterTrustBundles struct {
	Enabled bool
	// Bundles are the ClusterTrustBundles known to the cluster. Only consulted once APIState is ready: until then the
	// collection may never sync, and fetching from it would block the caller.
	Bundles krt.Collection[*certificatesv1.ClusterTrustBundle]
	// APIState is a krt input so that anything resolved before the API was usable is recomputed when it becomes so.
	APIState krt.Singleton[ClusterTrustBundleAPIState]
}

// NewClusterTrustBundles waits for discovery and informer sync before resolving references.
// Sync timeouts unblock reconciliation with an error status; a later successful sync recovers automatically.
func NewClusterTrustBundles(kc kube.Client, stop <-chan struct{}, opts krt.OptionsBuilder) ClusterTrustBundles {
	watcher := clustertrustbundle.SharedAPIWatcher(kc, stop)
	informer := clustertrustbundle.NewInformer(kc, watcher)
	state := krt.NewStatic(ptr.Of(ClusterTrustBundleAPINotServed), false, opts.WithName("ClusterTrustBundleAPIState")...)

	// Callbacks all run before the watcher reports synced, so after the first probe this channel being closed or not is
	// final until a later poll succeeds.
	availableCh := make(chan struct{})
	if watcher.KnownOrCallback(func(<-chan struct{}) { close(availableCh) }) {
		close(availableCh)
	}
	go func() {
		if !kube.WaitForCacheSync("clustertrustbundle api watcher", stop, watcher.HasSynced) {
			return
		}
		select {
		case <-availableCh:
		default:
			state.MarkSynced()
			select {
			case <-availableCh:
			case <-stop:
				return
			}
		}
		unreadable := false
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			switch watcher.InformerState(informer.HasSynced) {
			case clustertrustbundle.InformerSynced:
				if unreadable {
					log.Infof("ClusterTrustBundles are readable again")
				}
				state.Set(ptr.Of(ClusterTrustBundleAPIReady))
				state.MarkSynced()
				return
			case clustertrustbundle.InformerUnreadable:
				if !unreadable {
					unreadable = true
					log.Warnf("%s; ClusterTrustBundle references will be reported as invalid until it can", unreadableMessage)
					state.Set(ptr.Of(ClusterTrustBundleAPIUnreadable))
					state.MarkSynced()
				}
			}
			select {
			case <-t.C:
			case <-stop:
				return
			}
		}
	}()

	return ClusterTrustBundles{
		Enabled:  true,
		Bundles:  krt.WrapClient(informer, opts.WithName("informer/ClusterTrustBundles")...),
		APIState: state,
	}
}

const unreadableMessage = "istiod cannot list/watch clustertrustbundles.certificates.k8s.io (check its RBAC permissions)"

// Resolve looks up a ClusterTrustBundle by name and validates its contents. The bundle is cluster-scoped, so no
// namespace or ReferenceGrant is involved. Everything consulted is registered as a krt dependency of ctx.
func (c ClusterTrustBundles) Resolve(ctx krt.HandlerContext, name string) ClusterTrustBundleResolution {
	if !c.Enabled {
		return ClusterTrustBundleResolution{
			Outcome: ClusterTrustBundleDisabled,
			Message: "ClusterTrustBundle references are disabled by PILOT_ENABLE_GATEWAY_API_CLUSTER_TRUST_BUNDLE",
		}
	}
	// Reading the state first matters: Bundles must not be fetched unless the informer is known to be synced.
	switch st := krt.FetchOne(ctx, c.APIState.AsCollection()); {
	case st == nil || *st == ClusterTrustBundleAPINotServed:
		return ClusterTrustBundleResolution{
			Outcome: ClusterTrustBundleNotServed,
			Message: "the certificates.k8s.io/v1 ClusterTrustBundle API is not served by the cluster",
		}
	case *st == ClusterTrustBundleAPIUnreadable:
		return ClusterTrustBundleResolution{Outcome: ClusterTrustBundleUnreadable, Message: unreadableMessage}
	}
	bundle := ptr.Flatten(krt.FetchOne(ctx, c.Bundles, krt.FilterKey(name)))
	if bundle == nil {
		return ClusterTrustBundleResolution{
			Outcome: ClusterTrustBundleInvalid,
			Message: fmt.Sprintf("ClusterTrustBundle %q not found", name),
		}
	}
	if _, err := clustertrustbundle.ParseTrustBundle(bundle); err != nil {
		return ClusterTrustBundleResolution{Outcome: ClusterTrustBundleInvalid, Message: err.Error()}
	}
	return ClusterTrustBundleResolution{
		Outcome:        ClusterTrustBundleResolved,
		CredentialName: creds.KubernetesClusterTrustBundleTypeURI + name,
	}
}
