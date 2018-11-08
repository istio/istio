//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package meshconfig

import (
	"sync"

	"istio.io/api/mesh/v1alpha1"
)

// InMemoryCache is an in-memory only mesh cache
type InMemoryCache struct {
	configMutex sync.Mutex
	config      v1alpha1.MeshConfig
}

var _ Cache = &InMemoryCache{}

// NewInMemory returns a new InMemoryCache
func NewInMemory() *InMemoryCache {
	return &InMemoryCache{
		config: Default(),
	}
}

// Set the value of mesh config.
func (c *InMemoryCache) Set(cfg v1alpha1.MeshConfig) {
	c.configMutex.Lock()
	defer c.configMutex.Unlock()
	c.config = cfg
}

// Get the value of mesh config.
func (c *InMemoryCache) Get() v1alpha1.MeshConfig {
	c.configMutex.Lock()
	defer c.configMutex.Unlock()
	return c.config
}
