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
	"bytes"
	"crypto/sha1"
	"encoding/gob"
	"fmt"
	"sync"

	"github.com/golang/glog"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

// Manager manages all aspects - provides uniform interface to
// all aspect managers
type Manager struct {
	managerFinder map[string]aspect.Manager
	mapper        expr.Evaluator
	builderFinder builderFinder
	aspectmap     map[config.APIMethod]config.AspectSet

	// protects cache
	lock        sync.RWMutex
	aspectCache map[cacheKey]aspect.Wrapper
}

// builderFinder finds a builder by kind & name.
type builderFinder interface {
	// FindBuilder finds a builder by kind & name.
	FindBuilder(kind string, name string) (adapter.Builder, bool)
}

// cacheKey is used to cache fully constructed aspects
// These parameters are used in constructing an aspect
type cacheKey struct {
	kind             string
	impl             string
	builderParamsSHA [sha1.Size]byte
	aspectParamsSHA  [sha1.Size]byte
}

func newCacheKey(cfg *config.Combined) (*cacheKey, error) {
	ret := cacheKey{
		kind: cfg.Aspect.GetKind(),
		impl: cfg.Builder.GetImpl(),
	}

	//TODO pre-compute shas and store with params
	var b bytes.Buffer
	// use gob encoding so that we don't rely on proto marshal
	enc := gob.NewEncoder(&b)

	if cfg.Builder.GetParams() != nil {
		if err := enc.Encode(cfg.Builder.GetParams()); err != nil {
			return nil, err
		}
		ret.builderParamsSHA = sha1.Sum(b.Bytes())
	}
	b.Reset()
	if cfg.Aspect.GetParams() != nil {
		if err := enc.Encode(cfg.Aspect.GetParams()); err != nil {
			return nil, err
		}

		ret.aspectParamsSHA = sha1.Sum(b.Bytes())
	}

	return &ret, nil
}

// NewManager creates a new adapterManager.
func NewManager(builders []adapter.RegisterFn, managers []aspect.APIBinding, exp expr.Evaluator) *Manager {
	mm, am := ProcessBindings(managers)
	return newManager(newRegistry(builders), mm, exp, am)
}

// newManager
func newManager(r builderFinder, m map[string]aspect.Manager, exp expr.Evaluator, am map[config.APIMethod]config.AspectSet) *Manager {
	return &Manager{
		managerFinder: m,
		builderFinder: r,
		mapper:        exp,
		aspectCache:   make(map[cacheKey]aspect.Wrapper),
		aspectmap:     am,
	}
}

// Execute performs action described in the combined config using the attribute bag
func (m *Manager) Execute(cfg *config.Combined, attrs attribute.Bag) (out *aspect.Output, err error) {
	var mgr aspect.Manager
	var found bool

	if mgr, found = m.managerFinder[cfg.Aspect.Kind]; !found {
		return nil, fmt.Errorf("could not find aspect manager %#v", cfg.Aspect.Kind)
	}

	var adp adapter.Builder
	if adp, found = m.builderFinder.FindBuilder(cfg.Aspect.Kind, cfg.Builder.Impl); !found {
		return nil, fmt.Errorf("could not find registered adapter %#v", cfg.Builder.Impl)
	}

	// Both cacheGet and asp.Execute call adapter-supplied code, so we need to guard against both panicking.
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("adapter '%s' panicked with '%v'", adp.Name(), r)
		}
	}()

	var asp aspect.Wrapper
	if asp, err = m.cacheGet(cfg, mgr, adp); err != nil {
		return nil, err
	}

	// TODO act on adapter.Output
	return asp.Execute(attrs, m.mapper)
}

// cacheGet -- get from the cache, use adapter.Manager to construct an object in case of a cache miss
func (m *Manager) cacheGet(cfg *config.Combined, mgr aspect.Manager, builder adapter.Builder) (asp aspect.Wrapper, err error) {
	var key *cacheKey
	if key, err = newCacheKey(cfg); err != nil {
		return nil, err
	}
	// try fast path with read lock
	m.lock.RLock()
	asp, found := m.aspectCache[*key]
	m.lock.RUnlock()
	if found {
		return asp, nil
	}

	// create an aspect
	env := newEnv(builder.Name())
	asp, err = mgr.NewAspect(cfg, builder, env)
	if err != nil {
		return nil, err
	}

	// obtain write lock
	m.lock.Lock()
	// see if someone else beat you to it
	if other, found := m.aspectCache[*key]; found {
		defer closeWrapper(asp)
		asp = other
	} else {
		// your are the first one, save your aspect
		m.aspectCache[*key] = asp
	}

	m.lock.Unlock()

	return asp, nil
}

func closeWrapper(asp aspect.Wrapper) {
	if err := asp.Close(); err != nil {
		glog.Warningf("Error closing aspect: %v: %v", asp, err)
	}
}

type aspectValidatorFinder struct {
	m map[string]aspect.Manager
}

func (a *aspectValidatorFinder) FindValidator(kind string, name string) (adapter.ConfigValidator, bool) {
	c, ok := a.m[name]
	return c, ok
}

// AspectValidatorFinder returns ValidatorFinder for aspects.
func (m *Manager) AspectValidatorFinder() config.ValidatorFinder {
	return &aspectValidatorFinder{m: m.managerFinder}
}

type builderValidatorFinder struct {
	b builderFinder
}

func (a *builderValidatorFinder) FindValidator(kind string, name string) (adapter.ConfigValidator, bool) {
	return a.b.FindBuilder(kind, name)
}

// BuilderValidatorFinder returns ValidatorFinder for builders.
func (m *Manager) BuilderValidatorFinder() config.ValidatorFinder {
	return &builderValidatorFinder{b: m.builderFinder}
}

// AspectMap returns map of APIMethod --> AspectSet.
func (m *Manager) AspectMap() map[config.APIMethod]config.AspectSet {
	return m.aspectmap
}

// ProcessBindings returns a fully constructed manager map and aspectSet given APIBinding.
func ProcessBindings(bnds []aspect.APIBinding) (map[string]aspect.Manager, map[config.APIMethod]config.AspectSet) {
	r := make(map[string]aspect.Manager)
	as := make(map[config.APIMethod]config.AspectSet)

	// setup aspect sets for all methods
	for _, am := range config.APIMethods() {
		as[am] = config.AspectSet{}
	}
	for _, bnd := range bnds {
		r[bnd.Aspect.Kind()] = bnd.Aspect
		as[bnd.Method][bnd.Aspect.Kind()] = true
	}
	return r, as
}
