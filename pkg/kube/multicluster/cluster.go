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

package multicluster

import (
	"crypto/sha256"
	"fmt"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

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
)

var _ krt.ResourceNamer = &Cluster{}

const ClusterKRTMetadataKey = "cluster"

// Cluster defines cluster struct
type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	kubeConfigSha [sha256.Size]byte
	// sourceSecret identifies the secret that produced this cluster (for remote clusters).
	sourceSecret types.NamespacedName

	stop chan struct{}
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// initialSyncTimeout is set when RunAndWait timed out
	initialSyncTimeout *atomic.Bool

	syncStatusCallback SyncStatusCallback

	// Action indicates whether this is an Add or Update operation.
	// This allows constructors to behave differently during updates (e.g., defer registration).
	Action ACTION

	// SyncedCh is closed when the cluster has synced (or timed out).
	// This allows components to wait for sync before performing actions like registry swap.
	SyncedCh chan struct{}

	// prevComponent holds the previous component during an update operation.
	// This is set temporarily in clusterUpdated to allow constructors to access the old component
	// for seamless migration (comparing old vs new state).
	// This is only set during component construction and cleared afterwards.
	prevComponent ComponentConstraint

	// remoteClusterCollections holds the KRT collections for remote cluster informers.
	remoteClusterCollections *atomic.Pointer[remoteClusterCollections]
}

// remoteClusterCollections holds per-cluster KRT collections.
type remoteClusterCollections struct {
	namespaces     krt.Collection[*corev1.Namespace]
	pods           krt.Collection[*corev1.Pod]
	services       krt.Collection[*corev1.Service]
	endpointSlices krt.Collection[*discovery.EndpointSlice]
	nodes          krt.Collection[*corev1.Node]
	gateways       krt.Collection[*gatewayv1.Gateway]
}

// Namespaces returns the namespaces collection.
func (c *Cluster) Namespaces() krt.Collection[*corev1.Namespace] {
	return c.remoteClusterCollections.Load().namespaces
}

// Pods returns the pods collection.
func (c *Cluster) Pods() krt.Collection[*corev1.Pod] {
	return c.remoteClusterCollections.Load().pods
}

// Services returns the services collection.
func (c *Cluster) Services() krt.Collection[*corev1.Service] {
	return c.remoteClusterCollections.Load().services
}

// EndpointSlices returns the endpointSlices collection.
func (c *Cluster) EndpointSlices() krt.Collection[*discovery.EndpointSlice] {
	return c.remoteClusterCollections.Load().endpointSlices
}

// Nodes returns the nodes collection.
func (c *Cluster) Nodes() krt.Collection[*corev1.Node] {
	return c.remoteClusterCollections.Load().nodes
}

// Gateways returns the gateways collection.
func (c *Cluster) Gateways() krt.Collection[*gatewayv1.Gateway] {
	return c.remoteClusterCollections.Load().gateways
}

// ResourceName implements krt.ResourceNamer.
func (c *Cluster) ResourceName() string {
	return c.ID.String()
}

// Equals implements krt.Equaler for *Cluster.
// Two clusters are considered equal if they have the same ID and kubeconfig SHA.
// This avoids reflect.DeepEqual which always returns false for structs containing
// non-nil function values (e.g., syncStatusCallback).
func (c *Cluster) Equals(other *Cluster) bool {
	return c.ID == other.ID && c.kubeConfigSha == other.kubeConfigSha
}

// GetStop returns the stop channel for the cluster.
func (c *Cluster) GetStop() <-chan struct{} {
	return c.stop
}

type SyncStatusCallback func(cluster.ID, string)

type ACTION int

const (
	Add ACTION = iota
	Update
)

const (
	SyncStatusSynced  = "synced"
	SyncStatusSyncing = "syncing"
	SyncStatusTimeout = "timeout"
	SyncStatusClosed  = "closed"
)

func (a ACTION) String() string {
	switch a {
	case Add:
		return "Add"
	case Update:
		return "Update"
	}
	return "Unknown"
}

