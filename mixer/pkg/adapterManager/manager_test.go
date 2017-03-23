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

	fakemgr struct {
		aspect.Manager
		kind   aspect.Kind
		w      *fakewrapper
		called int8
	}

	fakeevaluator struct {
		expr.Evaluator
	}

	testManager struct {
		name     string
		throw    bool
		instance testAspect
	}

	// implements adapter.Builder too.
	testAspect struct {
		body func() aspect.Output
	}

	fakewrapper struct {
		called int8
	}

	fakeadp struct {
		name string
		adapter.Builder
	}
)

func (f *fakeadp) Name() string { return f.name }

func (f *fakewrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma aspect.APIMethodArgs) (output aspect.Output) {
	f.called++
	return
}
func (f *fakewrapper) Close() error { return nil }

func (m *fakemgr) Kind() aspect.Kind {
	return m.kind
}

func newTestManager(name string, throwOnNewAspect bool, body func() aspect.Output) testManager {
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

func (m testManager) NewAspect(cfg *configpb.Combined, adapter adapter.Builder, env adapter.Env, _ descriptor.Finder) (aspect.Wrapper, error) {
	if m.throw {
		panic("NewAspect panic")
	}
	return m.instance, nil
}
func (m testManager) NewDenyChecker(env adapter.Env, c adapter.Config) (adapter.DenialsAspect, error) {
	return m.instance, nil
}

func (testAspect) Close() error { return nil }
func (t testAspect) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma aspect.APIMethodArgs) aspect.Output {
	return t.body()
}
func (testAspect) Deny() rpc.Status                                    { return rpc.Status{Code: int32(rpc.INTERNAL)} }
func (testAspect) DefaultConfig() adapter.Config                       { return nil }
func (testAspect) ValidateConfig(adapter.Config) *adapter.ConfigErrors { return nil }
func (testAspect) Name() string                                        { return "" }
func (testAspect) Description() string                                 { return "" }

func (m *fakemgr) NewAspect(cfg *configpb.Combined, adp adapter.Builder, env adapter.Env, _ descriptor.Finder) (aspect.Wrapper, error) {
	m.called++
	if m.w == nil {
		return nil, errors.New("unable to create aspect")
	}

	return m.w, nil
}

func (m *fakeBuilderReg) FindBuilder(adapterName string) (adapter.Builder, bool) {
	return m.adp, m.found
}

func (m *fakeBuilderReg) SupportedKinds(name string) []string {
	return m.kinds
}

type ttable struct {
	mgrFound  bool
	kindFound bool
	errString string
	wrapper   *fakewrapper
	cfg       []*configpb.Combined
}

func getReg(found bool) *fakeBuilderReg {
	return &fakeBuilderReg{&fakeadp{name: "k1impl1"}, found, []string{aspect.DenialsKind.String()}}
}

func newFakeMgrReg(w *fakewrapper) map[aspect.Kind]aspect.Manager {
	mgrs := []aspect.Manager{&fakemgr{kind: aspect.DenialsKind, w: w}, &fakemgr{kind: aspect.AccessLogsKind}}
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
	mapper := &fakeevaluator{}

	ttt := []ttable{
		{false, false, "could not find aspect manager", nil, []*configpb.Combined{goodcfg}},
		{true, false, "could not find registered adapter", nil, []*configpb.Combined{goodcfg}},
		{true, true, "", &fakewrapper{}, []*configpb.Combined{goodcfg}},
		{true, true, "", nil, []*configpb.Combined{goodcfg}},
		{true, true, "can't handle type", nil, []*configpb.Combined{badcfg1}},
		{true, true, "can't handle type", nil, []*configpb.Combined{badcfg2}},
	}

	for idx, tt := range ttt {
		r := getReg(tt.kindFound)
		mgr := emptyMgrs
		if tt.mgrFound {
			mgr = newFakeMgrReg(tt.wrapper)
		}

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(r, mgr, mapper, nil, gp, agp)

		out := m.Execute(context.Background(), tt.cfg, requestBag, responseBag, nil, nil)
		errStr := out.Message()
		if !strings.Contains(errStr, tt.errString) {
			t.Errorf("[%d] expected: '%s' \ngot: '%s'", idx, tt.errString, errStr)
		}

		if tt.errString != "" || tt.wrapper == nil {
			continue
		}

		if tt.wrapper.called != 1 {
			t.Errorf("[%d] Expected wrapper call", idx)
		}
		mgr1 := mgr[aspect.DenialsKind]
		fmgr := mgr1.(*fakemgr)
		if fmgr.called != 1 {
			t.Errorf("[%d] Expected mgr.NewAspect call", idx)
		}

		// call again
		// check for cache
		_ = m.Execute(context.Background(), tt.cfg, requestBag, responseBag, nil, nil)
		if tt.wrapper.called != 2 {
			t.Errorf("[%d] Expected 2nd wrapper call", idx)
		}

		if fmgr.called != 1 {
			t.Errorf("[%d] Unexpected mgr.NewAspect call %d", idx, fmgr.called)
		}

		gp.Close()
		agp.Close()
	}
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
	mapper := &fakeevaluator{}
	for idx, c := range cases {
		r := getReg(true)
		mgr := newFakeMgrReg(&fakewrapper{})

		gp := pool.NewGoroutinePool(1, true)
		agp := pool.NewGoroutinePool(1, true)
		m := newManager(r, mgr, mapper, nil, gp, agp)

		out := m.Execute(context.Background(), c.cfgs, requestBag, responseBag, nil, nil)
		errStr := out.Message()
		if !strings.Contains(errStr, c.errString) {
			t.Errorf("[%d] got: '%s' want: '%s'", idx, c.errString, errStr)
		}

		gp.Close()
		agp.Close()
	}
}

