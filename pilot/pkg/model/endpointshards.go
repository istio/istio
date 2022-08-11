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

package model

import (
	"fmt"
	"sort"
	"sync"

	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// shardRegistry is a simplified interface for registries that can produce a shard key
type shardRegistry interface {
	Cluster() cluster.ID
	Provider() provider.ID
}

// ShardKeyFromRegistry computes the shard key based on provider type and cluster id.
func ShardKeyFromRegistry(instance shardRegistry) ShardKey {
	return ShardKey{Cluster: instance.Cluster(), Provider: instance.Provider()}
}

// ShardKey is the key for EndpointShards made of a key with the format "cluster/provider"
type ShardKey struct {
	Cluster  cluster.ID
	Provider provider.ID
}

func (sk ShardKey) String() string {
	return fmt.Sprintf("%s/%s", sk.Provider, sk.Cluster)
}

// MarshalText implements the TextMarshaler interface (for json key usage)
func (sk ShardKey) MarshalText() (text []byte, err error) {
	return []byte(sk.String()), nil
}

// EndpointShards holds the set of endpoint shards of a service. Registries update
// individual shards incrementally. The shards are aggregated and split into
// clusters when a push for the specific cluster is needed.
type EndpointShards struct {
	// mutex protecting below map.
	sync.RWMutex

	// Shards is used to track the shards. EDS updates are grouped by shard.
	// Current implementation uses the registry name as key - in multicluster this is the
	// name of the k8s cluster, derived from the config (secret).
	Shards map[ShardKey][]*IstioEndpoint

	// ServiceAccounts has the concatenation of all service accounts seen so far in endpoints.
	// This is updated on push, based on shards. If the previous list is different than
	// current list, a full push will be forced, to trigger a secure naming update.
	// Due to the larger time, it is still possible that connection errors will occur while
	// CDS is updated.
	ServiceAccounts sets.Set
}

// Keys gives a sorted list of keys for EndpointShards.Shards.
// Calls to Keys should be guarded with a lock on the EndpointShards.
func (es *EndpointShards) Keys() []ShardKey {
	// len(shards) ~= number of remote clusters which isn't too large, doing this sort frequently
	// shouldn't be too problematic. If it becomes an issue we can cache it in the EndpointShards struct.
	keys := make([]ShardKey, 0, len(es.Shards))
	for k := range es.Shards {
		keys = append(keys, k)
	}
	if len(keys) >= 2 {
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Provider == keys[j].Provider {
				return keys[i].Cluster < keys[j].Cluster
			}
			return keys[i].Provider < keys[j].Provider
		})
	}
	return keys
}

// EndpointIndex is a mutex protected index of endpoint shards
type EndpointIndex struct {
	mu sync.RWMutex
	// keyed by svc then ns
	shardsBySvc map[string]map[string]*EndpointShards
	// We'll need to clear the cache in-sync with endpoint shards modifications.
	cache XdsCache
}

func NewEndpointIndex() *EndpointIndex {
	return &EndpointIndex{
		shardsBySvc: make(map[string]map[string]*EndpointShards),
	}
}

func (e *EndpointIndex) SetCache(cache XdsCache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = cache
}

// must be called with lock
func (e *EndpointIndex) clearCacheForService(svc, ns string) {
	if e.cache == nil {
		return
	}
	e.cache.Clear(map[ConfigKey]struct{}{{
		Kind:      kind.ServiceEntry,
		Name:      svc,
		Namespace: ns,
	}: {}})
}

// Shardz returns a copy of the global map of shards but does NOT copy the underlying individual EndpointShards.
func (e *EndpointIndex) Shardz() map[string]map[string]*EndpointShards {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]map[string]*EndpointShards, len(e.shardsBySvc))
	for svcKey, v := range e.shardsBySvc {
		out[svcKey] = make(map[string]*EndpointShards, len(v))
		for nsKey, v := range v {
			out[svcKey][nsKey] = v
		}
	}
	return out
}

// ShardsForService returns the shards and true if they are found, or returns nil, false.
func (e *EndpointIndex) ShardsForService(serviceName, namespace string) (*EndpointShards, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	byNs, ok := e.shardsBySvc[serviceName]
	if !ok {
		return nil, false
	}
	shards, ok := byNs[namespace]
	return shards, ok
}

// GetOrCreateEndpointShard returns the shards. The second return parameter will be true if this service was seen
// for the first time.
func (e *EndpointIndex) GetOrCreateEndpointShard(serviceName, namespace string) (*EndpointShards, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.shardsBySvc[serviceName]; !exists {
		e.shardsBySvc[serviceName] = map[string]*EndpointShards{}
	}
	if ep, exists := e.shardsBySvc[serviceName][namespace]; exists {
		return ep, false
	}
	// This endpoint is for a service that was not previously loaded.
	ep := &EndpointShards{
		Shards:          map[ShardKey][]*IstioEndpoint{},
		ServiceAccounts: sets.Set{},
	}
	e.shardsBySvc[serviceName][namespace] = ep
	// Clear the cache here to avoid race in cache writes.
	e.clearCacheForService(serviceName, namespace)
	return ep, true
}

func (e *EndpointIndex) DeleteServiceShard(shard ShardKey, serviceName, namespace string, preserveKeys bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deleteServiceInner(shard, serviceName, namespace, preserveKeys)
}

func (e *EndpointIndex) DeleteShard(shardKey ShardKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for svc, shardsByNamespace := range e.shardsBySvc {
		for ns := range shardsByNamespace {
			e.deleteServiceInner(shardKey, svc, ns, false)
		}
	}
	e.cache.ClearAll()
}

// must be called with lock
func (e *EndpointIndex) deleteServiceInner(shard ShardKey, serviceName, namespace string, preserveKeys bool) {
	if e.shardsBySvc[serviceName] == nil ||
		e.shardsBySvc[serviceName][namespace] == nil {
		return
	}
	epShards := e.shardsBySvc[serviceName][namespace]
	epShards.Lock()
	delete(epShards.Shards, shard)
	epShards.ServiceAccounts = sets.Set{}
	for _, shard := range epShards.Shards {
		for _, ep := range shard {
			if ep.ServiceAccount != "" {
				epShards.ServiceAccounts.Insert(ep.ServiceAccount)
			}
		}
	}
	// Clear the cache here to avoid race in cache writes.
	e.clearCacheForService(serviceName, namespace)
	if !preserveKeys {
		if len(epShards.Shards) == 0 {
			delete(e.shardsBySvc[serviceName], namespace)
		}
		if len(e.shardsBySvc[serviceName]) == 0 {
			delete(e.shardsBySvc, serviceName)
		}
	}
	epShards.Unlock()
}
