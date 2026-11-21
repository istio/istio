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
	"strings"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/clustertrustbundle"
	"istio.io/istio/pkg/kube/clustertrustbundle/clustertrustbundletest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestIsClusterTrustBundleRef(t *testing.T) {
	cases := []struct {
		group, kind string
		want        bool
	}{
		{"certificates.k8s.io", "ClusterTrustBundle", true},
		{"", "ClusterTrustBundle", false},
		{"certificates.k8s.io", "ConfigMap", false},
		{"", "ConfigMap", false},
		{"certificates.k8s.io", "clustertrustbundle", false},
		{"trust.cert-manager.io", "Bundle", false},
	}
	for _, tt := range cases {
		t.Run(tt.group+"/"+tt.kind, func(t *testing.T) {
			assert.Equal(t, IsClusterTrustBundleRef(tt.group, tt.kind), tt.want)
		})
	}
}

func testBundle(name, data string) *certificatesv1.ClusterTrustBundle {
	return &certificatesv1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       certificatesv1.ClusterTrustBundleSpec{TrustBundle: data},
	}
}

// resolveCollection runs Resolve inside a krt transformation, so that dependencies are tracked the same way as in the controllers.
func resolveCollection(inputs ClusterTrustBundles, opts ...krt.CollectionOption) krt.Collection[resolveResult] {
	names := krt.NewStaticCollection[string](nil, []string{"bundle", "other", "missing", "empty", "garbage"})
	return krt.NewCollection(names, func(ctx krt.HandlerContext, name string) *resolveResult {
		return &resolveResult{Name: name, Resolution: inputs.Resolve(ctx, name)}
	}, opts...)
}

type resolveResult struct {
	Name       string
	Resolution ClusterTrustBundleResolution
}

func (r resolveResult) ResourceName() string { return r.Name }

type neverSynced struct{}

func (neverSynced) WaitUntilSynced(stop <-chan struct{}) bool {
	<-stop
	return false
}

func (neverSynced) HasSynced() bool { return false }

func TestClusterTrustBundlesResolve(t *testing.T) {
	ca := clustertrustbundletest.GenerateCA(t, "ca")
	bundles := []*certificatesv1.ClusterTrustBundle{
		testBundle("bundle", ca),
		testBundle("empty", ""),
		testBundle("garbage", "this is not a certificate"),
	}
	cases := []struct {
		name    string
		enabled bool
		state   ClusterTrustBundleAPIState
		ref     string
		want    ClusterTrustBundleOutcome
		cred    string
		msg     string
	}{
		{
			name: "disabled", enabled: false, state: ClusterTrustBundleAPIReady, ref: "bundle", want: ClusterTrustBundleDisabled,
			msg: "disabled by PILOT_ENABLE_GATEWAY_API_CLUSTER_TRUST_BUNDLE",
		},
		{
			name: "not served", enabled: true, state: ClusterTrustBundleAPINotServed, ref: "bundle", want: ClusterTrustBundleNotServed,
			msg: "certificates.k8s.io/v1 ClusterTrustBundle API is not served by the cluster",
		},
		{
			name: "unreadable", enabled: true, state: ClusterTrustBundleAPIUnreadable, ref: "bundle", want: ClusterTrustBundleUnreadable,
			msg: "istiod cannot list/watch clustertrustbundles.certificates.k8s.io",
		},
		{
			name: "valid", enabled: true, state: ClusterTrustBundleAPIReady, ref: "bundle", want: ClusterTrustBundleResolved,
			cred: "clustertrustbundle://bundle",
		},
		{
			name: "missing", enabled: true, state: ClusterTrustBundleAPIReady, ref: "missing", want: ClusterTrustBundleInvalid,
			msg: `"missing" not found`,
		},
		{name: "empty", enabled: true, state: ClusterTrustBundleAPIReady, ref: "empty", want: ClusterTrustBundleInvalid, msg: "empty"},
		{
			name: "garbage", enabled: true, state: ClusterTrustBundleAPIReady, ref: "garbage", want: ClusterTrustBundleInvalid,
			msg: "valid PEM-encoded CA certificates",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			stop := test.NewStop(t)
			opts := krt.NewOptionsBuilder(stop, "test", nil)
			// Bundles can never sync unless the state is ready, as with an informer that is not permitted to list. Resolve
			// must not touch it then, or the whole transformation would block.
			var synced krt.Syncer
			if tt.state != ClusterTrustBundleAPIReady {
				synced = neverSynced{}
			}
			inputs := ClusterTrustBundles{
				Enabled:  tt.enabled,
				Bundles:  krt.NewStaticCollection(synced, bundles, opts.WithName("bundles")...),
				APIState: krt.NewStatic(ptr.Of(tt.state), true, opts.WithName("state")...),
			}
			res := resolveCollection(inputs, opts.WithName("results")...)
			res.WaitUntilSynced(stop)
			got := res.GetKey(tt.ref)
			if got == nil {
				t.Fatalf("no result for %q", tt.ref)
			}
			assert.Equal(t, got.Resolution.Outcome, tt.want)
			assert.Equal(t, got.Resolution.CredentialName, tt.cred)
			if tt.msg == "" {
				assert.Equal(t, got.Resolution.Message, "")
			} else if !strings.Contains(got.Resolution.Message, tt.msg) {
				t.Fatalf("message %q does not contain %q", got.Resolution.Message, tt.msg)
			}
			assert.Equal(t, got.Resolution.Unsupported(),
				tt.want == ClusterTrustBundleDisabled || tt.want == ClusterTrustBundleNotServed)
		})
	}
}

