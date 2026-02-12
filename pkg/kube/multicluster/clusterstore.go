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
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// ClusterStore is a collection of clusters
type ClusterStore struct {
	sync.RWMutex
	// keyed by secret key(ns/name)->clusterID
	remoteClusters map[string]map[cluster.ID]*Cluster
	clusters       sets.String
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

// newClustersStore initializes data struct to store clusters information
func newClustersStore() *ClusterStore {
	return &ClusterStore{
		remoteClusters: make(map[string]map[cluster.ID]*Cluster),
		clusters:       sets.New[string](),
	}
}

func (c *ClusterStore) Store(secretKey string, clusterID cluster.ID, value *Cluster) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		c.remoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	c.remoteClusters[secretKey][clusterID] = value
	c.clusters.Insert(string(clusterID))
}

// Swap stores a new cluster and returns a PendingClusterSwap that manages
// the lifecycle of both old and new clusters. Call Complete() on the returned swap
// after the new cluster has synced.
func (c *ClusterStore) Swap(secretKey string, clusterID cluster.ID, value *Cluster) *PendingClusterSwap {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		c.remoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	prev := c.remoteClusters[secretKey][clusterID]
	c.remoteClusters[secretKey][clusterID] = value
	c.clusters.Insert(string(clusterID))

	return &PendingClusterSwap{
		clusterID: clusterID,
		prev:      prev,
	}
}

func (c *ClusterStore) Delete(secretKey string, clusterID cluster.ID) {
	c.Lock()
	defer c.Unlock()
	delete(c.remoteClusters[secretKey], clusterID)
	c.clusters.Delete(string(clusterID))
	if len(c.remoteClusters[secretKey]) == 0 {
		delete(c.remoteClusters, secretKey)
	}
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

// All returns a snapshot of the current remote clusters.
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
	c.Lock()
	defer c.Unlock()
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
