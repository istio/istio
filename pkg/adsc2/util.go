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

package adsc2

import (
	"sync"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/xds"
)

func isDebugType(typeURL string) bool {
	return typeURL == xds.TypeDebugSyncronization || typeURL == xds.TypeDebugConfigDump || typeURL == xds.TypeURLConnect
}

type cache struct {
	mu        sync.RWMutex
	resources map[resourceKey]*discovery.Resource
}

func newResourceCache() *cache {
	return &cache{
		resources: make(map[resourceKey]*discovery.Resource),
	}
}

func (c *cache) put(key resourceKey, resource *discovery.Resource) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resources[key] = resource
}

func (c *cache) get(key resourceKey) *discovery.Resource {
	c.mu.RLock()
	defer c.mu.RUnlock()
	res := c.resources[key]
	return res
}

func (c *cache) delete(key resourceKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.resources, key)
}
