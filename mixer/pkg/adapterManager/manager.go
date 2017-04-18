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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

// AspectDispatcher executes aspects associated with individual API methods
type AspectDispatcher interface {

	// Preprocess dispatches to the set of aspects that will run before any
	// other aspects in the mixer (aka: the Check, Report, Quota aspects).
	Preprocess(ctx context.Context, requestBag, responseBag *attribute.MutableBag) rpc.Status

	// Check dispatches to the set of aspects associated with the Check API method
	Check(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status

	// Report dispatches to the set of aspects associated with the Report API method
	Report(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag) rpc.Status

	// Quota dispatches to the set of aspects associated with the Quota API method
	Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status)
}

// Manager manages all aspects - provides uniform interface to
// all aspect managers
type Manager struct {
	managers          [config.NumKinds]aspect.Manager
	mapper            expr.Evaluator
	builders          builderFinder
	checkKindSet      config.KindSet
	reportKindSet     config.KindSet
	quotaKindSet      config.KindSet
	preprocessKindSet config.KindSet
	gp                *pool.GoroutinePool
	adapterGP         *pool.GoroutinePool

	// Configs for the aspects that'll be used to serve each API method.
	cfg atomic.Value
	df  atomic.Value

	// protects cache
	lock          sync.RWMutex
	executorCache map[cacheKey]aspect.Executor
}

// builderFinder finds a builder by name.
// a builder may produce aspects of multiple kinds.
type builderFinder interface {
	// FindBuilder finds a builder by name. == cfg.Adapter.Impl.
	FindBuilder(name string) (adapter.Builder, bool)

	// SupportedKinds returns kinds supported by a builder.
	SupportedKinds(builder string) config.KindSet
}

// NewManager creates a new adapterManager.
func NewManager(builders []adapter.RegisterFn, inventory aspect.ManagerInventory,
	exp expr.Evaluator, gp *pool.GoroutinePool, adapterGP *pool.GoroutinePool) *Manager {
	mm := Aspects(inventory)
	return newManager(newRegistry(builders), mm, exp, inventory, gp, adapterGP)
}

func newManager(r builderFinder, m [config.NumKinds]aspect.Manager, exp expr.Evaluator,
	inventory aspect.ManagerInventory, gp *pool.GoroutinePool, adapterGP *pool.GoroutinePool) *Manager {

	mg := &Manager{
		builders:      r,
		managers:      m,
		mapper:        exp,
		executorCache: make(map[cacheKey]aspect.Executor),
		gp:            gp,
		adapterGP:     adapterGP,
	}

	for _, m := range inventory.Preprocess {
		mg.preprocessKindSet = mg.preprocessKindSet.Set(m.Kind())
	}

	for _, m := range inventory.Check {
		mg.checkKindSet = mg.checkKindSet.Set(m.Kind())
	}

	for _, m := range inventory.Report {
		mg.reportKindSet = mg.reportKindSet.Set(m.Kind())
	}

	for _, m := range inventory.Quota {
		mg.quotaKindSet = mg.quotaKindSet.Set(m.Kind())
	}

	return mg
}

// Check dispatches to the set of aspects associated with the Check API method
func (m *Manager) Check(ctx context.Context, requestBag, responseBag *attribute.MutableBag) rpc.Status {
	configs, err := m.loadConfigs(requestBag, m.checkKindSet, false, true /* fail if unable to eval all selectors */)
	if err != nil {
		glog.Error(err)
		return status.WithError(err)
	}
	return m.dispatch(ctx, requestBag, responseBag, configs,
		func(executor aspect.Executor, evaluator expr.Evaluator) rpc.Status {
			cw := executor.(aspect.CheckExecutor)
			return cw.Execute(requestBag, evaluator)
		})
}

