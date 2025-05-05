package ambient

import (
	"fmt"
	"sync"

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

	val, ok := collection.Metadata()[ClusterKRTMetadataKey]
	if !ok {
		panic(fmt.Sprintf("metadata key %s not found in collection %v", ClusterKRTMetadataKey, collection))
	}

	clusterID, ok := val.(cluster.ID)
	if !ok {
		panic(fmt.Sprintf("metadata value %s is not a cluster.ID in collection %v", ClusterKRTMetadataKey, collection))
	}

	if _, exists := c.collections[clusterID]; exists {
		log.Warnf("collection %v already exists for cluster %s", collection, clusterID)
		return false
	}

	c.collections[clusterID] = collection

	return true
}

func NewCollectionCacheByClusterFromMetadata[T any]() *collectionCacheByCluster[T] {
	return &collectionCacheByCluster[T]{
		collections: make(map[cluster.ID]krt.Collection[T]),
	}
}
