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
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
)

// Cluster defines cluster struct
type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	kubeConfigSha [sha256.Size]byte

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

// Run starts the cluster's informers and waits for caches to sync. Once caches are synced, we mark the cluster synced.
// This should be called after each of the handlers have registered informers, and should be run in a goroutine.
// The swap parameter manages the make-before-break lifecycle - its Complete() method is called via defer
// to clean up the previous cluster after sync completes (success, failure, or timeout).
func (c *Cluster) Run(mesh mesh.Watcher, handlers []handler, action ACTION, swap *PendingClusterSwap) {
	// Ensure previous cluster is cleaned up when this method exits (success, failure, or timeout)
	defer swap.Complete()

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

	// Build a namespace watcher. This must have no filter, since this is our input to the filter itself.
	// This must be done before we build components, so they can access the filter.
	namespaces := kclient.New[*corev1.Namespace](c.Client)
	// When this cluster stops, clean up the namespace watcher
	go func() {
		<-c.stop
		namespaces.ShutdownHandlers()
	}()
	// This will start a namespace informer and wait for it to be ready. So we must start it in a go routine to avoid blocking.
	filter := filter.NewDiscoveryNamespacesFilter(namespaces, mesh, c.stop)
	kube.SetObjectFilter(c.Client, filter)

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

	c.initialSync.Store(true)
	c.reportStatus(SyncStatusSynced)

	// Signal that sync is complete
	c.closeSyncedCh()
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
