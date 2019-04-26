// Copyright 2019 Istio Authors
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

package cache

import (
	"encoding/json"
	"sync"

	"istio.io/istio/pilot/pkg/model"
)

// EndpointShards holds the set of endpoint shards of a service. Registries update
// individual shards incrementally. The shards are aggregated and split into
// clusters when a push for the specific cluster is needed.
type EndpointShards struct {
	// mutex protecting below map.
	mutex sync.RWMutex

	// Shards is used to track the shards. EDS updates are grouped by shard.
	// Current implementation uses the registry name as key - in multicluster this is the
	// name of the k8s cluster, derived from the config (secret).
	Shards map[string][]*model.IstioEndpoint

	// ServiceAccounts has the concatenation of all service accounts seen so far in endpoints.
	// This is updated on push, based on shards. If the previous list is different than
	// current list, a full push will be forced, to trigger a secure naming update.
	// Due to the larger time, it is still possible that connection errors will occur while
	// CDS is updated.
	ServiceAccounts map[string]bool
}

// EndpointShards for a service. This is a global (per-server) list, built from
// incremental updates.
type EndpointShardsByService struct {
	mutex sync.RWMutex
	cache map[string]*EndpointShards
}

func (e *EndpointShardsByService) Set(key string, value *EndpointShards) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.cache == nil {
		e.cache = make(map[string]*EndpointShards)
	}
	e.cache[key] = value
}

func (e *EndpointShardsByService) Get(key string) (*EndpointShards, bool) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	value, ok := e.cache[key]

	return value, ok
}

func (e *EndpointShardsByService) Delete(key string) {
	e.mutex.Lock()
	delete(e.cache, key)
	e.mutex.Unlock()
}

func (e *EndpointShardsByService) DeleteServiceShard(serviceName string, shard string) {
	if epShards, _ := e.Get(serviceName); epShards != nil {
		epShards.mutex.Lock()
		delete(epShards.Shards, shard)
		// delete service shards totally
		if len(epShards.Shards) == 0 {
			e.Delete(serviceName)
		}
		epShards.mutex.Unlock()
	}
}

func (e *EndpointShardsByService) ToBytes() []byte {
	e.mutex.RLock()
	out, _ := json.MarshalIndent(e.cache, " ", " ")
	e.mutex.RUnlock()

	return out
}

func (e *EndpointShards) ServiceAccountExist(sa string) bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.ServiceAccounts[sa]
}

func (e *EndpointShards) SetShard(shard string, endpoints []*model.IstioEndpoint) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.Shards[shard] = endpoints
}

// Get all the IstioEndpoints from all shards
func (e *EndpointShards) ListIstioEndpoints() []*model.IstioEndpoint {
	out := []*model.IstioEndpoint{}
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	for _, eps := range e.Shards {
		out = append(out, eps...)
	}

	return out
}
