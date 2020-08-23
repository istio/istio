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
	"fmt"
	"sync"

	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/sets"
)

type CacheKey interface {
	Key() string
	DependentConfigs() []model.ConfigKey
	Cacheable() bool
}

// Cache interface defines a store for caching XDS responses
// Note this is currently only for EDS, and will need some modifications to support other types
// All operations are thread safe
type Cache interface {
	Add(key CacheKey, value *any.Any)
	Get(key CacheKey) (*any.Any, bool)
	Clear(map[model.ConfigKey]struct{})
	ClearAll()
	// Keys returns all currently configured keys. This is for testing/debug only
	Keys() []string
}

type inMemoryCache struct {
	store       map[string]*any.Any
	configIndex map[model.ConfigKey]sets.Set
	mu          sync.RWMutex
}

var _ Cache = &inMemoryCache{}

func NewInMemoryCache() Cache {
	return &inMemoryCache{
		store:       map[string]*any.Any{},
		configIndex: map[model.ConfigKey]sets.Set{},
	}
}

func (c *inMemoryCache) Add(key CacheKey, value *any.Any) {
	if !key.Cacheable() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := key.Key()
	c.store[k] = value
	for _, config := range key.DependentConfigs() {
		if c.configIndex[config] == nil {
			c.configIndex[config] = sets.NewSet()
		}
		c.configIndex[config].Insert(k)
	}
}

func (c *inMemoryCache) Get(key CacheKey) (*any.Any, bool) {
	if !key.Cacheable() {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, f := c.store[key.Key()]
	return k, f
}

func (c *inMemoryCache) Clear(configs map[model.ConfigKey]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Println(configs)
	for ckey := range configs {
		referenced := c.configIndex[ckey]
		delete(c.configIndex, ckey)
		for keys := range referenced {
			delete(c.store, keys)
		}
	}
}

func (c *inMemoryCache) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = map[string]*any.Any{}
	c.configIndex = map[model.ConfigKey]sets.Set{}
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

func (d DisabledCache) Add(key CacheKey, value *any.Any) {}

func (d DisabledCache) Get(CacheKey) (*any.Any, bool) {
	return nil, false
}

func (d DisabledCache) Clear(configsUpdated map[model.ConfigKey]struct{}) {}

func (d DisabledCache) ClearAll() {}

func (d DisabledCache) Keys() []string { return nil }
