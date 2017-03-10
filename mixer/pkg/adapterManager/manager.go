// Copyright 2016 Istio Authors
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
	"context"
	"crypto/sha1"
	"encoding/gob"
	"fmt"
	"sync"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

// Manager manages all aspects - provides uniform interface to
// all aspect managers
type Manager struct {
	managers  map[aspect.Kind]aspect.Manager
	mapper    expr.Evaluator
	builders  builderFinder
	methodMap map[aspect.APIMethod]config.AspectSet
	wg        sync.WaitGroup
	gp        *pool.GoroutinePool
	adapterGP *pool.GoroutinePool

	// protects cache
	lock        sync.RWMutex
	aspectCache map[cacheKey]aspect.Wrapper
}

// builderFinder finds a builder by name.
// a builder may produce aspects of multiple kinds.
type builderFinder interface {
	// FindBuilder finds a builder by name. == cfg.Adapter.Impl.
	FindBuilder(name string) (adapter.Builder, bool)

	// SupportedKinds returns kinds supported by a builder.
	SupportedKinds(name string) []string
}

// cacheKey is used to cache fully constructed aspects
// These parameters are used in constructing an aspect
type cacheKey struct {
	kind             aspect.Kind
	impl             string
	builderParamsSHA [sha1.Size]byte
	aspectParamsSHA  [sha1.Size]byte
}

func newCacheKey(kind aspect.Kind, cfg *config.Combined) (*cacheKey, error) {
	ret := cacheKey{
		kind: kind,
		impl: cfg.Builder.GetImpl(),
	}

	//TODO pre-compute shas and store with params
	b := pool.GetBuffer()
	// use gob encoding so that we don't rely on proto marshal
	enc := gob.NewEncoder(b)

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
	pool.PutBuffer(b)

	return &ret, nil
}

// NewManager creates a new adapterManager.
func NewManager(builders []adapter.RegisterFn, managers aspect.ManagerInventory, exp expr.Evaluator, gp *pool.GoroutinePool) *Manager {
	mm, am := ProcessBindings(managers)
	return newManager(newRegistry(builders), mm, exp, am, gp)
}

func newManager(r builderFinder, m map[aspect.Kind]aspect.Manager, exp expr.Evaluator,
	am map[aspect.APIMethod]config.AspectSet, gp *pool.GoroutinePool) *Manager {

	// TODO: make the pool parameters configurable
	adapterGP := pool.NewGoroutinePool(128)
	adapterGP.AddWorkers(1024)

	return &Manager{
		builders:    r,
		managers:    m,
		mapper:      exp,
		methodMap:   am,
		aspectCache: make(map[cacheKey]aspect.Wrapper),
		gp:          gp,
		adapterGP:   adapterGP,
	}
}

// Shutdown gracefully drains the manager's worker pool.
func (m *Manager) Shutdown() {
	m.wg.Wait()
	if m.adapterGP != nil {
		m.adapterGP.Close()
	}
}

// Execute iterates over cfgs and performs the actions described by the combined config using the attribute bag on each config.
func (m *Manager) Execute(ctx context.Context, cfgs []*config.Combined, attrs attribute.Bag, ma aspect.APIMethodArgs) aspect.Output {
	numCfgs := len(cfgs)

	// TODO: look into pooling both result array and channel, they're created per-request and are constant size for cfg lifetime.
	results := make([]result, numCfgs)
	resultChan := make(chan result, numCfgs)

	if m.gp == nil {
		for i, cfg := range cfgs {
			results[i] = result{cfg, m.execute(ctx, cfg, attrs, ma)}
		}
	} else {
		// schedule all the work that needs to happen
		for _, cfg := range cfgs {
			m.wg.Add(1)
			m.gp.ScheduleWork(func() {
				out := m.execute(ctx, cfg, attrs, ma)
				resultChan <- result{cfg, out}
				m.wg.Done()
			})
		}

		// wait for all the work to be done or the context to be cancelled
		for i := 0; i < numCfgs; i++ {
			select {
			case <-ctx.Done():
				return aspect.Output{Status: status.WithDeadlineExceeded(fmt.Sprintf("deadline exceeded waiting for adapter results with err: %v", ctx.Err()))}
			case res := <-resultChan:
				results[i] = res
			}
		}
	}

	return combineResults(results)
}

