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

import "sync"

// EdsUpdatedServices keeps track of all service updated since last full push.
// Key is the hostname (servicename). Value is set when any shard part of the service is
// updated. For 1.0.3+ it is used only for tracking incremental
// pushes between the 2 epochs.
type EdsUpdatedServices struct {
	mutex sync.RWMutex
	// key is servicename
	cache map[string]struct{}
}

func (e *EdsUpdatedServices) Set(key string) {
	e.mutex.Lock()
	if e.cache == nil {
		e.cache = make(map[string]struct{})
	}
	e.cache[key] = struct{}{}
	e.mutex.Unlock()
}

// GetAndClear returns the cache and clear the cache
func (e *EdsUpdatedServices) GetAndClear() map[string]struct{} {
	e.mutex.Lock()
	out := e.cache
	e.cache = make(map[string]struct{})
	e.mutex.Unlock()

	return out
}

// ProxyUpdates keeps track of proxies that need full xDS push during the next push epoch
type ProxyUpdates struct {
	mutex sync.RWMutex
	// key is proxy ip
	cache map[string]struct{}
}

func (p *ProxyUpdates) Set(key string) {
	p.mutex.Lock()
	if p.cache == nil {
		p.cache = make(map[string]struct{})
	}
	p.cache[key] = struct{}{}
	p.mutex.Unlock()
}

// GetAndClear returns the cache and clear the cache
func (p *ProxyUpdates) GetAndClear() map[string]struct{} {
	p.mutex.Lock()
	out := p.cache
	p.cache = make(map[string]struct{})
	p.mutex.Unlock()

	return out
}