// Run starts the cluster's informers, builds KRT collections, invokes handler callbacks, and waits for caches to sync.
// Once caches are synced, we mark the cluster synced.
// For local/config clusters with pre-existing collections, it simply waits for those collections to sync.
// For remote clusters, it builds new collections, invokes handler callbacks, and manages the make-before-break
// lifecycle via the swap parameter.
// This should be run in a goroutine.
func (c *Cluster) Run(mesh meshwatcher.WatcherCollection, handlers []handler, action ACTION, swap *PendingClusterSwap, debugger *krt.DebugHandler) {
	// Check and see if this is a local cluster with pre-existing collections
	if c.remoteClusterCollections.Load() != nil {
		log.Infof("Configuring cluster %s with existing informers", c.ID)
		syncers := []krt.Syncer{
			c.Namespaces(),
			c.Gateways(),
			c.Services(),
			c.Nodes(),
			c.EndpointSlices(),
			c.Pods(),
		}
		for _, syncer := range syncers {
			if !syncer.WaitUntilSynced(c.stop) {
				log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
				continue
			}
		}
		c.initialSync.Store(true)
		return
	}

	// Ensure previous cluster is cleaned up when this method exits (success, failure, or timeout)
	if swap != nil {
		defer swap.Complete()
	}

	c.reportStatus(SyncStatusSyncing)
	if features.RemoteClusterTimeout > 0 {
		time.AfterFunc(features.RemoteClusterTimeout, func() {
			if c.Closed() {
				log.Debugf("remote cluster %s was stopped before hitting the sync timeout", c.ID)
			}
			if !c.initialSync.Load() {
				log.Errorf("remote cluster %s failed to sync after %v", c.ID, features.RemoteClusterTimeout)
				timeouts.With(clusterLabel.Value(string(c.ID))).Increment()
				// Signal that sync is complete (timed out)
				c.closeSyncedCh()
			}
			c.initialSyncTimeout.Store(true)
			c.reportStatus(SyncStatusTimeout)
		})
	}

	opts := krt.NewOptionsBuilder(c.stop, fmt.Sprintf("cluster[%s]", c.ID), debugger)

	// Build a namespace watcher. This must have no filter, since this is our input to the filter itself.
	// This must be done before we build components, so they can access the filter.
	namespaces := kclient.New[*corev1.Namespace](c.Client)
	// When this cluster stops, clean up the namespace watcher
	go func() {
		<-c.stop
		namespaces.ShutdownHandlers()
	}()
	// This will start a namespace informer and wait for it to be ready.
	filter := filter.NewDiscoveryNamespacesFilter(namespaces, mesh, c.stop)
	kube.SetObjectFilter(c.Client, filter)

	c.remoteClusterCollections.Store(buildClusterCollections(c.Client, c.ID, opts))

	// Invoke handler callbacks (clusterAdded/clusterUpdated)
	syncers := make([]ComponentConstraint, 0, len(handlers))
	for _, h := range handlers {
		switch action {
		case Add:
			syncers = append(syncers, h.clusterAdded(c))
		case Update:
			syncers = append(syncers, h.clusterUpdated(c))
		}
	}

	if !c.Client.RunAndWait(c.stop) {
		log.Warnf("remote cluster %s failed to sync", c.ID)
		// Signal that sync is complete (failed)
		c.closeSyncedCh()
		return
	}
	for _, h := range syncers {
		if !kube.WaitForCacheSync("cluster "+string(c.ID), c.stop, h.HasSynced) {
			log.Warnf("remote cluster %s failed to sync handler", c.ID)
			// Signal that sync is complete (failed)
			c.closeSyncedCh()
			return
		}
	}

	// Also wait for KRT collections to sync
	krtSyncers := []krt.Syncer{
		c.Namespaces(),
		c.Gateways(),
		c.Services(),
		c.Nodes(),
		c.EndpointSlices(),
		c.Pods(),
	}
	for _, syncer := range krtSyncers {
		if !syncer.WaitUntilSynced(c.stop) {
			log.Warnf("remote cluster %s failed to sync KRT collection %v", c.ID, syncer)
			// Signal that sync is complete (failed)
			c.closeSyncedCh()
			return
		}
	}

	c.initialSync.Store(true)
	c.reportStatus(SyncStatusSynced)

	// Signal that sync is complete
	c.closeSyncedCh()
}