func TestClusterTrustBundlesResolveIsReactive(t *testing.T) {
	stop := test.NewStop(t)
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	bundles := krt.NewMutableCollection[*certificatesv1.ClusterTrustBundle](nil, nil, opts.WithName("bundles")...)
	available := krt.NewStatic(ptr.Of(ClusterTrustBundleAPINotServed), true, opts.WithName("state")...)
	res := resolveCollection(ClusterTrustBundles{
		Enabled:  true,
		Bundles:  bundles.AsCollection(),
		APIState: available,
	}, opts.WithName("results")...)
	res.WaitUntilSynced(stop)

	outcome := func(name string) func() ClusterTrustBundleOutcome {
		return func() ClusterTrustBundleOutcome {
			r := res.GetKey(name)
			if r == nil {
				return -1
			}
			return r.Resolution.Outcome
		}
	}
	ca := clustertrustbundletest.GenerateCA(t, "ca")

	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleNotServed)
	// The API appearing must recompute results even though no bundle exists.
	available.Set(ptr.Of(ClusterTrustBundleAPIUnreadable))
	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleUnreadable)
	available.Set(ptr.Of(ClusterTrustBundleAPIReady))
	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleInvalid)
	assert.EventuallyEqual(t, outcome("missing"), ClusterTrustBundleInvalid)

	bundles.UpdateObject(testBundle("bundle", ca))
	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleResolved)
	assert.EventuallyEqual(t, outcome("other"), ClusterTrustBundleInvalid)

	bundles.UpdateObject(testBundle("bundle", "broken"))
	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleInvalid)

	bundles.UpdateObject(testBundle("bundle", ca))
	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleResolved)

	bundles.DeleteObject("bundle")
	assert.EventuallyEqual(t, outcome("bundle"), ClusterTrustBundleInvalid)
}

func TestDisabledClusterTrustBundles(t *testing.T) {
	stop := test.NewStop(t)
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	res := resolveCollection(ClusterTrustBundles{}, opts.WithName("results")...)
	res.WaitUntilSynced(stop)
	assert.Equal(t, res.GetKey("bundle").Resolution.Outcome, ClusterTrustBundleDisabled)
}

func TestNewClusterTrustBundlesServedAtStartup(t *testing.T) {
	stop := test.NewStop(t)
	test.SetForTest(t, &clustertrustbundle.PollInterval, 10*time.Millisecond)
	kc := kube.NewFakeClient(testBundle("bundle", clustertrustbundletest.GenerateCA(t, "ca")))
	clustertrustbundletest.SetAPIAvailable(kc)
	in := NewClusterTrustBundles(kc, stop, krt.NewOptionsBuilder(stop, "test", nil))
	kc.RunAndWait(stop)

	// Synced only once the answer is known, so no one computes against a stale "unavailable".
	assert.EventuallyEqual(t, in.APIState.AsCollection().HasSynced, true)
	assert.EventuallyEqual(t, func() bool {
		v := in.APIState.Get()
		return v != nil && *v == ClusterTrustBundleAPIReady
	}, true)
	// Available is only reported after the informer has the data, so a bundle that exists is never seen as missing.
	assert.Equal(t, len(in.Bundles.List()), 1)
	assert.Equal(t, in.Enabled, true)
}
