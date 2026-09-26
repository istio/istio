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

// ClusterCollections is a restricted, read-only view over one generation of a Cluster: its ID, its
// client, its stop channel and the krt collections built from its informers. It exists so that code
// outside this package can derive krt collections from a cluster's informers without ever holding
// the *Cluster itself.
//
// Deriving a krt collection registers a handler on the source collection, and
// that registration lives until the derived collection is stopped. A remote cluster's collections
// are per-generation — they are discarded when the cluster is removed or its credentials rotate — so
// a derived collection that is not stopped with the cluster keeps its registration, its informer and
// the whole *Cluster alive for the lifetime of the process. Handing out only this view keeps the set
// of places that can make that mistake small enough to audit.
//
// A ClusterCollections is only produced where deriving collections is safe:
//   - in the callbacks of NestedCollectionFromLocalAndRemote and
//     NestedManyCollectionsFromLocalAndRemote, which run inside the cluster's lifecycle and whose
//     results are dropped when the cluster goes away;
//   - from Controller.ConfigCluster, whose cluster lives for the lifetime of the process (its stop
//     channel is the controller's, so passing it to krt.WithStop is a no-op but still correct).
//
// Rules for callers:
//   - Build every derived collection with krt.WithStop(cc.GetStop()), never with a process-wide stop
//     channel.
//   - Do not retain a ClusterCollections, or the collections it returns, past the call that received
//     it. After a credential rotation the same cluster ID is served by a new generation; the old
//     collections are frozen at rotation time, fed by informers that are no longer running.
//   - Use Client() for things that do not create an informer — write clients, object filters, direct
//     API calls.
type ClusterCollections struct {
	cluster *Cluster
}

// ID returns the ID of the cluster.
func (cc ClusterCollections) ID() cluster.ID {
	return cc.cluster.ID
}

// Client returns the cluster's kube client.
//
// Building a *new* informer on it — one for a (resource, filter) combination the cluster does not
// already have — requires starting it yourself:
//
//	inf := kclient.NewFiltered[T](cc.Client(), filter)
//	inf.Start(cc.GetStop())
//	col := krt.WrapClient(inf, krt.WithStop(cc.GetStop()), ...)
//
// Prefer the collection accessors below, which are already started and synced.
func (cc ClusterCollections) Client() kube.Client {
	return cc.cluster.Client
}

// GetStop returns the stop channel for the cluster. Every collection derived from this view must be
// built with krt.WithStop(cc.GetStop()) so it is torn down with the cluster.
func (cc ClusterCollections) GetStop() <-chan struct{} {
	return cc.cluster.stop
}

// Namespaces returns the namespaces collection.
func (cc ClusterCollections) Namespaces() krt.Collection[*corev1.Namespace] {
	return cc.cluster.namespaces()
}

// Pods returns the pods collection.
func (cc ClusterCollections) Pods() krt.Collection[*corev1.Pod] {
	return cc.cluster.pods()
}

// Services returns the services collection.
func (cc ClusterCollections) Services() krt.Collection[*corev1.Service] {
	return cc.cluster.services()
}

// EndpointSlices returns the endpointSlices collection.
func (cc ClusterCollections) EndpointSlices() krt.Collection[*discovery.EndpointSlice] {
	return cc.cluster.endpointSlices()
}

// Nodes returns the nodes collection.
func (cc ClusterCollections) Nodes() krt.Collection[*corev1.Node] {
	return cc.cluster.nodes()
}

// Gateways returns the gateways collection.
func (cc ClusterCollections) Gateways() krt.Collection[*gatewayv1.Gateway] {
	return cc.cluster.gateways()
}

// Cluster defines cluster struct.
//
// A Cluster owns one generation of a remote (or the config) cluster: its client, its informers, its
// stop channel and the krt collections built from those informers. The collections are deliberately
// unexported; code outside this package gets at them through ClusterCollections, which documents the
// rules that keep derived collections from outliving the cluster they read from.
type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	kubeConfigSha [sha256.Size]byte
	// SourceSecret identifies the secret that produced this cluster (for remote clusters).
	SourceSecret types.NamespacedName

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