// Combines a bunch of distinct result structs and turns 'em into one single Output struct
func combineResults(results []result) aspect.Output {
	var buf *bytes.Buffer
	code := rpc.OK

	for _, r := range results {
		if !r.out.IsOK() {
			if buf == nil {
				buf = pool.GetBuffer()
				// the first failure result's code becomes the result code for the output
				code = rpc.Code(r.out.Status.Code)
			} else {
				buf.WriteString(", ")
			}
			buf.WriteString(r.cfg.String() + ":" + r.out.Message())
		}
	}

	s := status.OK
	if buf != nil {
		s = status.WithMessage(code, buf.String())
		pool.PutBuffer(buf)
	}

	return aspect.Output{Status: s}
}

// result holds the values returned by the execution of an adapter
type result struct {
	cfg *config.Combined
	out aspect.Output
}

// execute performs action described in the combined config using the attribute bag
func (m *Manager) execute(ctx context.Context, cfg *config.Combined, attrs attribute.Bag, ma aspect.APIMethodArgs) (out aspect.Output) {
	var mgr aspect.Manager
	var found bool

	kind, found := aspect.ParseKind(cfg.Aspect.Kind)
	if !found {
		return aspect.Output{Status: status.WithError(fmt.Errorf("invalid aspect %#v", cfg.Aspect.Kind))}
	}

	mgr, found = m.managers[kind]
	if !found {
		return aspect.Output{Status: status.WithError(fmt.Errorf("could not find aspect manager %#v", cfg.Aspect.Kind))}
	}

	var adp adapter.Builder
	if adp, found = m.builders.FindBuilder(cfg.Builder.Impl); !found {
		return aspect.Output{Status: status.WithError(fmt.Errorf("could not find registered adapter %#v", cfg.Builder.Impl))}
	}

	// Both cacheGet and asp.Execute call adapter-supplied code, so we need to guard against both panicking.
	defer func() {
		if r := recover(); r != nil {
			out = aspect.Output{Status: status.WithError(fmt.Errorf("adapter '%s' panicked with '%v'", adp.Name(), r))}
		}
	}()

	asp, err := m.cacheGet(cfg, mgr, adp)
	if err != nil {
		return aspect.Output{Status: status.WithError(err)}
	}

	// TODO: plumb  ctx through asp.Execute
	_ = ctx
	// TODO: act on asp.Output
	return asp.Execute(attrs, m.mapper, ma)
}

// cacheGet gets an aspect wrapper from the cache, use adapter.Manager to construct an object in case of a cache miss
func (m *Manager) cacheGet(cfg *config.Combined, mgr aspect.Manager, builder adapter.Builder) (asp aspect.Wrapper, err error) {
	var key *cacheKey
	if key, err = newCacheKey(mgr.Kind(), cfg); err != nil {
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
	env := newEnv(builder.Name(), m.adapterGP)
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

// AspectValidatorFinder returns a ValidatorFinderFunc for aspects.
func (m *Manager) AspectValidatorFinder() config.ValidatorFinderFunc {
	return func(kind string) (adapter.ConfigValidator, bool) {
		k, found := aspect.ParseKind(kind)
		if !found {
			return nil, false
		}
		c, found := m.managers[k]
		return c, found
	}
}

// AdapterToAspectMapperFunc returns AdapterToAspectMapperFunc.
func (m *Manager) AdapterToAspectMapperFunc() config.AdapterToAspectMapperFunc {
	return func(impl string) (kinds []string) {
		return m.builders.SupportedKinds(impl)
	}
}

// BuilderValidatorFinder returns a ValidatorFinderFunc for builders.
func (m *Manager) BuilderValidatorFinder() config.ValidatorFinderFunc {
	return func(name string) (adapter.ConfigValidator, bool) {
		return m.builders.FindBuilder(name)
	}
}

// MethodMap returns map of APIMethod --> AspectSet.
func (m *Manager) MethodMap() map[aspect.APIMethod]config.AspectSet {
	return m.methodMap
}

// ProcessBindings returns a fully constructed manager map and aspectSet.
func ProcessBindings(managers aspect.ManagerInventory) (map[aspect.Kind]aspect.Manager, map[aspect.APIMethod]config.AspectSet) {
	r := make(map[aspect.Kind]aspect.Manager)
	as := make(map[aspect.APIMethod]config.AspectSet)

	for method, mgrs := range managers {
		as[method] = config.AspectSet{}

		for _, m := range mgrs {
			r[m.Kind()] = m
			as[method][m.Kind().String()] = true
		}
	}

	return r, as
}
