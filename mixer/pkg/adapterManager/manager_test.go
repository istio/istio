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
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

type (
	fakeBuilderReg struct {
		adp   adapter.Builder
		found bool
		kinds config.KindSet
	}

	fakePreprocessMgr struct {
		aspect.Manager
		kind   config.Kind
		pe     aspect.PreprocessExecutor
		called int8
	}

	fakeCheckAspectMgr struct {
		aspect.Manager
		kind   config.Kind
		ce     aspect.CheckExecutor
		called int8
	}

	fakeReportAspectMgr struct {
		aspect.Manager
		kind   config.Kind
		re     aspect.ReportExecutor
		called int8
	}

	fakeQuotaAspectMgr struct {
		aspect.Manager
		kind   config.Kind
		qe     aspect.QuotaExecutor
		called int8
	}

	fakeEvaluator struct {
		expr.Evaluator
	}

	testManager struct {
		name     string
		throw    bool
		instance testAspect
	}

	// implements adapter.Builder too.
	testAspect struct {
		body func() rpc.Status
	}

	fakePreprocessExecutor struct {
		called int8
	}

	fakeCheckExecutor struct {
		called int8
	}

	fakeReportExecutor struct {
		called int8
	}

	fakeQuotaExecutor struct {
		called int8
		result aspect.QuotaMethodResp
	}

	fakeBuilder struct {
		name string
		adapter.Builder
	}

	fakeResolver struct {
		ret []*cpb.Combined
		err error
	}
)

func (f *fakeResolver) Resolve(attribute.Bag, config.KindSet, bool) ([]*cpb.Combined, error) {
	return f.ret, f.err
}

func (f *fakeResolver) ResolveUnconditional(attribute.Bag, config.KindSet, bool) ([]*cpb.Combined, error) {
	return f.ret, f.err
}

func (f *fakeBuilder) Name() string { return f.name }
func (fakeBuilder) NewAccessLogsAspect(adapter.Env, adapter.Config) (adapter.AccessLogsAspect, error) {
	return nil, nil
}
func (fakeBuilder) NewDenialsAspect(adapter.Env, adapter.Config) (adapter.DenialsAspect, error) {
	return nil, nil
}
func (fakeBuilder) BuildAttributesGenerator(adapter.Env, adapter.Config) (adapter.AttributesGenerator, error) {
	return nil, nil
}
func (fakeBuilder) NewQuotasAspect(adapter.Env, adapter.Config, map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return nil, nil
}

func (f *fakePreprocessExecutor) Execute(attribute.Bag, expr.Evaluator) (*aspect.PreprocessResult, rpc.Status) {
	f.called++
	return &aspect.PreprocessResult{}, status.OK
}
func (f *fakePreprocessExecutor) Close() error { return nil }

func (f *fakeCheckExecutor) Execute(attribute.Bag, expr.Evaluator) (output rpc.Status) {
	f.called++
	return
}
func (f *fakeCheckExecutor) Close() error { return nil }

func (f *fakeReportExecutor) Execute(attribute.Bag, expr.Evaluator) (output rpc.Status) {
	f.called++
	return
}
func (f *fakeReportExecutor) Close() error { return nil }

func (f *fakeQuotaExecutor) Execute(attribute.Bag, expr.Evaluator, *aspect.QuotaMethodArgs) (output rpc.Status, qmr *aspect.QuotaMethodResp) {
	f.called++
	return status.OK, &f.result
}
func (f *fakeQuotaExecutor) Close() error { return nil }

func (m *fakePreprocessMgr) Kind() config.Kind {
	return m.kind
}

func (m *fakePreprocessMgr) NewPreprocessExecutor(
	c *cpb.Combined, _ aspect.CreateAspectFunc, e adapter.Env, _ descriptor.Finder) (aspect.PreprocessExecutor, error) {
	m.called++
	if m.pe == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.pe, nil
}

func (m *fakeCheckAspectMgr) Kind() config.Kind {
	return m.kind
}

func (m *fakeCheckAspectMgr) NewCheckExecutor(
	cfg *cpb.Combined, _ aspect.CreateAspectFunc, env adapter.Env, _ descriptor.Finder, _ string) (aspect.CheckExecutor, error) {
	m.called++
	if m.ce == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.ce, nil
}

func (m *fakeReportAspectMgr) Kind() config.Kind {
	return m.kind
}

func (m *fakeReportAspectMgr) NewReportExecutor(
	cfg *cpb.Combined, _ aspect.CreateAspectFunc, env adapter.Env, _ descriptor.Finder, _ string) (aspect.ReportExecutor, error) {
	m.called++
	if m.re == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.re, nil
}

