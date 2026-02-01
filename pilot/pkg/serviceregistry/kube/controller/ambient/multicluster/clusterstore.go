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
	*krt.RecomputeTrigger
}

// NewClustersStore initializes data struct to store clusters information
func NewClustersStore() *ClusterStore {
	return &ClusterStore{
		remoteClusters:       make(map[string]map[cluster.ID]*Cluster),
		clusters:             sets.New[string](),
		RecomputeTrigger:     krt.NewRecomputeTrigger(false),
		clustersAwaitingSync: sets.New[cluster.ID](),
	}
}

func (c *ClusterStore) Store(secretKey string, clusterID cluster.ID, value *Cluster) {
	c.Lock()
	defer c.Unlock()
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

func (c *ClusterStore) Delete(secretKey string, clusterID cluster.ID) {
	c.Lock()
	defer c.Unlock()
	delete(c.remoteClusters[secretKey], clusterID)
	c.clusters.Delete(string(clusterID))
	if c.clustersAwaitingSync.Contains(clusterID) {
		c.clustersAwaitingSync.Delete(clusterID)
	}
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

// All returns a copy of the current remote clusters.
func (c *ClusterStore) AllReady() map[string]map[cluster.ID]*Cluster {
	if c == nil {
		return nil
	}
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]map[cluster.ID]*Cluster)
	for secret, clusters := range c.remoteClusters {
		for cid, cl := range clusters {
			if cl.Closed() || cl.SyncDidTimeout() {
				log.Warnf("remote cluster %s is closed or timed out, omitting it from the clusters collection", cl.ID)
				continue
			}
			if !cl.HasSynced() {
				log.Debugf("remote cluster %s registered informers have not been synced up yet. Skipping and will recompute on sync", cl.ID)
				c.triggerRecomputeOnSyncLocked(cl.ID)
				continue
			}
			outCluster := *cl
			if _, ok := out[secret]; !ok {
				out[secret] = make(map[cluster.ID]*Cluster)
			}
			out[secret][cid] = &outCluster
		}
	}
	return out
}

// All returns a copy of the current remote clusters, including those that may not
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

// triggerRecomputeOnSyncLocked sets up a goroutine to wait for the cluster to be synced,
// and then triggers a recompute when it is. Ensure you hold the lock before calling this.
func (c *ClusterStore) triggerRecomputeOnSyncLocked(id cluster.ID) {
	cluster := c.GetByID(id)
	if cluster == nil {
		log.Debugf("cluster %s not found in store to trigger recompute", id)
		return
	}
	exists := c.clustersAwaitingSync.InsertContains(id)
	if exists {
		// Already waiting for sync
		return
	}

	go func() {
		// Wait until the cluster is synced. If it's deleted from the store before
		// it's fully synced, this will return because of the stop.
		// Double check to make sure this cluster is still in the store
		// and that it wasn't closed/timed out (we don't want to send an event for bad clusters)
		if cluster.WaitUntilSynced(cluster.stop) && !cluster.Closed() && !cluster.SyncDidTimeout() && c.GetByID(id) != nil {
			// Let dependent krt collections know that this cluster is ready to use
			c.TriggerRecomputation()
			// And clean up our tracking set
			c.Lock()
			c.clustersAwaitingSync.Delete(id)
			c.Unlock()
			log.Debugf("remote cluster %s informers synced, triggering recompute", id)
		}
	}()
}
