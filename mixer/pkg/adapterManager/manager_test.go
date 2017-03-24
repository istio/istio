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
	configpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

type (
	fakeBuilderReg struct {
		adp   adapter.Builder
		found bool
		kinds []string
	}

	fakeCheckAspectMgr struct {
		aspect.Manager
		kind   aspect.Kind
		ce     aspect.CheckExecutor
		called int8
	}

	fakeReportAspectMgr struct {
		aspect.Manager
		kind   aspect.Kind
		re     aspect.ReportExecutor
		called int8
	}

	fakeQuotaAspectMgr struct {
		aspect.Manager
		kind   aspect.Kind
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
		ret []*configpb.Combined
		err error
	}
)

func (f *fakeResolver) Resolve(bag attribute.Bag, aspectSet config.AspectSet) ([]*configpb.Combined, error) {
	return f.ret, f.err
}

func (f *fakeBuilder) Name() string { return f.name }

func (f *fakeCheckExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) (output rpc.Status) {
	f.called++
	return
}
func (f *fakeCheckExecutor) Close() error { return nil }

func (f *fakeReportExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) (output rpc.Status) {
	f.called++
	return
}
func (f *fakeReportExecutor) Close() error { return nil }

func (f *fakeQuotaExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator, qma *aspect.QuotaMethodArgs) (output rpc.Status, qmr *aspect.QuotaMethodResp) {
	f.called++
	return status.OK, &f.result
}
func (f *fakeQuotaExecutor) Close() error { return nil }

func (m *fakeCheckAspectMgr) Kind() aspect.Kind {
	return m.kind
}

func (m *fakeCheckAspectMgr) NewCheckExecutor(cfg *configpb.Combined, adp adapter.Builder, env adapter.Env, _ descriptor.Finder) (aspect.CheckExecutor, error) {
	m.called++
	if m.ce == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.ce, nil
}

func (m *fakeReportAspectMgr) Kind() aspect.Kind {
	return m.kind
}

func (m *fakeReportAspectMgr) NewReportExecutor(cfg *configpb.Combined, adp adapter.Builder,
	env adapter.Env, _ descriptor.Finder) (aspect.ReportExecutor, error) {

	m.called++
	if m.re == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.re, nil
}

func (m *fakeQuotaAspectMgr) Kind() aspect.Kind {
	return m.kind
}

func (m *fakeQuotaAspectMgr) NewQuotaExecutor(cfg *configpb.Combined, adp adapter.Builder, env adapter.Env, _ descriptor.Finder) (aspect.QuotaExecutor, error) {
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
func (testManager) ValidateConfig(config.AspectParams, expr.Validator, descriptor.Finder) *adapter.ConfigErrors {
	return nil
}
func (testManager) Kind() aspect.Kind   { return aspect.DenialsKind }
func (m testManager) Name() string      { return m.name }
func (testManager) Description() string { return "deny checker aspect manager for testing" }

func (m testManager) NewCheckExecutor(cfg *configpb.Combined, adapter adapter.Builder, env adapter.Env, _ descriptor.Finder) (aspect.CheckExecutor, error) {
	if m.throw {
		panic("NewCheckExecutor panic")
	}
	return m.instance, nil
}

func (m testManager) NewDenyChecker(env adapter.Env, c adapter.Config) (adapter.DenialsAspect, error) {
	return m.instance, nil
}

func (testAspect) Close() error { return nil }
func (t testAspect) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	return t.body()
}
func (testAspect) Deny() rpc.Status                                    { return rpc.Status{Code: int32(rpc.INTERNAL)} }
func (testAspect) DefaultConfig() adapter.Config                       { return nil }
func (testAspect) ValidateConfig(adapter.Config) *adapter.ConfigErrors { return nil }
func (testAspect) Name() string                                        { return "" }
func (testAspect) Description() string                                 { return "" }

func (m *fakeBuilderReg) FindBuilder(adapterName string) (adapter.Builder, bool) {
	return m.adp, m.found
}

func (m *fakeBuilderReg) SupportedKinds(name string) []string {
	return m.kinds
}

func getReg(found bool) *fakeBuilderReg {
	return &fakeBuilderReg{&fakeBuilder{name: "k1impl1"}, found, []string{
		aspect.DenialsKind.String(),
		aspect.AccessLogsKind.String(),
		aspect.QuotasKind.String(),
	}}
}

func newFakeMgrReg(ce *fakeCheckExecutor, re *fakeReportExecutor, qe *fakeQuotaExecutor) map[aspect.Kind]aspect.Manager {
	var f1 aspect.CheckManager
	var f2 aspect.ReportManager
	var f3 aspect.QuotaManager

	f1 = &fakeCheckAspectMgr{kind: aspect.DenialsKind, ce: ce}
	f2 = &fakeReportAspectMgr{kind: aspect.AccessLogsKind, re: re}
	f3 = &fakeQuotaAspectMgr{kind: aspect.QuotasKind, qe: qe}

	mgrs := []aspect.Manager{f1, f2, f3}

	mreg := make(map[aspect.Kind]aspect.Manager, len(mgrs))
	for _, mgr := range mgrs {
		mreg[mgr.Kind()] = mgr
	}
	return mreg
}

