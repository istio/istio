// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ambient

import (
	"fmt"
	"sync"

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
)

type collectionCacheByCluster[T any] struct {
	collections map[cluster.ID]krt.Collection[T]
	sync.RWMutex
}

func (c *collectionCacheByCluster[T]) Remove(clusterID cluster.ID) bool {
	c.Lock()
	defer c.Unlock()

	if _, exists := c.collections[clusterID]; !exists {
		return false
	}
	delete(c.collections, clusterID)
	return true
}

func (c *collectionCacheByCluster[T]) Get(clusterID cluster.ID) krt.Collection[T] {
	c.RLock()
	defer c.RUnlock()

	collection, exists := c.collections[clusterID]
	if !exists {
		return nil
	}
	return collection
}

func (c *collectionCacheByCluster[T]) Insert(collection krt.Collection[T]) bool {
	c.Lock()
	defer c.Unlock()

	val, ok := collection.Metadata()[multicluster.ClusterKRTMetadataKey]
	if !ok {
		panic(fmt.Sprintf("metadata key %s not found in collection %v", multicluster.ClusterKRTMetadataKey, collection))
	}

	clusterID, ok := val.(cluster.ID)
	if !ok {
		panic(fmt.Sprintf("metadata value %s is not a cluster.ID in collection %v", multicluster.ClusterKRTMetadataKey, collection))
	}

	if _, exists := c.collections[clusterID]; exists {
		log.Warnf("collection %v already exists for cluster %s", collection, clusterID)
		return false
	}

	c.collections[clusterID] = collection
	return true
}

func newCollectionCacheByClusterFromMetadata[T any]() *collectionCacheByCluster[T] {
	return &collectionCacheByCluster[T]{
		collections: make(map[cluster.ID]krt.Collection[T]),
	}
}
