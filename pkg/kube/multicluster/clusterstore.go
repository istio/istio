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
	"sync"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// ClusterStore is a collection of clusters
type ClusterStore struct {
	sync.RWMutex
	// keyed by secret key(ns/name)->clusterID
	remoteClusters       map[string]map[cluster.ID]*Cluster
	clusters             sets.String
	clustersAwaitingSync sets.Set[cluster.ID]
	// previousClusters holds, per cluster ID, the previous synced cluster during an in-flight
	// update (make-before-break). AllReady keeps serving it until the new cluster has synced,
	// so the cluster is not momentarily dropped from the collection. Guarded by casMu.
	previousClusters map[cluster.ID]*Cluster
	casMu            sync.Mutex
	*krt.RecomputeTrigger
}

// PendingClusterSwap manages the make-before-break swap of a cluster.
// It holds reference to the old cluster and handles cleanup after sync.
type PendingClusterSwap struct {
	clusterID cluster.ID
	prev      *Cluster
}

// Complete should be called after the new cluster has synced (or failed/timed out).
// It stops and cleans up the previous cluster if one exists.
func (p *PendingClusterSwap) Complete() {
	if p.prev != nil {
		log.Infof("stopping previous cluster %s after new cluster synced", p.clusterID)
		p.prev.Stop()
		p.prev.Client.Shutdown()
	}
}

// NewClustersStore initializes data struct to store clusters information
func NewClustersStore() *ClusterStore {
	return &ClusterStore{
		remoteClusters:       make(map[string]map[cluster.ID]*Cluster),
		clusters:             sets.New[string](),
		RecomputeTrigger:     krt.NewRecomputeTrigger(false),
		clustersAwaitingSync: sets.New[cluster.ID](),
		previousClusters:     make(map[cluster.ID]*Cluster),
	}
}

func (c *ClusterStore) Store(secretKey string, clusterID cluster.ID, value *Cluster) {
	c.Lock()
	defer c.Unlock()
	c.casMu.Lock()
	defer c.casMu.Unlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		c.remoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	c.remoteClusters[secretKey][clusterID] = value
	exists := c.clusters.InsertContains(string(clusterID))
	if exists && c.clustersAwaitingSync.Contains(clusterID) {
		// If there was an old version of this cluster that existed and was waiting for sync,
		// we can remove it from the awaiting set since we have a new version now.
		c.clustersAwaitingSync.Delete(clusterID)
	}
	c.TriggerRecomputation()
}

// Swap stores a new cluster and returns a PendingClusterSwap that manages
// the lifecycle of both old and new clusters. Call Complete() on the returned swap
// after the new cluster has synced.
func (c *ClusterStore) Swap(secretKey string, clusterID cluster.ID, value *Cluster) *PendingClusterSwap {
	c.Lock()
	defer c.Unlock()
	c.casMu.Lock()
	defer c.casMu.Unlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		c.remoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	prev := c.remoteClusters[secretKey][clusterID]
	c.remoteClusters[secretKey][clusterID] = value
	exists := c.clusters.InsertContains(string(clusterID))
	if exists && c.clustersAwaitingSync.Contains(clusterID) {
		c.clustersAwaitingSync.Delete(clusterID)
	}
	// If the previous cluster is currently ready, keep serving it via AllReady until the new
	// (not-yet-synced) cluster syncs. This preserves make-before-break at the collection level:
	// otherwise AllReady would drop the cluster (emitting a spurious delete followed by an add once
	// the new cluster syncs) instead of a single update.
	if clusterReady(prev) {
		c.previousClusters[clusterID] = prev
	}
	c.TriggerRecomputation()

	return &PendingClusterSwap{
		clusterID: clusterID,
		prev:      prev,
	}
}

func (c *ClusterStore) Delete(secretKey string, clusterID cluster.ID) {
	c.Lock()
	defer c.Unlock()
	c.casMu.Lock()
	defer c.casMu.Unlock()
	delete(c.remoteClusters[secretKey], clusterID)
	c.clusters.Delete(string(clusterID))
	if c.clustersAwaitingSync.Contains(clusterID) {
		c.clustersAwaitingSync.Delete(clusterID)
	}
	delete(c.previousClusters, clusterID)
	if len(c.remoteClusters[secretKey]) == 0 {
		delete(c.remoteClusters, secretKey)
	}
	c.TriggerRecomputation()
}

func (c *ClusterStore) Get(secretKey string, clusterID cluster.ID) *Cluster {
	c.RLock()
	defer c.RUnlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		return nil
	}
	return c.remoteClusters[secretKey][clusterID]
}

func (c *ClusterStore) Contains(clusterID cluster.ID) bool {
	c.RLock()
	defer c.RUnlock()
	return c.clusters.Contains(string(clusterID))
}

func (c *ClusterStore) GetByID(clusterID cluster.ID) *Cluster {
	c.RLock()
	defer c.RUnlock()
	for _, clusters := range c.remoteClusters {
		c, ok := clusters[clusterID]
		if ok {
			return c
		}
	}
	return nil
}

