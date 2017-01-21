// Copyright 2016 Google Inc.
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

package uber

import (
	"fmt"
	"sync"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspectsupport"
	"istio.io/mixer/pkg/aspectsupport/denyChecker"
	"istio.io/mixer/pkg/aspectsupport/listChecker"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

// RegistryQuerier enables querying the adapter registry for  adapter instances.
type RegistryQuerier interface {
	// ByImpl queries the registry by adapter name.
	ByImpl(adapterName string) (adapter.Adapter, bool)
}

// Manager manages all aspects - provides uniform interface to
// all aspect managers
type Manager struct {
	mreg map[string]aspectsupport.Manager
	areg RegistryQuerier

	// protects cache
	lock        sync.RWMutex
	aspectCache map[CacheKey]aspectsupport.AspectWrapper
}

// CacheKey is used to cache fully constructed aspects
// These parameters are used in constructing an aspect
type CacheKey struct {
	Kind   string
	Impl   string
	Params string
	Args   string
}

func cacheKey(cfg *aspectsupport.CombinedConfig) CacheKey {
	return CacheKey{
		Kind:   cfg.Aspect.GetKind(),
		Impl:   cfg.Adapter.GetImpl(),
		Params: cfg.Aspect.GetParams().String(),
		Args:   cfg.Adapter.GetParams().String(),
	}
}

// NewManager Creates a new Uber Aspect manager
func NewManager(areg RegistryQuerier, mgrs []aspectsupport.Manager) *Manager {
	// Add all managers here as new aspects are added
	if mgrs == nil {
		mgrs = aspectManagers()
	}
	mreg := make(map[string]aspectsupport.Manager, len(mgrs))
	for _, mgr := range mgrs {
		mreg[mgr.Kind()] = mgr
	}
	return &Manager{
		mreg:        mreg,
		aspectCache: make(map[CacheKey]aspectsupport.AspectWrapper),
		areg:        areg,
	}
}

// Execute performs the aspect function based on CombinedConfig and attributes and an expression evaluator
// returns aspect output or error if the operation could not be performed
func (m *Manager) Execute(cfg *aspectsupport.CombinedConfig, attrs attribute.Bag, mapper expr.Evaluator) (*aspectsupport.Output, error) {
	var mgr aspectsupport.Manager
	var found bool

	if mgr, found = m.mreg[cfg.Aspect.Kind]; !found {
		return nil, fmt.Errorf("could not find aspect manager %#v", cfg.Aspect.Kind)
	}

	var adapter adapter.Adapter
	if adapter, found = m.areg.ByImpl(cfg.Adapter.Impl); !found {
		return nil, fmt.Errorf("could not find registered adapter %#v", cfg.Adapter.Impl)
	}

	var asp aspectsupport.AspectWrapper
	var err error
	if asp, err = m.CacheGet(cfg, mgr, adapter); err != nil {
		return nil, err
	}

	// TODO act on adapter.Output
	return asp.Execute(attrs, mapper)
}

// CacheGet -- get from the cache, use adapter.Manager to construct an object in case of a cache miss
func (m *Manager) CacheGet(cfg *aspectsupport.CombinedConfig, mgr aspectsupport.Manager, adapter adapter.Adapter) (asp aspectsupport.AspectWrapper, err error) {
	key := cacheKey(cfg)
	// try fast path with read lock
	m.lock.RLock()
	asp, found := m.aspectCache[key]
	m.lock.RUnlock()
	if found {
		return asp, nil
	}
	// obtain write lock
	m.lock.Lock()
	defer m.lock.Unlock()
	asp, found = m.aspectCache[key]
	if !found {
		env := newEnv(adapter.Name())
		asp, err = mgr.NewAspect(cfg, adapter, env)
		if err != nil {
			return nil, err
		}
		m.aspectCache[key] = asp
	}
	return asp, nil
}

// return list of aspect managers
func aspectManagers() []aspectsupport.Manager {
	return []aspectsupport.Manager{
		listChecker.NewManager(),
		denyChecker.NewManager(),
	}
}