// Report dispatches to the set of aspects associated with the Report API method
func (m *Manager) Report(ctx context.Context, requestBag, responseBag *attribute.MutableBag) rpc.Status {
	configs, err := m.loadConfigs(requestBag, m.reportKindSet, false, false /* carry on if unable to eval all selectors */)
	if err != nil {
		glog.Error(err)
		return status.WithError(err)
	}
	return m.dispatch(ctx, requestBag, responseBag, configs,
		func(executor aspect.Executor, evaluator expr.Evaluator) rpc.Status {
			rw := executor.(aspect.ReportExecutor)
			return rw.Execute(requestBag, evaluator)
		})
}

// Quota dispatches to the set of aspects associated with the Quota API method
func (m *Manager) Quota(ctx context.Context, requestBag, responseBag *attribute.MutableBag,
	qma *aspect.QuotaMethodArgs) (*aspect.QuotaMethodResp, rpc.Status) {

	var qmr *aspect.QuotaMethodResp

	configs, err := m.loadConfigs(requestBag, m.quotaKindSet, false, true /* fail if unable to eval all selectors */)
	if err != nil {
		glog.Error(err)
		return qmr, status.WithError(err)
	}

	o := m.dispatch(ctx, requestBag, responseBag, configs,
		func(executor aspect.Executor, evaluator expr.Evaluator) rpc.Status {
			qw := executor.(aspect.QuotaExecutor)
			var o rpc.Status
			o, qmr = qw.Execute(requestBag, evaluator, qma)
			return o
		})

	return qmr, o
}

func (m *Manager) loadConfigs(attrs attribute.Bag, ks config.KindSet, isPreprocess bool, strict bool) ([]*cpb.Combined, error) {
	cfg, _ := m.cfg.Load().(config.Resolver)
	if cfg == nil {
		return nil, errors.New("configuration is not yet available")
	}

	resolveFn := cfg.Resolve
	if isPreprocess {
		resolveFn = cfg.ResolveUnconditional
	}
	configs, err := resolveFn(attrs, ks, strict)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve config: %v", err)
	}
	if glog.V(2) {
		glog.Infof("Resolved %d configs: %v ", len(configs), configs)
	}
	return configs, nil
}

// Preprocess dispatches to the set of aspects that must run before any other
// configured aspects.
func (m *Manager) Preprocess(ctx context.Context, requestBag, responseBag *attribute.MutableBag) rpc.Status {
	// We may be missing attributes used in selectors, so we must be non-strict during evaluation.
	configs, err := m.loadConfigs(requestBag, m.preprocessKindSet, true, false)
	if err != nil {
		glog.Error(err)
		return status.WithError(err)
	}
	return m.dispatch(ctx, requestBag, responseBag, configs,
		func(executor aspect.Executor, eval expr.Evaluator) rpc.Status {
			ppw := executor.(aspect.PreprocessExecutor)
			result, rpcStatus := ppw.Execute(requestBag, eval)
			if status.IsOK(rpcStatus) {
				if err := responseBag.Merge(result.Attrs); err != nil {
					// TODO: better error messages that push internal details into debuginfo messages
					rpcStatus = status.WithInternal("The results from the request preprocessing could not be merged.")
				}
			}
			return rpcStatus
		})
}

type invokeExecutorFunc func(executor aspect.Executor, evaluator expr.Evaluator) rpc.Status

