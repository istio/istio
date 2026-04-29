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
	"fmt"
	"sync"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
)

// NestedCollectionIndexByCluster creates an index by cluster ID for nested collections.
func NestedCollectionIndexByCluster[T any](
	collection krt.Collection[krt.Collection[T]],
) krt.Index[cluster.ID, krt.Collection[T]] {
	return krt.NewIndex(collection, "cluster", func(col krt.Collection[T]) []cluster.ID {
		val, ok := col.Metadata()[ClusterKRTMetadataKey]
		if !ok {
			panic(fmt.Sprintf("Cluster metadata not set on collection %v", col))
		}
		id, ok := val.(cluster.ID)
		if !ok {
			panic(fmt.Sprintf("Invalid cluster metadata set on collection %v: %v", col, val))
		}
		return []cluster.ID{id}
	})
}

// NestedCollectionFromLocalAndRemote builds a collection of collections that merges
// a local collection with per-cluster remote collections derived from the Controller's
// Clusters() collection.
func NestedCollectionFromLocalAndRemote[T any](
	ctrl *Controller,
	localCollection krt.Collection[T],
	clusterToCollection krt.TransformationSingle[*Cluster, krt.Collection[T]],
	name string,
	opts krt.OptionsBuilder,
) krt.Collection[krt.Collection[T]] {
	return NestedManyCollectionsFromLocalAndRemote(
		ctrl,
		[]krt.Collection[T]{localCollection},
		func(ctx krt.HandlerContext, c *Cluster) []krt.Collection[T] {
			col := clusterToCollection(ctx, c)
			if col == nil {
				return nil
			}
			return []krt.Collection[T]{*col}
		},
		name,
		opts,
	)
}

// NestedManyCollectionsFromLocalAndRemote builds a collection of collections that merges
// multiple local collections with per-cluster remote collections derived from the Controller's
// Clusters() collection. This is a generalization of NestedCollectionFromLocalAndRemote for
// cases where each cluster produces multiple collections instead of one.
func NestedManyCollectionsFromLocalAndRemote[T any](
	ctrl *Controller,
	localCollections []krt.Collection[T],
	clusterToCollections func(krt.HandlerContext, *Cluster) []krt.Collection[T],
	name string,
	opts krt.OptionsBuilder,
) krt.Collection[krt.Collection[T]] {
	clustersCollection := ctrl.Clusters()
	globalCollection := krt.NewStaticCollection(
		localCollections[0],
		localCollections,
		opts.WithName("Global"+name)...,
	)
	cache := &collectionCacheByClusterMany[T]{
		collections: make(map[cluster.ID][]krt.Collection[T]),
	}
	clustersCollection.Register(func(e krt.Event[*Cluster]) {
		if e.Event != controllers.EventDelete {
			return
		}
		old := ptr.Flatten(e.Old)
		if !cache.Remove(old.ID) {
			log.Debugf("clusterID %s doesn't exist in cache %v. Removal is a no-op", old.ID, cache)
		}
	})
	remoteCollections := krt.NewManyCollection(clustersCollection, func(ctx krt.HandlerContext, c *Cluster) []krt.Collection[T] {
		if existing := cache.Get(c.ID); existing != nil {
			return existing
		}
		cols := clusterToCollections(ctx, c)
		if cols == nil {
			log.Warnf("no collections for %s returned for cluster %v", name, c.ID)
			return nil
		}
		cache.Insert(c.ID, cols)
		return cols
	}, opts.WithName("Remote"+name)...)

	remoteCollections.RegisterBatch(func(o []krt.Event[krt.Collection[T]]) {
		for _, e := range o {
			l := e.Latest()
			switch e.Event {
			case controllers.EventAdd, controllers.EventUpdate:
				globalCollection.UpdateObject(l)
			case controllers.EventDelete:
				globalCollection.DeleteObject(krt.GetKey(l))
			}
		}
	}, true)
	return globalCollection
}

// collectionCacheByClusterMany is a thread-safe cache of slices of krt collections keyed by cluster ID.
type collectionCacheByClusterMany[T any] struct {
	collections map[cluster.ID][]krt.Collection[T]
	sync.RWMutex
}

func (c *collectionCacheByClusterMany[T]) Get(clusterID cluster.ID) []krt.Collection[T] {
	c.RLock()
	defer c.RUnlock()
	return c.collections[clusterID]
}

func (c *collectionCacheByClusterMany[T]) Insert(clusterID cluster.ID, cols []krt.Collection[T]) {
	c.Lock()
	defer c.Unlock()
	c.collections[clusterID] = cols
}

func (c *collectionCacheByClusterMany[T]) Remove(clusterID cluster.ID) bool {
	c.Lock()
	defer c.Unlock()
	if _, exists := c.collections[clusterID]; !exists {
		return false
	}
	delete(c.collections, clusterID)
	return true
}
