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

	"istio.io/istio/pilot/pkg/features"
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

// ShardKey is the key for EndpointShards made of a key with the format "provider/cluster"
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
	ServiceAccounts sets.String
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

// CopyEndpoints takes a snapshot of all endpoints. As input, it takes a map of port name to number, to allow it to group
// the results by service port number. This is a bit weird, but lets us efficiently construct the format the caller needs.
func (es *EndpointShards) CopyEndpoints(portMap map[string]int) map[int][]*IstioEndpoint {
	es.RLock()
	defer es.RUnlock()
	res := map[int][]*IstioEndpoint{}
	for _, v := range es.Shards {
		for _, ep := range v {
			portNum, f := portMap[ep.ServicePortName]
			if !f {
				continue
			}
			res[portNum] = append(res[portNum], ep)
		}
	}
	return res
}

func (es *EndpointShards) DeepCopy() *EndpointShards {
	es.RLock()
	defer es.RUnlock()
	res := &EndpointShards{
		Shards:          make(map[ShardKey][]*IstioEndpoint, len(es.Shards)),
		ServiceAccounts: es.ServiceAccounts.Copy(),
	}
	for k, v := range es.Shards {
		res.Shards[k] = make([]*IstioEndpoint, 0, len(v))
		for _, ep := range v {
			res.Shards[k] = append(res.Shards[k], ep.DeepCopy())
		}
	}
	return res
}

// EndpointIndex is a mutex protected index of endpoint shards
type EndpointIndex struct {
	mu sync.RWMutex
	// keyed by svc then ns
	shardsBySvc map[string]map[string]*EndpointShards
	// We'll need to clear the cache in-sync with endpoint shards modifications.
	cache XdsCache
}

func NewEndpointIndex(cache XdsCache) *EndpointIndex {
	return &EndpointIndex{
		shardsBySvc: make(map[string]map[string]*EndpointShards),
		cache:       cache,
	}
}

// must be called with lock
func (e *EndpointIndex) clearCacheForService(svc, ns string) {
	e.cache.Clear(sets.Set[ConfigKey]{{
		Kind:      kind.ServiceEntry,
		Name:      svc,
		Namespace: ns,
	}: {}})
}