// namespaces returns the namespaces collection.
func (c *Cluster) namespaces() krt.Collection[*corev1.Namespace] {
	return c.remoteClusterCollections.Load().namespaces
}

// pods returns the pods collection.
func (c *Cluster) pods() krt.Collection[*corev1.Pod] {
	return c.remoteClusterCollections.Load().pods
}

// services returns the services collection.
func (c *Cluster) services() krt.Collection[*corev1.Service] {
	return c.remoteClusterCollections.Load().services
}

// endpointSlices returns the endpointSlices collection.
func (c *Cluster) endpointSlices() krt.Collection[*discovery.EndpointSlice] {
	return c.remoteClusterCollections.Load().endpointSlices
}

// nodes returns the nodes collection.
func (c *Cluster) nodes() krt.Collection[*corev1.Node] {
	return c.remoteClusterCollections.Load().nodes
}

// gateways returns the gateways collection.
func (c *Cluster) gateways() krt.Collection[*gatewayv1.Gateway] {
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
			c.namespaces(),
			c.gateways(),
			c.services(),
			c.nodes(),
			c.endpointSlices(),
			c.pods(),
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
				c.initialSyncTimeout.Store(true)
				c.reportStatus(SyncStatusTimeout)
			}
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
		c.namespaces(),
		c.gateways(),
		c.services(),
		c.nodes(),
		c.endpointSlices(),
		c.pods(),
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
		krt.WithName(fmt.Sprintf("informer/Namespaces[%s]", clusterID)),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)
	Pods := krt.NewFilteredInformer[*corev1.Pod](client, kclient.Filter{
		ObjectFilter:    client.ObjectFilter(),
		ObjectTransform: kube.StripPodUnusedFields,
		FieldSelector:   "status.phase!=Failed",
	}, opts.With(
		krt.WithName(fmt.Sprintf("informer/Pods[%s]", clusterID)),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	gatewayClient := kclient.NewDelayedInformer[*gatewayv1.Gateway](client, gvr.KubernetesGateway, kubetypes.StandardInformer, defaultFilter)
	Gateways := krt.WrapClient(gatewayClient, opts.With(
		krt.WithName(fmt.Sprintf("informer/Gateways[%s]", clusterID)),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)
	servicesClient := kclient.NewFiltered[*corev1.Service](client, defaultFilter)
	Services := krt.WrapClient(servicesClient, opts.With(
		krt.WithName(fmt.Sprintf("informer/Services[%s]", clusterID)),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	Nodes := krt.NewFilteredInformer[*corev1.Node](client, kclient.Filter{
		ObjectFilter:    client.ObjectFilter(),
		ObjectTransform: kube.StripNodeUnusedFields,
	}, opts.With(
		krt.WithName(fmt.Sprintf("informer/Nodes[%s]", clusterID)),
		krt.WithMetadata(krt.Metadata{
			ClusterKRTMetadataKey: clusterID,
		}),
	)...)

	EndpointSlices := krt.NewFilteredInformer[*discovery.EndpointSlice](client, kclient.Filter{
		ObjectFilter: client.ObjectFilter(),
	}, opts.With(
		krt.WithName(fmt.Sprintf("informer/EndpointSlices[%s]", clusterID)),
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
		c.namespaces() != nil &&
		c.gateways() != nil &&
		c.services() != nil &&
		c.nodes() != nil &&
		c.endpointSlices() != nil &&
		c.pods() != nil
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

// WaitUntilInitiallySynced waits for the cluster to be fully synced
// compared to WaitUntilSynced, this method does not return when cluster initial sync has timed out,
// it will only return when the cluster is closed, has synced or the stop channel is closed.
func (c *Cluster) WaitUntilInitiallySynced(stop <-chan struct{}) bool {
	if c.Closed() {
		return true
	}
	if c.initialSync.Load() {
		return true
	}

	// First wait to confirm all of the collections are assigned
	// and then check if they are synced.
	kube.WaitForCacheSync(fmt.Sprintf("cluster[%s] synced", c.ID), stop, func() bool {
		if c.Closed() {
			return true
		}
		if c.initialSync.Load() {
			return true
		}
		return false
	})

	return true
}