// dispatch resolves config and invokes the specific set of aspects necessary to service the current request
func (m *Manager) dispatch(ctx context.Context, requestBag, responseBag *attribute.MutableBag, cfgs []*cpb.Combined, invokeFunc invokeExecutorFunc) rpc.Status {
	// get a new context with the attribute bag attached
	ctx = attribute.NewContext(ctx, requestBag)

	df, _ := m.df.Load().(descriptor.Finder)
	numCfgs := len(cfgs)

	// TODO: consider implementing a fast path when there is only a single config.
	//       we don't need to schedule goroutines, we could use the incoming attribute
	//       bags without needing children & merging, etc.

	// TODO: look into pooling both result array and channel, they're created per-request and are constant size for cfg lifetime.
	results := make([]result, numCfgs)
	resultChan := make(chan result, numCfgs)

	// schedule all the work that needs to happen
	for _, cfg := range cfgs {
		c := cfg // ensure proper capture in the worker func below
		m.gp.ScheduleWork(func() {
			childRequestBag := requestBag.Child()
			childResponseBag := responseBag.Child()

			out := m.execute(ctx, c, childRequestBag, childResponseBag, df, invokeFunc)
			resultChan <- result{c, out, childResponseBag}

			childRequestBag.Done()
		})
	}

	// wait for all the work to be done or the context to be cancelled
	for i := 0; i < numCfgs; i++ {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return status.WithCancelled(fmt.Sprintf("request cancelled: %v", ctx.Err()))
			}
			return status.WithDeadlineExceeded(fmt.Sprintf("deadline exceeded waiting for adapter results: %v", ctx.Err()))
		case res := <-resultChan:
			results[i] = res
		}
	}

	// TODO: look into having a pool of these to avoid frequent allocs
	bags := make([]*attribute.MutableBag, numCfgs)
	for i, r := range results {
		bags[i] = r.responseBag
	}

	if err := responseBag.Merge(bags...); err != nil {
		glog.Errorf("Unable to merge response attributes: %v", err)
		return status.WithError(err)
	}

	for _, b := range bags {
		b.Done()
	}

	return combineResults(results)
}

// Combines a bunch of distinct result structs and turns 'em into one single rpc.Status
func combineResults(results []result) rpc.Status {
	var buf *bytes.Buffer
	code := rpc.OK

	for _, r := range results {
		if !status.IsOK(r.status) {
			if buf == nil {
				buf = pool.GetBuffer()
				// the first failure result's code becomes the result code for the output
				code = rpc.Code(r.status.Code)
			} else {
				buf.WriteString(", ")
			}
			buf.WriteString(r.cfg.String() + ":" + r.status.Message)
		}
	}

	s := status.OK
	if buf != nil {
		s = status.WithMessage(code, buf.String())
		pool.PutBuffer(buf)
	}

	return s
}

// result holds the values returned by the execution of an adapter
type result struct {
	cfg         *cpb.Combined
	status      rpc.Status
	responseBag *attribute.MutableBag
}

// execute performs action described in the combined config using the attribute bag
func (m *Manager) execute(ctx context.Context, cfg *cpb.Combined, requestBag, responseBag *attribute.MutableBag,
	df descriptor.Finder, invokeFunc invokeExecutorFunc) (out rpc.Status) {
	var mgr aspect.Manager
	var found bool

	kind, found := config.ParseKind(cfg.Aspect.Kind)
	if !found {
		return status.WithError(fmt.Errorf("invalid aspect %#v", cfg.Aspect.Kind))
	}

	mgr = m.managers[kind]
	if mgr == nil {
		return status.WithError(fmt.Errorf("could not find aspect manager %#v", cfg.Aspect.Kind))
	}

	var builder adapter.Builder
	if builder, found = m.builders.FindBuilder(cfg.Builder.Impl); !found {
		return status.WithError(fmt.Errorf("could not find registered adapter %#v", cfg.Builder.Impl))
	}

	// Both cacheGet and invokeFunc call adapter-supplied code, so we need to guard against both panicking.
	defer func() {
		if r := recover(); r != nil {
			out = status.WithError(fmt.Errorf("adapter '%s' panicked with '%v'", builder.Name(), r))
		}
	}()

	executor, err := m.cacheGet(cfg, mgr, builder, df)
	if err != nil {
		return status.WithError(err)
	}

	// TODO: plumb ctx through asp.Execute
	_ = ctx

	return invokeFunc(executor, m.mapper)
}

// cacheKey is used to cache fully constructed aspects
// These parameters are used in constructing an aspect
type cacheKey struct {
	kind             config.Kind
	impl             string
	builderParamsSHA [sha1.Size]byte
	aspectParamsSHA  [sha1.Size]byte
}