func (m *fakeQuotaAspectMgr) Kind() config.Kind {
	return m.kind
}

func (m *fakeQuotaAspectMgr) NewQuotaExecutor(
	cfg *cpb.Combined, _ aspect.CreateAspectFunc, env adapter.Env, _ descriptor.Finder, _ string) (aspect.QuotaExecutor, error) {
	m.called++
	if m.qe == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.qe, nil
}

func newTestManager(name string, throwOnNewAspect bool, body func() rpc.Status) testManager {
	return testManager{name, throwOnNewAspect, testAspect{body}}
}
func (testManager) Close() error                       { return nil }
func (testManager) DefaultConfig() config.AspectParams { return nil }
func (testManager) ValidateConfig(config.AspectParams, expr.TypeChecker, descriptor.Finder) *adapter.ConfigErrors {
	return nil
}
func (testManager) Kind() config.Kind   { return config.DenialsKind }
func (m testManager) Name() string      { return m.name }
func (testManager) Description() string { return "deny checker aspect manager for testing" }

func (m testManager) NewCheckExecutor(*cpb.Combined, adapter.Builder, adapter.Env, descriptor.Finder) (aspect.CheckExecutor, error) {
	if m.throw {
		panic("NewCheckExecutor panic")
	}
	return m.instance, nil
}

func (m testManager) NewDenyChecker(adapter.Env, adapter.Config) (adapter.DenialsAspect, error) {
	return m.instance, nil
}

func (testAspect) Close() error { return nil }
func (t testAspect) Execute(attribute.Bag, expr.Evaluator) rpc.Status {
	return t.body()
}
func (testAspect) Deny() rpc.Status                                    { return rpc.Status{Code: int32(rpc.INTERNAL)} }
func (testAspect) DefaultConfig() adapter.Config                       { return nil }
func (testAspect) ValidateConfig(adapter.Config) *adapter.ConfigErrors { return nil }
func (testAspect) Name() string                                        { return "" }
func (testAspect) Description() string                                 { return "" }
func (testAspect) NewDenialsAspect(adapter.Env, adapter.Config) (adapter.DenialsAspect, error) {
	return nil, nil
}

func (m *fakeBuilderReg) FindBuilder(string) (adapter.Builder, bool) {
	return m.adp, m.found
}

func (m *fakeBuilderReg) SupportedKinds(string) config.KindSet {
	return m.kinds
}

func getReg(found bool) *fakeBuilderReg {
	var ks config.KindSet
	return &fakeBuilderReg{&fakeBuilder{name: "k1impl1"}, found,
		ks.Set(config.AttributesKind).Set(config.DenialsKind).Set(config.AccessLogsKind).Set(config.QuotasKind),
	}
}

func newFakeMgrReg(pe *fakePreprocessExecutor, ce *fakeCheckExecutor, re *fakeReportExecutor, qe *fakeQuotaExecutor) [config.NumKinds]aspect.Manager {
	var f0 aspect.PreprocessManager
	var f1 aspect.CheckManager
	var f2 aspect.ReportManager
	var f3 aspect.QuotaManager

	f0 = &fakePreprocessMgr{kind: config.AttributesKind, pe: pe}
	f1 = &fakeCheckAspectMgr{kind: config.DenialsKind, ce: ce}
	f2 = &fakeReportAspectMgr{kind: config.AccessLogsKind, re: re}
	f3 = &fakeQuotaAspectMgr{kind: config.QuotasKind, qe: qe}

	mgrs := []aspect.Manager{f0, f1, f2, f3}

	mreg := [config.NumKinds]aspect.Manager{}
	for _, mgr := range mgrs {
		mreg[mgr.Kind()] = mgr
	}
	return mreg
}

