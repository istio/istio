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

package remotecluster

import (
	"sync"

	"istio.io/istio/pkg/cluster"
)

// Store is a mutex-protected collection of clusters
type Store struct {
	sync.RWMutex
	// keyed by secret key(ns/name)->clusterID
	RemoteClusters map[string]map[cluster.ID]*Cluster
}

// NewStore initializes data struct to store clusters information
func NewStore() *Store {
	return &Store{
		RemoteClusters: make(map[string]map[cluster.ID]*Cluster),
	}
}

// Insert adds the given cluster to the store indexed by the secretKey and clusterID
func (c *Store) Insert(secretKey string, clusterID cluster.ID, value *Cluster) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.RemoteClusters[secretKey]; !ok {
		c.RemoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	c.RemoteClusters[secretKey][clusterID] = value
}

func (c *Store) Get(secretKey string, clusterID cluster.ID) *Cluster {
	c.RLock()
	defer c.RUnlock()
	if _, ok := c.RemoteClusters[secretKey]; !ok {
		return nil
	}
	return c.RemoteClusters[secretKey][clusterID]
}

// All returns a copy of the current remote clusters.
func (c *Store) All() map[string]map[cluster.ID]*Cluster {
	if c == nil {
		return nil
	}
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]map[cluster.ID]*Cluster, len(c.RemoteClusters))
	for secret, clusters := range c.RemoteClusters {
		out[secret] = make(map[cluster.ID]*Cluster, len(clusters))
		for cid, c := range clusters {
			outCluster := *c
			out[secret][cid] = &outCluster
		}
	}
	return out
}

// GetExistingClustersFor the given secret
func (c *Store) GetExistingClustersFor(secretKey string) []*Cluster {
	c.RLock()
	defer c.RUnlock()
	out := make([]*Cluster, 0, len(c.RemoteClusters[secretKey]))
	for _, cluster := range c.RemoteClusters[secretKey] {
		out = append(out, cluster)
	}
	return out
}

func (c *Store) Len() int {
	c.Lock()
	defer c.Unlock()
	out := 0
	for _, clusterMap := range c.RemoteClusters {
		out += len(clusterMap)
	}
	return out
}