func newCacheKey(kind config.Kind, cfg *cpb.Combined) (*cacheKey, error) {
	ret := cacheKey{
		kind: kind,
		impl: cfg.Builder.GetImpl(),
	}

	//TODO pre-compute shas and store with params
	b := pool.GetBuffer()
	// use gob encoding so that we don't rely on proto marshal
	enc := gob.NewEncoder(b)

	if cfg.Builder.Params != nil {
		if err := enc.Encode(cfg.Builder.Params); err != nil {
			return nil, err
		}
		ret.builderParamsSHA = sha1.Sum(b.Bytes())
	}
	b.Reset()
	if cfg.Aspect.Params != nil {
		if err := enc.Encode(cfg.Aspect.Params); err != nil {
			return nil, err
		}

		ret.aspectParamsSHA = sha1.Sum(b.Bytes())
	}
	pool.PutBuffer(b)

	return &ret, nil
}

// cacheGet gets an aspect executor from the cache, use adapter.Manager to construct an object in case of a cache miss
func (m *Manager) cacheGet(cfg *cpb.Combined, mgr aspect.Manager, builder adapter.Builder, df descriptor.Finder) (executor aspect.Executor, err error) {
	var key *cacheKey
	if key, err = newCacheKey(mgr.Kind(), cfg); err != nil {
		return nil, err
	}

	// try fast path with read lock
	m.lock.RLock()
	executor, found := m.executorCache[*key]
	m.lock.RUnlock()

	if found {
		return executor, nil
	}

	// create an aspect
	env := newEnv(builder.Name(), m.adapterGP)

	switch m := mgr.(type) {
	case aspect.PreprocessManager:
		executor, err = m.NewPreprocessExecutor(cfg, builder, env, df)
	case aspect.CheckManager:
		executor, err = m.NewCheckExecutor(cfg, builder, env, df)
	case aspect.ReportManager:
		executor, err = m.NewReportExecutor(cfg, builder, env, df)
	case aspect.QuotaManager:
		executor, err = m.NewQuotaExecutor(cfg, builder, env, df)
	}

	if err != nil {
		return nil, err
	}

	// obtain write lock
	m.lock.Lock()

	// see if someone else beat us to it
	if other, found := m.executorCache[*key]; found {
		defer closeExecutor(executor)
		executor = other
	} else {
		// we are the first one so save the executor
		m.executorCache[*key] = executor
	}

	m.lock.Unlock()

	return executor, nil
}

func closeExecutor(executor aspect.Executor) {
	if err := executor.Close(); err != nil {
		glog.Warningf("Error closing executor: %v: %v", executor, err)
	}
}

// AspectValidatorFinder returns a BuilderValidatorFinder for aspects.
func (m *Manager) AspectValidatorFinder(kind config.Kind) (config.AspectValidator, bool) {
	c := m.managers[kind]
	return c, c != nil
}

// BuilderValidatorFinder returns a BuilderValidatorFinder for builders.
func (m *Manager) BuilderValidatorFinder(name string) (adapter.ConfigValidator, bool) {
	return m.builders.FindBuilder(name)
}

// SupportedKinds returns the set of aspect kinds supported by the builder
func (m *Manager) SupportedKinds(builder string) config.KindSet {
	return m.builders.SupportedKinds(builder)
}

// Aspects returns a fully constructed manager table, indexed by config.Kind.
func Aspects(inventory aspect.ManagerInventory) [config.NumKinds]aspect.Manager {
	r := [config.NumKinds]aspect.Manager{}

	for _, m := range inventory.Check {
		r[m.Kind()] = m
	}

	for _, m := range inventory.Report {
		r[m.Kind()] = m
	}

	for _, m := range inventory.Quota {
		r[m.Kind()] = m
	}

	for _, m := range inventory.Preprocess {
		r[m.Kind()] = m
	}

	return r
}

// ConfigChange listens for config change notifications.
func (m *Manager) ConfigChange(cfg config.Resolver, df descriptor.Finder) {
	m.cfg.Store(cfg)
	m.df.Store(df)
}