// buildClusterCollections creates the standard KRT collections for a cluster.
// This is used for both config and remote clusters to ensure identical collection setup.
func buildClusterCollections(client kube.Client, clusterID cluster.ID, opts krt.OptionsBuilder) *remoteClusterCollections {
	defaultFilter := kclient.Filter{
		ObjectFilter: client.ObjectFilter(),
	}

	Namespaces := krt.NewInformer[*corev1.Namespace](client, opts.With(
		krt.WithName("informer/Namespaces"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)
	Pods := krt.NewFilteredInformer[*corev1.Pod](client, kclient.Filter{
		ObjectFilter:    client.ObjectFilter(),
		ObjectTransform: kube.StripPodUnusedFields,
		FieldSelector:   "status.phase!=Failed",
	}, opts.With(
		krt.WithName("informer/Pods"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	gatewayClient := kclient.NewDelayedInformer[*gatewayv1.Gateway](client, gvr.KubernetesGateway, kubetypes.StandardInformer, defaultFilter)
	Gateways := krt.WrapClient(gatewayClient, opts.With(
		krt.WithName("informer/Gateways"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)
	servicesClient := kclient.NewFiltered[*corev1.Service](client, defaultFilter)
	Services := krt.WrapClient(servicesClient, opts.With(
		krt.WithName("informer/Services"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	Nodes := krt.NewFilteredInformer[*corev1.Node](client, kclient.Filter{
		ObjectFilter:    client.ObjectFilter(),
		ObjectTransform: kube.StripNodeUnusedFields,
	}, opts.With(
		krt.WithName("informer/Nodes"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	EndpointSlices := krt.NewFilteredInformer[*discovery.EndpointSlice](client, kclient.Filter{
		ObjectFilter: client.ObjectFilter(),
	}, opts.With(
		krt.WithName("informer/EndpointSlices"),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	return &remoteClusterCollections{
		namespaces:     Namespaces,
		pods:           Pods,
		services:       Services,
		endpointSlices: EndpointSlices,
		nodes:          Nodes,
		gateways:       Gateways,
	}
}

// closeSyncedCh closes the SyncedCh channel to signal sync completion.
// Safe to call multiple times.
func (c *Cluster) closeSyncedCh() {
	if c.SyncedCh != nil {
		select {
		case <-c.SyncedCh:
			// already closed
		default:
			close(c.SyncedCh)
		}
	}
}

// Stop closes the stop channel, if is safe to be called multi times.
func (c *Cluster) Stop() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
		c.reportStatus(SyncStatusClosed)
	}
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

func (c *Cluster) SyncStatus() string {
	if c.Closed() {
		return SyncStatusClosed
	}
	if c.SyncDidTimeout() {
		return SyncStatusTimeout
	}
	if c.HasSynced() {
		return SyncStatusSynced
	}
	return SyncStatusSyncing
}

func (c *Cluster) reportStatus(status string) {
	if c.syncStatusCallback != nil {
		c.syncStatusCallback(c.ID, status)
	}
}

func (c *Cluster) hasInitialCollections() bool {
	return c.remoteClusterCollections.Load() != nil &&
		c.Namespaces() != nil &&
		c.Gateways() != nil &&
		c.Services() != nil &&
		c.Nodes() != nil &&
		c.EndpointSlices() != nil &&
		c.Pods() != nil
}

// WaitUntilSynced waits for the cluster to be fully synced.
func (c *Cluster) WaitUntilSynced(stop <-chan struct{}) bool {
	if c.HasSynced() {
		return true
	}

	// First wait to confirm all of the collections are assigned
	// and then check if they are synced.
	kube.WaitForCacheSync(fmt.Sprintf("cluster[%s] synced", c.ID), stop, c.hasInitialCollections, c.HasSynced)

	return true
}
