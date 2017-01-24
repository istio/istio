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

package adapterManager

import (
	"fmt"
	"sync"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

// Manager manages all aspects - provides uniform interface to
// all aspect managers
type Manager struct {
	mreg map[string]aspect.Manager
	areg *Registry

	// protects cache
	lock        sync.RWMutex
	aspectCache map[cacheKey]aspect.Wrapper
}

// cacheKey is used to cache fully constructed aspects
// These parameters are used in constructing an aspect
type cacheKey struct {
	Kind   string
	Impl   string
	Params string
	Args   string
}

func newCacheKey(cfg *aspect.CombinedConfig) cacheKey {
	return cacheKey{
		Kind:   cfg.Aspect.GetKind(),
		Impl:   cfg.Builder.GetImpl(),
		Params: cfg.Aspect.GetParams().String(),
		Args:   cfg.Builder.GetParams().String(),
	}
}

// NewManager Creates a new Uber Aspect manager
func NewManager(mgrs []aspect.Manager) *Manager {
	// Add all managers here as new aspects are added
	if mgrs == nil {
		mgrs = []aspect.Manager{
			aspect.NewListCheckerManager(),
			aspect.NewDenyCheckerManager(),
		}
	}

	mreg := make(map[string]aspect.Manager, len(mgrs))
	for _, mgr := range mgrs {
		mreg[mgr.Kind()] = mgr
	}

	return &Manager{
		mreg:        mreg,
		areg:        newRegistry(),
		aspectCache: make(map[cacheKey]aspect.Wrapper),
	}
}

// Registry returns the registry used to track adapters.
func (m *Manager) Registry() *Registry {
	return m.areg
}

// Execute performs the aspect function based on CombinedConfig and attributes and an expression evaluator
// returns aspect output or error if the operation could not be performed
func (m *Manager) Execute(cfg *aspect.CombinedConfig, attrs attribute.Bag, mapper expr.Evaluator) (out *aspect.Output, err error) {
	var mgr aspect.Manager
	var found bool

	if mgr, found = m.mreg[cfg.Aspect.Kind]; !found {
		return nil, fmt.Errorf("could not find aspect manager %#v", cfg.Aspect.Kind)
	}

	var adapter adapter.Builder
	if adapter, found = m.areg.ByImpl(cfg.Builder.Impl); !found {
		return nil, fmt.Errorf("could not find registered adapter %#v", cfg.Builder.Impl)
	}

	// Both cacheGet and asp.Execute call adapter-supplied code, so we need to guard against both panicking.
	defer func() {
		if r := recover(); r != nil {
			out = nil // invalidate whatever partial result we got; should we set this to a Code.Code_INTERNAL or similar?
			err = fmt.Errorf("adapter '%s' panicked with '%v'", cfg.Builder.Name, r)
			return
		}
	}()

	var asp aspect.Wrapper
	if asp, err = m.cacheGet(cfg, mgr, adapter); err != nil {
		return nil, err
	}

	// TODO act on adapter.Output
	return asp.Execute(attrs, mapper)
}

// CacheGet -- get from the cache, use adapter.Manager to construct an object in case of a cache miss
func (m *Manager) cacheGet(cfg *aspect.CombinedConfig, mgr aspect.Manager, adapter adapter.Builder) (asp aspect.Wrapper, err error) {
	key := newCacheKey(cfg)
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