func TestManager(t *testing.T) {
	goodcfg := &cpb.Combined{
		Aspect:  &cpb.Aspect{Kind: config.DenialsKindName, Params: &rpc.Status{}},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: "k1impl1", Params: &rpc.Status{}},
	}

	goodcfg2 := &cpb.Combined{
		Aspect:  &cpb.Aspect{Kind: config.AccessLogsKindName, Params: &rpc.Status{}},
		Builder: &cpb.Adapter{Kind: config.AccessLogsKindName, Impl: "k1impl2", Params: &rpc.Status{}},
	}

	handlerName := "not in builder registry"
	handlercfg := &cpb.Combined{
		Aspect:  &cpb.Aspect{Kind: config.DenialsKindName, Params: &rpc.Status{}},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: handlerName, Params: &rpc.Status{}},
	}

	badcfg1 := &cpb.Combined{
		Aspect: &cpb.Aspect{Kind: config.DenialsKindName, Params: &rpc.Status{}},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: "k1impl1",
			Params: make(chan int)},
	}
	badcfg2 := &cpb.Combined{
		Aspect: &cpb.Aspect{Kind: config.DenialsKindName, Params: make(chan int)},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: "k1impl1",
			Params: &rpc.Status{}},
	}
	emptyMgrs := [config.NumKinds]aspect.Manager{}
	requestBag := attribute.GetMutableBag(nil)
	responseBag := attribute.GetMutableBag(nil)
	mapper := &fakeEvaluator{}

	ttt := []struct {
		mgrFound      bool
		kindFound     bool
		errString     string
		checkExecutor *fakeCheckExecutor
		cfg           []*cpb.Combined
	}{
		{false, false, "could not find aspect manager", nil, []*cpb.Combined{goodcfg}},
		{true, false, "could not find registered adapter", nil, []*cpb.Combined{goodcfg}},
		{true, true, "", &fakeCheckExecutor{}, []*cpb.Combined{goodcfg, goodcfg2}},
		{true, true, "", nil, []*cpb.Combined{goodcfg}},
		{true, true, "non-proto cfg.Builder.Params", nil, []*cpb.Combined{badcfg1}},
		{true, true, "non-proto cfg.Aspect.Params", nil, []*cpb.Combined{badcfg2}},
		{true, false, "", &fakeCheckExecutor{}, []*cpb.Combined{handlercfg}},
	}

	for idx, tt := range ttt {
		r := getReg(tt.kindFound)
		mgr := emptyMgrs
		if tt.mgrFound {
			mgr = newFakeMgrReg(nil, tt.checkExecutor, nil, nil)
		}

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(r, mgr, mapper, aspect.ManagerInventory{}, gp, agp)

		m.resolver.Store(&fakeResolver{tt.cfg, nil})
		m.handlers.Store(map[string]*config.HandlerInfo{
			handlerName: {Instance: &fakeBuilder{name: "handlerName"}, Name: handlerName},
		})

		out := m.Check(context.Background(), requestBag, responseBag)
		errStr := out.Message
		if !strings.Contains(errStr, tt.errString) {
			t.Errorf("[%d] expected: '%s' \ngot: '%s'", idx, tt.errString, errStr)
		}

		if tt.errString != "" || tt.checkExecutor == nil {
			continue
		}

		if tt.checkExecutor.called != 1 {
			t.Errorf("[%d] Expected executor to have been called once, got %d calls", idx, tt.checkExecutor.called)
		}

		mgr1 := mgr[config.DenialsKind].(aspect.CheckManager)
		fmgr := mgr1.(*fakeCheckAspectMgr)
		if fmgr.called != 1 {
			t.Errorf("[%d] Expected mgr.NewExecutor called 1, got %d calls", idx, fmgr.called)
		}

		// call again
		// check for cache
		_ = m.Check(context.Background(), requestBag, responseBag)
		if tt.checkExecutor.called != 2 {
			t.Errorf("[%d] Expected executor to have been called twice, got %d calls", idx, tt.checkExecutor.called)
		}

		if fmgr.called != 1 {
			t.Errorf("[%d] Unexpected mgr.NewExecutor call %d", idx, fmgr.called)
		}

		gp.Close()
		agp.Close()
	}
}

func TestManager_Preprocess(t *testing.T) {
	r := getReg(true)
	requestBag := attribute.GetMutableBag(nil)
	responseBag := attribute.GetMutableBag(nil)
	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	mapper := &fakeEvaluator{}
	pe := &fakePreprocessExecutor{}
	mgrs := newFakeMgrReg(pe, nil, nil, nil)

	m := newManager(r, mgrs, mapper, aspect.ManagerInventory{}, gp, agp)

	cfg := []*cpb.Combined{
		{
			Aspect:  &cpb.Aspect{Kind: config.AttributesKindName},
			Builder: &cpb.Adapter{Name: "Foo"},
		},
	}

	m.resolver.Store(&fakeResolver{cfg, nil})

	out := m.Preprocess(context.Background(), requestBag, responseBag)

	if !status.IsOK(out) {
		t.Errorf("Preprocess failed with %v", out)
	}

	if pe.called != 1 {
		t.Errorf("Executor invoked %d times, want: 0", pe.called)
	}

	gp.Close()
	agp.Close()
}