// Shardz returns a full deep copy of the global map of shards. This should be used only for testing
// and debugging, as the cloning is expensive.
func (e *EndpointIndex) Shardz() map[string]map[string]*EndpointShards {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]map[string]*EndpointShards, len(e.shardsBySvc))
	for svcKey, v := range e.shardsBySvc {
		out[svcKey] = make(map[string]*EndpointShards, len(v))
		for nsKey, v := range v {
			out[svcKey][nsKey] = v.DeepCopy()
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
		ServiceAccounts: sets.String{},
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
	if e.cache == nil {
		return
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

// PushType is an enumeration that decides what type push we should do when we get EDS update.
type PushType int

const (
	// NoPush does not push any thing.
	NoPush PushType = iota
	// IncrementalPush just pushes endpoints.
	IncrementalPush
	// FullPush triggers full push - typically used for new services.
	FullPush
)

// UpdateServiceEndpoints updates EndpointShards data by clusterID, hostname, IstioEndpoints.
// It also tracks the changes to ServiceAccounts. It returns whether endpoints need to be pushed and
// it also returns if they need to be pushed whether a full push is needed or incremental push is sufficient.
func (e *EndpointIndex) UpdateServiceEndpoints(
	shard ShardKey,
	hostname string,
	namespace string,
	istioEndpoints []*IstioEndpoint,
) PushType {
	if len(istioEndpoints) == 0 {
		// Should delete the service EndpointShards when endpoints become zero to prevent memory leak,
		// but we should not delete the keys from EndpointIndex map - that will trigger
		// unnecessary full push which can become a real problem if a pod is in crashloop and thus endpoints
		// flip flopping between 1 and 0.
		e.DeleteServiceShard(shard, hostname, namespace, true)
		log.Infof("Incremental push, service %s at shard %v has no endpoints", hostname, shard)
		return IncrementalPush
	}

	pushType := IncrementalPush
	// Find endpoint shard for this service, if it is available - otherwise create a new one.
	ep, created := e.GetOrCreateEndpointShard(hostname, namespace)
	// If we create a new endpoint shard, that means we have not seen the service earlier. We should do a full push.
	if created {
		log.Infof("Full push, new service %s/%s", namespace, hostname)
		pushType = FullPush
	}

	ep.Lock()
	defer ep.Unlock()
	newIstioEndpoints := istioEndpoints

	oldIstioEndpoints := ep.Shards[shard]
	needPush := false
	if oldIstioEndpoints == nil {
		// If there are no old endpoints, we should push with incoming endpoints as there is nothing to compare.
		needPush = true
	} else {
		newIstioEndpoints = make([]*IstioEndpoint, 0, len(istioEndpoints))
		// Check if new Endpoints are ready to be pushed. This check
		// will ensure that if a new pod comes with a non ready endpoint,
		// we do not unnecessarily push that config to Envoy.
		// Please note that address is not a unique key. So this may not accurately
		// identify based on health status and push too many times - which is ok since its an optimization.
		emap := make(map[string]*IstioEndpoint, len(oldIstioEndpoints))
		nmap := make(map[string]*IstioEndpoint, len(newIstioEndpoints))
		// Add new endpoints only if they are ever ready once to shards
		// so that full push does not send them from shards.
		for _, oie := range oldIstioEndpoints {
			emap[oie.Address] = oie
		}
		for _, nie := range istioEndpoints {
			nmap[nie.Address] = nie
		}
		for _, nie := range istioEndpoints {
			if oie, exists := emap[nie.Address]; exists {
				// If endpoint exists already, we should push if it's health status changes.
				if oie.HealthStatus != nie.HealthStatus {
					needPush = true
				}
				newIstioEndpoints = append(newIstioEndpoints, nie)
			} else {
				// If the endpoint does not exist in shards that means it is a
				// new endpoint. Always send new healthy endpoints.
				// Also send new unhealthy endpoints when SendUnhealthyEndpoints is enabled.
				// This is OK since we disable panic threshold when SendUnhealthyEndpoints is enabled.
				if nie.HealthStatus != UnHealthy || features.SendUnhealthyEndpoints.Load() {
					needPush = true
				}
				newIstioEndpoints = append(newIstioEndpoints, nie)
			}
		}
		// Next, check for endpoints that were in old but no longer exist. If there are any, there is a
		// removal so we need to push an update.
		for _, oie := range oldIstioEndpoints {
			if _, f := nmap[oie.Address]; !f {
				needPush = true
			}
		}
	}

	if pushType != FullPush && !needPush {
		log.Debugf("No push, either old endpoint health status did not change or new endpoint came with unhealthy status, %v", hostname)
		pushType = NoPush
	}

	ep.Shards[shard] = newIstioEndpoints

	// Check if ServiceAccounts have changed. We should do a full push if they have changed.
	saUpdated := updateShardServiceAccount(ep, hostname)

	// For existing endpoints, we need to do full push if service accounts change.
	if saUpdated && pushType != FullPush {
		// Avoid extra logging if already a full push
		log.Infof("Full push, service accounts changed, %v", hostname)
		pushType = FullPush
	}

	// Clear the cache here. While it would likely be cleared later when we trigger a push, a race
	// condition is introduced where an XDS response may be generated before the update, but not
	// completed until after a response after the update. Essentially, we transition from v0 -> v1 ->
	// v0 -> invalidate -> v1. Reverting a change we pushed violates our contract of monotonically
	// moving forward in version. In practice, this is pretty rare and self corrects nearly
	// immediately. However, clearing the cache here has almost no impact on cache performance as we
	// would clear it shortly after anyways.
	e.clearCacheForService(hostname, namespace)

	return pushType
}

// updateShardServiceAccount updates the service endpoints' sa when service/endpoint event happens.
// Note: it is not concurrent safe.
func updateShardServiceAccount(shards *EndpointShards, serviceName string) bool {
	oldServiceAccount := shards.ServiceAccounts
	serviceAccounts := sets.String{}
	for _, epShards := range shards.Shards {
		for _, ep := range epShards {
			if ep.ServiceAccount != "" {
				serviceAccounts.Insert(ep.ServiceAccount)
			}
		}
	}

	if !oldServiceAccount.Equals(serviceAccounts) {
		shards.ServiceAccounts = serviceAccounts
		log.Debugf("Updating service accounts now, svc %v, before service account %v, after %v",
			serviceName, oldServiceAccount, serviceAccounts)
		return true
	}

	return false
}

// EndpointIndexUpdater is an updater that will keep an EndpointIndex in sync. This is intended for tests only.
type EndpointIndexUpdater struct {
	Index *EndpointIndex
}

var _ XDSUpdater = &EndpointIndexUpdater{}

func NewEndpointIndexUpdater(ei *EndpointIndex) *EndpointIndexUpdater {
	return &EndpointIndexUpdater{Index: ei}
}

func (f *EndpointIndexUpdater) ConfigUpdate(*PushRequest) {}

func (f *EndpointIndexUpdater) EDSUpdate(shard ShardKey, serviceName string, namespace string, eps []*IstioEndpoint) {
	f.Index.UpdateServiceEndpoints(shard, serviceName, namespace, eps)
}

func (f *EndpointIndexUpdater) EDSCacheUpdate(shard ShardKey, serviceName string, namespace string, eps []*IstioEndpoint) {
	f.Index.UpdateServiceEndpoints(shard, serviceName, namespace, eps)
}

func (f *EndpointIndexUpdater) SvcUpdate(shard ShardKey, hostname string, namespace string, event Event) {
	if event == EventDelete {
		f.Index.DeleteServiceShard(shard, hostname, namespace, false)
	}
}

func (f *EndpointIndexUpdater) ProxyUpdate(_ cluster.ID, _ string) {}

func (f *EndpointIndexUpdater) RemoveShard(shardKey ShardKey) {
	f.Index.DeleteShard(shardKey)
}
