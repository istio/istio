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

package xds

import (
	"sync"

	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/host"
)

// Cache interface defines a store for caching XDS responses
// Note this is currently only for EDS, and will need some modifications to support other types
// All operations are thread safe
type Cache interface {
	Insert(key EndpointBuilder, value *any.Any)
	Get(key EndpointBuilder) (*any.Any, bool)
	ClearDestinationRules(map[model.ConfigKey]struct{})
	ClearHostnames(map[model.ConfigKey]struct{})
	ClearAll()
	// Keys returns all currently configured keys. This is for testing/debug only
	Keys() []string
}

type inMemoryCache struct {
	store                map[string]*any.Any
	hostIndex            map[host.Name]sets.Set
	destinationRuleIndex map[string]sets.Set
	mu                   sync.RWMutex
}

var _ Cache = &inMemoryCache{}

func NewInMemoryCache() Cache {
	return &inMemoryCache{
		store:                map[string]*any.Any{},
		hostIndex:            map[host.Name]sets.Set{},
		destinationRuleIndex: map[string]sets.Set{},
	}
}

func (c *inMemoryCache) Insert(key EndpointBuilder, value *any.Any) {
	// If service is not defined, we cannot do any caching as we will not have a way to invalidate the results
	// Service being nil means the EDS will be empty anyways, so not much lost here
	if key.service == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := key.Key()
	c.store[k] = value
	if c.hostIndex[key.service.Hostname] == nil {
		c.hostIndex[key.service.Hostname] = sets.NewSet()
	}
	c.hostIndex[key.service.Hostname].Insert(k)
	if dr := key.destinationRule; dr != nil {
		dkey := dr.Name + "/" + dr.Namespace
		if c.destinationRuleIndex[dkey] == nil {
			c.destinationRuleIndex[dkey] = sets.NewSet()
		}
		c.destinationRuleIndex[dkey].Insert(k)
	}
}

func (c *inMemoryCache) Get(key EndpointBuilder) (*any.Any, bool) {
	if key.service == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, f := c.store[key.Key()]
	return k, f
}

func (c *inMemoryCache) ClearHostnames(hostsUpdated map[model.ConfigKey]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for h := range hostsUpdated {
		referenced := c.hostIndex[host.Name(h.Name)]
		delete(c.hostIndex, host.Name(h.Name))
		for keys := range referenced {
			delete(c.store, keys)
		}
	}
}

func (c *inMemoryCache) ClearDestinationRules(destinationRulesUpdate map[model.ConfigKey]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for d := range destinationRulesUpdate {
		key := d.Name + "/" + d.Namespace
		referenced := c.destinationRuleIndex[key]
		delete(c.destinationRuleIndex, key)
		for keys := range referenced {
			delete(c.store, keys)
		}
	}
}

func (c *inMemoryCache) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = map[string]*any.Any{}
	c.hostIndex = map[host.Name]sets.Set{}
	c.destinationRuleIndex = map[string]sets.Set{}
}

func (c *inMemoryCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := []string{}
	for k := range c.store {
		keys = append(keys, k)
	}
	return keys
}

// DisabledCache is a cache that is always empty
type DisabledCache struct{}

var _ Cache = &DisabledCache{}

func (d DisabledCache) Insert(EndpointBuilder, *any.Any) {}

func (d DisabledCache) Get(EndpointBuilder) (*any.Any, bool) {
	return nil, false
}

func (d DisabledCache) ClearDestinationRules(map[model.ConfigKey]struct{}) {}

func (d DisabledCache) ClearHostnames(map[model.ConfigKey]struct{}) {}

func (d DisabledCache) ClearAll() {}

func (d DisabledCache) Keys() []string { return nil }