func TestReport(t *testing.T) {
	r := getReg(true)
	requestBag := attribute.GetMutableBag(nil)
	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	mapper := &fakeEvaluator{}
	re := &fakeReportExecutor{}
	mgrs := newFakeMgrReg(nil, nil, re, nil)

	m := newManager(r, mgrs, mapper, aspect.ManagerInventory{}, gp, agp)

	cfg := []*cpb.Combined{
		{
			Aspect:  &cpb.Aspect{Kind: config.AccessLogsKindName},
			Builder: &cpb.Adapter{Name: "Foo"},
		},
	}
	m.resolver.Store(&fakeResolver{cfg, nil})

	out := m.Report(context.Background(), requestBag)

	if !status.IsOK(out) {
		t.Errorf("Report failed with %v", out)
	}

	if re.called != 1 {
		t.Errorf("Executor invoked %d times, expected once", re.called)
	}

	gp.Close()
	agp.Close()
}

func TestQuota(t *testing.T) {
	r := getReg(true)
	requestBag := attribute.GetMutableBag(nil)
	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	mapper := &fakeEvaluator{}
	qe := &fakeQuotaExecutor{result: aspect.QuotaMethodResp{Amount: 42}}
	mgrs := newFakeMgrReg(nil, nil, nil, qe)

	m := newManager(r, mgrs, mapper, aspect.ManagerInventory{}, gp, agp)

	cfg := []*cpb.Combined{
		{
			Aspect:  &cpb.Aspect{Kind: config.QuotasKindName},
			Builder: &cpb.Adapter{Name: "Foo"},
		},
	}
	m.resolver.Store(&fakeResolver{cfg, nil})

	qmr, out := m.Quota(context.Background(), requestBag, nil)

	if !status.IsOK(out) {
		t.Errorf("Quota failed with %v", out)
	}

	if qe.called != 1 {
		t.Errorf("Executor invoked %d times, expected once", qe.called)
	}

	if qmr.Amount != 42 {
		t.Errorf("Got quota amount of %d, expecting 42", qmr.Amount)
	}

	gp.Close()
	agp.Close()
}

func TestManager_BulkExecute(t *testing.T) {
	goodcfg := &cpb.Combined{
		Aspect:  &cpb.Aspect{Kind: config.DenialsKindName, Params: &rpc.Status{}},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: "k1impl1", Params: &rpc.Status{}},
	}
	badcfg1 := &cpb.Combined{
		Aspect: &cpb.Aspect{Kind: config.DenialsKindName, Params: &rpc.Status{}},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: "k1impl1",
			Params: make(chan int)},
	}
	badcfg2 := &cpb.Combined{
		Aspect: &cpb.Aspect{Kind: config.DenialsKindName, Params: make(chan int)},
		Builder: &cpb.Adapter{Kind: config.DenialsKindName, Impl: "k1impl1",
			Params: &rpc.Status{}},
	}

	cases := []struct {
		errString string
		cfgs      []*cpb.Combined
	}{
		{"", []*cpb.Combined{}},
		{"", []*cpb.Combined{goodcfg}},
		{"", []*cpb.Combined{goodcfg, goodcfg}},
		{"non-proto cfg.Builder.Params", []*cpb.Combined{badcfg1, goodcfg}},
		{"non-proto cfg.Aspect.Params", []*cpb.Combined{goodcfg, badcfg2}},
	}

	requestBag := attribute.GetMutableBag(nil)
	responseBag := attribute.GetMutableBag(nil)
	mapper := &fakeEvaluator{}
	for idx, c := range cases {
		r := getReg(true)
		mgr := newFakeMgrReg(&fakePreprocessExecutor{}, &fakeCheckExecutor{}, &fakeReportExecutor{}, &fakeQuotaExecutor{})

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(r, mgr, mapper, aspect.ManagerInventory{}, gp, agp)

		m.resolver.Store(&fakeResolver{c.cfgs, nil})

		out := m.Check(context.Background(), requestBag, responseBag)
		errStr := out.Message
		if !strings.Contains(errStr, c.errString) {
			t.Errorf("[%d] got: '%s' want: '%s'", idx, errStr, c.errString)
		}

		gp.Close()
		agp.Close()
	}
}

func TestRecovery_NewAspect(t *testing.T) {
	testRecovery(t, "NewExecutor Throws", "NewExecutor")
}

func TestRecovery_AspectExecute(t *testing.T) {
	testRecovery(t, "aspect.Execute Throws", "Execute")
}