func TestManager(t *testing.T) {
	goodcfg := &configpb.Combined{
		Aspect:  &configpb.Aspect{Kind: aspect.DenialsKindName, Params: &rpc.Status{}},
		Builder: &configpb.Adapter{Kind: aspect.DenialsKindName, Impl: "k1impl1", Params: &rpc.Status{}},
	}

	goodcfg2 := &configpb.Combined{
		Aspect:  &configpb.Aspect{Kind: aspect.AccessLogsKindName, Params: &rpc.Status{}},
		Builder: &configpb.Adapter{Kind: aspect.AccessLogsKindName, Impl: "k1impl2", Params: &rpc.Status{}},
	}

	badcfg1 := &configpb.Combined{
		Aspect: &configpb.Aspect{Kind: aspect.DenialsKindName, Params: &rpc.Status{}},
		Builder: &configpb.Adapter{Kind: aspect.DenialsKindName, Impl: "k1impl1",
			Params: make(chan int)},
	}
	badcfg2 := &configpb.Combined{
		Aspect: &configpb.Aspect{Kind: aspect.DenialsKindName, Params: make(chan int)},
		Builder: &configpb.Adapter{Kind: aspect.DenialsKindName, Impl: "k1impl1",
			Params: &rpc.Status{}},
	}
	emptyMgrs := map[aspect.Kind]aspect.Manager{}
	requestBag := attribute.GetMutableBag(nil)
	responseBag := attribute.GetMutableBag(nil)
	mapper := &fakeEvaluator{}

	ttt := []struct {
		mgrFound      bool
		kindFound     bool
		errString     string
		checkExecutor *fakeCheckExecutor
		cfg           []*configpb.Combined
	}{
		{false, false, "could not find aspect manager", nil, []*configpb.Combined{goodcfg}},
		{true, false, "could not find registered adapter", nil, []*configpb.Combined{goodcfg}},
		{true, true, "", &fakeCheckExecutor{}, []*configpb.Combined{goodcfg, goodcfg2}},
		{true, true, "", nil, []*configpb.Combined{goodcfg}},
		{true, true, "can't handle type", nil, []*configpb.Combined{badcfg1}},
		{true, true, "can't handle type", nil, []*configpb.Combined{badcfg2}},
	}

	for idx, tt := range ttt {
		r := getReg(tt.kindFound)
		mgr := emptyMgrs
		if tt.mgrFound {
			mgr = newFakeMgrReg(tt.checkExecutor, nil, nil)
		}

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(r, mgr, mapper, nil, gp, agp)

		m.cfg.Store(&fakeResolver{tt.cfg, nil})

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

		mgr1 := mgr[aspect.DenialsKind].(aspect.CheckManager)
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

func TestReport(t *testing.T) {
	r := getReg(true)
	requestBag := attribute.GetMutableBag(nil)
	responseBag := attribute.GetMutableBag(nil)
	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	mapper := &fakeEvaluator{}
	re := &fakeReportExecutor{}
	mgrs := newFakeMgrReg(nil, re, nil)

	m := newManager(r, mgrs, mapper, nil, gp, agp)

	cfg := []*configpb.Combined{
		{
			Aspect:  &configpb.Aspect{Kind: aspect.AccessLogsKindName},
			Builder: &configpb.Adapter{Name: "Foo"},
		},
	}
	m.cfg.Store(&fakeResolver{cfg, nil})

	out := m.Report(context.Background(), requestBag, responseBag)

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
	responseBag := attribute.GetMutableBag(nil)
	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	mapper := &fakeEvaluator{}
	qe := &fakeQuotaExecutor{result: aspect.QuotaMethodResp{Amount: 42}}
	mgrs := newFakeMgrReg(nil, nil, qe)

	m := newManager(r, mgrs, mapper, nil, gp, agp)

	cfg := []*configpb.Combined{
		{
			Aspect:  &configpb.Aspect{Kind: aspect.QuotasKindName},
			Builder: &configpb.Adapter{Name: "Foo"},
		},
	}
	m.cfg.Store(&fakeResolver{cfg, nil})

	qmr, out := m.Quota(context.Background(), requestBag, responseBag, nil)

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
	goodcfg := &configpb.Combined{
		Aspect:  &configpb.Aspect{Kind: aspect.DenialsKindName, Params: &rpc.Status{}},
		Builder: &configpb.Adapter{Kind: aspect.DenialsKindName, Impl: "k1impl1", Params: &rpc.Status{}},
	}
	badcfg1 := &configpb.Combined{
		Aspect: &configpb.Aspect{Kind: aspect.DenialsKindName, Params: &rpc.Status{}},
		Builder: &configpb.Adapter{Kind: aspect.DenialsKindName, Impl: "k1impl1",
			Params: make(chan int)},
	}
	badcfg2 := &configpb.Combined{
		Aspect: &configpb.Aspect{Kind: aspect.DenialsKindName, Params: make(chan int)},
		Builder: &configpb.Adapter{Kind: aspect.DenialsKindName, Impl: "k1impl1",
			Params: &rpc.Status{}},
	}

	cases := []struct {
		errString string
		cfgs      []*configpb.Combined
	}{
		{"", []*configpb.Combined{}},
		{"", []*configpb.Combined{goodcfg}},
		{"", []*configpb.Combined{goodcfg, goodcfg}},
		{"can't handle type", []*configpb.Combined{badcfg1, goodcfg}},
		{"can't handle type", []*configpb.Combined{goodcfg, badcfg2}},
	}

	requestBag := attribute.GetMutableBag(nil)
	responseBag := attribute.GetMutableBag(nil)
	mapper := &fakeEvaluator{}
	for idx, c := range cases {
		r := getReg(true)
		mgr := newFakeMgrReg(&fakeCheckExecutor{}, &fakeReportExecutor{}, &fakeQuotaExecutor{})

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(r, mgr, mapper, nil, gp, agp)

		m.cfg.Store(&fakeResolver{c.cfgs, nil})

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
	testRecovery(t, "NewExecutor Throws", true, false, "NewExecutor")
}

func TestRecovery_AspectExecute(t *testing.T) {
	testRecovery(t, "aspect.Execute Throws", true, false, "Execute")
}

func testRecovery(t *testing.T, name string, throwOnNewAspect bool, throwOnExecute bool, want string) {
	var cacheThrow testManager
	if throwOnExecute {
		cacheThrow = newTestManager(name, throwOnNewAspect, func() rpc.Status {
			panic("panic")
		})
	} else {
		cacheThrow = newTestManager(name, throwOnNewAspect, func() rpc.Status {
			return status.WithError(errors.New("empty"))
		})
	}
	mreg := map[aspect.Kind]aspect.Manager{
		aspect.DenialsKind: cacheThrow,
	}
	breg := &fakeBuilderReg{
		adp:   cacheThrow.instance,
		found: true,
	}

	gp := pool.NewGoroutinePool(1, true)
	agp := pool.NewGoroutinePool(1, true)
	m := newManager(breg, mreg, nil, nil, gp, agp)

	cfg := []*configpb.Combined{
		{
			Builder: &configpb.Adapter{Name: name},
			Aspect:  &configpb.Aspect{Kind: name},
		},
	}
	m.cfg.Store(&fakeResolver{cfg, nil})

	out := m.Check(context.Background(), nil, nil)
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
		{aspect.DenialsKindName, rpc.OK, nil, rpc.OK},
		{"error", rpc.UNKNOWN, errors.New("expected"), rpc.UNKNOWN},
	}

	for i, c := range cases {
		mngr := newTestManager(c.name, false, func() rpc.Status {
			return status.New(c.inCode)
		})
		mreg := map[aspect.Kind]aspect.Manager{
			aspect.DenialsKind: mngr,
		}
		breg := &fakeBuilderReg{
			adp:   mngr.instance,
			found: true,
		}

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(breg, mreg, nil, nil, gp, agp)

		cfg := []*configpb.Combined{
			{&configpb.Adapter{Name: c.name}, &configpb.Aspect{Kind: c.name}},
		}
		m.cfg.Store(&fakeResolver{cfg, nil})

		o := m.dispatch(context.Background(), nil, nil, checkMethod,
			func(executor aspect.Executor, evaluator expr.Evaluator) rpc.Status {
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

	gp := pool.NewGoroutinePool(128, true)
	gp.AddWorkers(32)

	agp := pool.NewGoroutinePool(128, true)
	agp.AddWorkers(32)

	m := &Manager{gp: gp, adapterGP: agp}
	cancel()

	cfg := []*configpb.Combined{
		{&configpb.Adapter{Name: ""}, &configpb.Aspect{Kind: ""}},
	}
	m.cfg.Store(&fakeResolver{cfg, nil})

	if out := m.dispatch(ctx, attribute.GetMutableBag(nil), attribute.GetMutableBag(nil), checkMethod, nil); status.IsOK(out) {
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
	mreg := map[aspect.Kind]aspect.Manager{
		aspect.DenialsKind: mngr,
	}
	breg := &fakeBuilderReg{
		adp:   mngr.instance,
		found: true,
	}

	gp := pool.NewGoroutinePool(128, true)
	gp.AddWorkers(32)

	agp := pool.NewGoroutinePool(128, true)
	agp.AddWorkers(32)

	m := newManager(breg, mreg, nil, nil, gp, agp)

	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	cfg := []*configpb.Combined{{
		&configpb.Adapter{Name: name},
		&configpb.Aspect{Kind: name},
	}}
	m.cfg.Store(&fakeResolver{cfg, nil})

	if out := m.Check(ctx, attribute.GetMutableBag(nil), attribute.GetMutableBag(nil)); status.IsOK(out) {
		t.Error("handler.Execute(canceledContext, ...) = _, nil; wanted any err")
	}
	close(blockChan)

	gp.Close()
	agp.Close()
}