func TestRecovery_NewAspect(t *testing.T) {
	testRecovery(t, "NewAspect Throws", true, false, "NewAspect")
}

func TestRecovery_AspectExecute(t *testing.T) {
	testRecovery(t, "aspect.Execute Throws", true, false, "Execute")
}

func testRecovery(t *testing.T, name string, throwOnNewAspect bool, throwOnExecute bool, want string) {
	var cacheThrow testManager
	if throwOnExecute {
		cacheThrow = newTestManager(name, throwOnNewAspect, func() aspect.Output {
			panic("panic")
		})
	} else {
		cacheThrow = newTestManager(name, throwOnNewAspect, func() aspect.Output {
			return aspect.Output{Status: status.WithError(errors.New("empty"))}
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

	out := m.Execute(context.Background(), cfg, nil, nil, nil, nil)
	if out.IsOK() {
		t.Error("Aspect panicked, but got no error from manager.Execute")
	}

	if !strings.Contains(out.Message(), want) {
		t.Errorf("Expected err from panic with message containing '%s', got: %v", want, out.Message())
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
		resp     aspect.APIMethodResp
	}{
		{aspect.DenialsKindName, rpc.OK, nil, rpc.OK, "RESPONSE"},
		{"error", rpc.UNKNOWN, errors.New("expected"), rpc.UNKNOWN, nil},
	}

	for _, c := range cases {
		mngr := newTestManager(c.name, false, func() aspect.Output {
			return aspect.Output{Status: status.New(c.inCode), Response: c.resp}
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

		o := m.Execute(context.Background(), cfg, nil, nil, nil, nil)
		if c.inErr != nil && o.IsOK() {
			t.Errorf("m.Execute(...) want err: %v", c.inErr)
		}
		if c.inErr == nil && !o.IsOK() {
			t.Errorf("m.Execute(...) = %v; wanted o.Status.Code == rpc.OK", o)
		}

		if c.resp != o.Response {
			t.Errorf("m.Execute(...) got response %v, expected %v", o.Response, c.resp)
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

	// we're skipping NewMethodHandlers so we don't have to deal with config since configuration shouldn't matter when we have a canceled ctx
	handler := &Manager{gp: gp, adapterGP: agp}
	cancel()

	cfg := []*configpb.Combined{
		{&configpb.Adapter{Name: ""}, &configpb.Aspect{Kind: ""}},
	}
	if out := handler.Execute(ctx, cfg, attribute.GetMutableBag(nil), attribute.GetMutableBag(nil), nil, nil); out.IsOK() {
		t.Error("handler.Execute(canceledContext, ...) = _, nil; wanted any err")
	}

	gp.Close()
	agp.Close()
}

func TestExecute_TimeoutWaitingForResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	blockChan := make(chan struct{})

	name := "blocked"
	mngr := newTestManager(name, false, func() aspect.Output {
		<-blockChan
		return aspect.Output{Status: status.OK}
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
	if out := m.Execute(ctx, cfg, attribute.GetMutableBag(nil), attribute.GetMutableBag(nil), nil, nil); out.IsOK() {
		t.Error("handler.Execute(canceledContext, ...) = _, nil; wanted any err")
	}
	close(blockChan)

	gp.Close()
	agp.Close()
}
