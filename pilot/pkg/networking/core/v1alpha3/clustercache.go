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

package v1alpha3

import (
	"sync"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
)

type CDSCache struct {
	mutex       sync.RWMutex
	blackHole   *cluster.Cluster
	passthrough *cluster.Cluster
}

func (c *CDSCache) GetBlackHoleCluster() *cluster.Cluster {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.blackHole
}

func (c *CDSCache) SetBlackHoleCluster(cluster *cluster.Cluster) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.blackHole = cluster
}

func (c *CDSCache) GetPassthroughCluster() *cluster.Cluster {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.passthrough
}

func (c *CDSCache) SetPassthroughCluster(cluster *cluster.Cluster) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.passthrough = cluster
}