// AllReady returns a copy of the current remote clusters that are ready (synced and not closed/timed out).
func (c *ClusterStore) AllReady() map[string]map[cluster.ID]*Cluster {
	if c == nil {
		return nil
	}
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]map[cluster.ID]*Cluster)
	for secret, clusters := range c.remoteClusters {
		for cid, cl := range clusters {
			if cl.Closed() {
				log.Warnf("remote cluster %s is closed, omitting it from the clusters collection", cl.ID)
				continue
			}
			// If the cluster has timed out, we don't want to serve it, but we also don't want to drop it entirely.
			// Instead, we wait for it to sync (or fail) and then recompute the collection.
			if cl.SyncDidTimeout() {
				log.Warnf("remote cluster %s is timed out, omitting it from the clusters collection", cl.ID)
				c.triggerRecomputeOnSync(cl)
				// we should also clear the previous cluster if it exists, since the new cluster is not usable and we don't want to keep serving it.
				c.clearPreviousCluster(cid)
				continue
			}
			if !cl.HasSynced() {
				log.Debugf("remote cluster %s registered informers have not been synced up yet. Skipping and will recompute on sync", cl.ID)
				// During an in-flight update, keep serving the previous synced cluster until the new
				// one syncs, so it is not momentarily dropped from the collection (make-before-break).
				if prev := c.getPreviousCluster(cid); clusterReady(prev) {
					log.Debugf("serving previous synced cluster %s while its update syncs", cid)
					outCluster := *prev
					if _, ok := out[secret]; !ok {
						out[secret] = make(map[cluster.ID]*Cluster)
					}
					out[secret][cid] = &outCluster
				}
				c.triggerRecomputeOnSync(cl)
				continue
			}
			// The new cluster has synced; stop tracking the previous cluster we were serving.
			c.clearPreviousCluster(cid)
			outCluster := *cl
			if _, ok := out[secret]; !ok {
				out[secret] = make(map[cluster.ID]*Cluster)
			}
			out[secret][cid] = &outCluster
		}
	}
	return out
}

// All returns a snapshot of the current remote clusters, including those that may not
// be ready for use. In most cases outside of this package, you should use AllReady().
func (c *ClusterStore) All() map[string]map[cluster.ID]*Cluster {
	if c == nil {
		return nil
	}
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]map[cluster.ID]*Cluster, len(c.remoteClusters))
	for secret, clusters := range c.remoteClusters {
		out[secret] = make(map[cluster.ID]*Cluster, len(clusters))
		for cid, c := range clusters {
			outCluster := *c
			out[secret][cid] = &outCluster
		}
	}
	return out
}

// GetExistingClustersFor return existing clusters registered for the given secret
func (c *ClusterStore) GetExistingClustersFor(secretKey string) []*Cluster {
	c.RLock()
	defer c.RUnlock()
	out := make([]*Cluster, 0, len(c.remoteClusters[secretKey]))
	for _, cluster := range c.remoteClusters[secretKey] {
		out = append(out, cluster)
	}
	return out
}

func (c *ClusterStore) Len() int {
	c.RLock()
	defer c.RUnlock()
	out := 0
	for _, clusterMap := range c.remoteClusters {
		out += len(clusterMap)
	}
	return out
}

func (c *ClusterStore) HasSynced() bool {
	c.RLock()
	defer c.RUnlock()
	for _, clusterMap := range c.remoteClusters {
		for _, cl := range clusterMap {
			if !cl.HasSynced() {
				log.Debugf("remote cluster %s registered informers have not been synced up yet", cl.ID)
				return false
			}
		}
	}

	return true
}

// clusterReady reports whether a cluster is currently usable: it is still open
// and it's synced. Used to decide whether the previous cluster can keep serving
// during an in-flight update and allow performing an atomic swap of clusters without
// dropping the cluster from the collection.
func clusterReady(cl *Cluster) bool {
	return cl != nil && !cl.Closed() && cl.HasSynced() && !cl.SyncDidTimeout()
}

// getPreviousCluster returns the previous cluster tracked for an in-flight update, if any.
func (c *ClusterStore) getPreviousCluster(id cluster.ID) *Cluster {
	c.casMu.Lock()
	defer c.casMu.Unlock()
	return c.previousClusters[id]
}

// clearPreviousCluster stops tracking the previous cluster for the given cluster ID.
func (c *ClusterStore) clearPreviousCluster(id cluster.ID) {
	c.casMu.Lock()
	defer c.casMu.Unlock()
	delete(c.previousClusters, id)
}

// triggerRecomputeOnSync sets up a goroutine to wait for the cluster to be fully synced,
// and then triggers a recompute when it is. Callers must pass the cluster directly
// rather than its ID: AllReady holds the store RLock when calling this, and looking
// the cluster up here would attempt a recursive RLock that can deadlock against a
// concurrent writer waiting on the same RWMutex.
func (c *ClusterStore) triggerRecomputeOnSync(cl *Cluster) {
	c.casMu.Lock()
	defer c.casMu.Unlock()
	id := cl.ID
	exists := c.clustersAwaitingSync.InsertContains(id)
	if exists {
		// Already waiting for sync
		log.Debugf("cluster %s is already awaiting sync, not setting up another recompute trigger", id)
		return
	}

	go func() {
		// Wait until the cluster is fully synced. If it's deleted from the store before
		// it's fully synced, this will return because of the stop.
		// Double check to make sure this cluster is still in the store
		// and that it wasn't closed (we don't want to send an event for bad clusters).
		// The GetByID call below runs without holding the caller's RLock, so it does not
		// reintroduce the recursive-lock hazard.
		if cl.WaitUntilInitiallySynced(cl.stop) && !cl.Closed() && c.GetByID(id) != nil {
			log.Debugf("remote cluster %s informers synced, triggering recompute", id)
			// Let dependent krt collections know that this cluster is ready to use
			c.TriggerRecomputation()
		}
		c.casMu.Lock()
		c.clustersAwaitingSync.Delete(id)
		c.casMu.Unlock()
	}()
}
