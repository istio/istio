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

package ambient

import (
	"crypto/sha256"
	"fmt"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

var _ krt.ResourceNamer = &Cluster{}

type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	SourceSecret types.NamespacedName

	kubeConfigSha [sha256.Size]byte
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// initialSyncTimeout is set when RunAndWait timed out
	initialSyncTimeout *atomic.Bool
	stop               chan struct{}
	namespaces         krt.Collection[*corev1.Namespace]
	pods               krt.Collection[*corev1.Pod]
	services           krt.Collection[*corev1.Service]
	endpointSlices     krt.Collection[*discovery.EndpointSlice]
	nodes              krt.Collection[*corev1.Node]
	gateways           krt.Collection[*v1beta1.Gateway]

	// xref: https://github.com/istio/istio/pull/56097
	// TODO: Figure out if we really want to keep doing this
	// Note that the impl details of TestSeamlessMigration
	// depend on this, so if we remove it, we'll need to
	// adjust the test.
	configmaps krt.Collection[*corev1.ConfigMap]

	initialized *atomic.Bool
}

func (c *Cluster) ResourceName() string {
	return c.ID.String()
}

func (c *Cluster) Run(mesh meshwatcher.WatcherCollection, debugger *krt.DebugHandler) {
	// Check and see if this is a local cluster or not
	syncers := []krt.Syncer{
		c.namespaces,
		c.gateways,
		c.services,
		c.nodes,
		c.endpointSlices,
		c.pods,
		c.configmaps,
	}

	existingCollection := slices.FindFunc(syncers, func(s krt.Syncer) bool {
		return s != nil
	})

	// There's at least one pre-existing informer, so we can skip the rest of the setup
	if existingCollection != nil {
		c.initialized.Store(true)
		log.Infof("Configuring cluster %s with existing informers", c.ID)
		// Just wait for all syncers to be synced
		for _, syncer := range syncers {
			if !syncer.WaitUntilSynced(c.stop) {
				log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
				continue
			}
		}
		c.initialSync.Store(true)
		return
	}

	// We're about to start modifying the cluster, so we need to lock it
	if features.RemoteClusterTimeout > 0 {
		time.AfterFunc(features.RemoteClusterTimeout, func() {
			if !c.initialSync.Load() {
				log.Errorf("remote cluster %s failed to sync after %v", c.ID, features.RemoteClusterTimeout)
				timeouts.With(clusterLabel.Value(string(c.ID))).Increment()
			}
			c.initialSyncTimeout.Store(true)
		})
	}

	opts := krt.NewOptionsBuilder(c.stop, fmt.Sprintf("ambient/cluster[%s]", c.ID), debugger, krt.Metadata{
		ClusterKRTMetadataKey: c.ID,
	})
	namespaces := kclient.New[*corev1.Namespace](c.Client)
	// This will start a namespace informer and wait for it to be ready. So we must start it in a go routine to avoid blocking.
	filter := filter.NewDiscoveryNamespacesFilter(namespaces, mesh, c.stop)
	kube.SetObjectFilter(c.Client, filter)
	// Register all of the informers before starting the client
	defaultFilter := kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}

	Namespaces := krt.WrapClient(namespaces, opts.WithName("informer/Namespaces")...)
	Pods := krt.NewInformerFiltered[*corev1.Pod](c.Client, kclient.Filter{
		ObjectFilter:    c.Client.ObjectFilter(),
		ObjectTransform: kube.StripPodUnusedFields,
	}, opts.WithName("informer/Pods")...)

	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](c.Client, gvr.KubernetesGateway, kubetypes.StandardInformer, defaultFilter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, opts.WithName("informer/Gateways")...)
	servicesClient := kclient.NewFiltered[*corev1.Service](c.Client, defaultFilter)
	Services := krt.WrapClient[*corev1.Service](servicesClient, opts.WithName("informer/Services")...)

	Nodes := krt.NewInformerFiltered[*corev1.Node](c.Client, kclient.Filter{
		ObjectFilter:    c.Client.ObjectFilter(),
		ObjectTransform: kube.StripNodeUnusedFields,
	}, opts.WithName("informer/Nodes")...)

	EndpointSlices := krt.NewInformerFiltered[*discovery.EndpointSlice](c.Client, kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}, opts.WithName("informer/EndpointSlices")...)

	ConfigMaps := krt.NewInformerFiltered[*corev1.ConfigMap](c.Client, kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}, opts.WithName("informer/ConfigMaps")...)

	if !c.Client.RunAndWait(c.stop) {
		log.Warnf("remote cluster %s failed to sync", c.ID)
		return
	}

	c.namespaces = Namespaces
	c.gateways = Gateways
	c.services = Services
	c.nodes = Nodes
	c.endpointSlices = EndpointSlices
	c.pods = Pods
	c.configmaps = ConfigMaps
	c.initialized.Store(true)

	// Reassign syncers for the check
	syncers = []krt.Syncer{
		c.namespaces,
		c.gateways,
		c.services,
		c.nodes,
		c.endpointSlices,
		c.pods,
	}

	for _, syncer := range syncers {
		if !syncer.WaitUntilSynced(c.stop) {
			log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
			continue
		}
	}

	c.initialSync.Store(true)
}

func (c *Cluster) HasSynced() bool {
	// It could happen when a wrong credential provide, this cluster has no chance to run.
	// In this case, the `initialSyncTimeout` will never be set
	// In order not block istiod start up, check close as well.
	if c.Closed() {
		return true
	}
	return c.initialSync.Load() || c.initialSyncTimeout.Load()
}

func (c *Cluster) Closed() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *Cluster) SyncDidTimeout() bool {
	return !c.initialSync.Load() && c.initialSyncTimeout.Load()
}

// Stop closes the stop channel, if is safe to be called multi times.
func (c *Cluster) Stop() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
}

func (c *Cluster) WaitUntilSynced(stop <-chan struct{}) bool {
	if c.HasSynced() {
		return true
	}
	for !c.initialized.Load() {
		select {
		case <-stop:
			return false
		default:
		}
	}
	// Wait for all syncers to be synced
	for _, syncer := range []krt.Syncer{
		c.namespaces,
		c.gateways,
		c.services,
		c.nodes,
		c.endpointSlices,
		c.pods,
	} {
		if !syncer.WaitUntilSynced(stop) {
			log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
			return false
		}
	}
	return true
}