func testRecovery(t *testing.T, name string, want string) {
	cacheThrow := newTestManager(name, true, func() rpc.Status {
		return status.WithError(errors.New("empty"))
	})
	mreg := [config.NumKinds]aspect.Manager{}
	mreg[config.DenialsKind] = cacheThrow
	breg := &fakeBuilderReg{
		adp:   cacheThrow.instance,
		found: true,
	}

	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	m := newManager(breg, mreg, nil, aspect.ManagerInventory{}, gp, agp)

	cfg := []*cpb.Combined{
		{
			Builder: &cpb.Adapter{Name: name},
			Aspect:  &cpb.Aspect{Kind: name},
		},
	}
	m.resolver.Store(&fakeResolver{cfg, nil})

	out := m.Check(context.Background(), attribute.GetMutableBag(nil), attribute.GetMutableBag(nil))
	if status.IsOK(out) {
		t.Error("Aspect panicked, but got no error from manager.Execute")
	}

	if !strings.Contains(out.Message, want) {
		t.Errorf("Expected err from panic with message containing '%s', got: %v", want, out.Message)
	}

	gp.Close()
	agp.Close()
}

func TestExecute(t *testing.T) {
	cases := []struct {
		name     string
		inCode   rpc.Code
		inErr    error
		wantCode rpc.Code
	}{
		{config.DenialsKindName, rpc.OK, nil, rpc.OK},
		{"error", rpc.UNKNOWN, errors.New("expected"), rpc.UNKNOWN},
	}

	for i, c := range cases {
		mngr := newTestManager(c.name, false, func() rpc.Status {
			return status.New(c.inCode)
		})
		mreg := [config.NumKinds]aspect.Manager{}
		mreg[config.DenialsKind] = mngr
		breg := &fakeBuilderReg{
			adp:   mngr.instance,
			found: true,
		}

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(breg, mreg, nil, aspect.ManagerInventory{}, gp, agp)

		cfg := []*cpb.Combined{
			{&cpb.Adapter{Name: c.name}, &cpb.Aspect{Kind: c.name}, nil},
		}
		m.resolver.Store(&fakeResolver{cfg, nil})

		o := m.dispatch(context.Background(), attribute.GetMutableBag(nil), attribute.GetMutableBag(nil), cfg,
			func(executor aspect.Executor, evaluator expr.Evaluator, _ attribute.Bag, _ *attribute.MutableBag) rpc.Status {
				return status.OK
			})
		if c.inErr != nil && status.IsOK(o) {
			t.Errorf("m.dispatch(...) want err: %v", c.inErr)
		}
		if c.inErr == nil && !status.IsOK(o) {
			t.Errorf("m.dispatch(...) = %v; wanted o.Code == rpc.OK, case %d", o, i)
		}

		gp.Close()
		agp.Close()
	}
}

func TestExecute_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gp := pool.NewGoroutinePool(128, true)
	gp.AddWorkers(32)

	agp := pool.NewGoroutinePool(128, true)
	agp.AddWorkers(32)

	m := &Manager{gp: gp, adapterGP: agp}

	cfg := []*cpb.Combined{
		{&cpb.Adapter{Name: ""}, &cpb.Aspect{Kind: ""}, nil},
	}
	m.resolver.Store(&fakeResolver{cfg, nil})

	reqBag := attribute.GetMutableBag(nil)
	respBag := attribute.GetMutableBag(nil)

	if out := m.dispatch(ctx, reqBag, respBag, cfg, nil); status.IsOK(out) {
		t.Error("m.dispatch(canceledContext, ...) = _, nil; wanted any err")
	}

	gp.Close()
	agp.Close()
}

func TestExecute_TimeoutWaitingForResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	blockChan := make(chan struct{})

	name := "blocked"
	mngr := newTestManager(name, false, func() rpc.Status {
		<-blockChan
		return status.OK
	})
	mreg := [config.NumKinds]aspect.Manager{}
	mreg[config.DenialsKind] = mngr
	breg := &fakeBuilderReg{
		adp:   mngr.instance,
		found: true,
	}

	gp := pool.NewGoroutinePool(128, true)
	gp.AddWorkers(32)

	agp := pool.NewGoroutinePool(128, true)
	agp.AddWorkers(32)

	m := newManager(breg, mreg, nil, aspect.ManagerInventory{}, gp, agp)

	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	cfg := []*cpb.Combined{{
		&cpb.Adapter{Name: name},
		&cpb.Aspect{Kind: name},
		nil,
	}}
	m.resolver.Store(&fakeResolver{cfg, nil})

	if out := m.Check(ctx, attribute.GetMutableBag(nil), attribute.GetMutableBag(nil)); status.IsOK(out) {
		t.Error("handler.Execute(canceledContext, ...) = _, nil; wanted any err")
	}
	close(blockChan)

	gp.Close()
	agp.Close()
}
