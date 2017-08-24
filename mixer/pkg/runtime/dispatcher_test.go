// Copyright 2017 Istio Authors
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

package runtime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	google_rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	adptTmpl "istio.io/mixer/pkg/adapter/template"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/template"
)

func TestReport(t *testing.T) {
	gp := pool.NewGoroutinePool(1, true)
	tname := "metric1"
	err1 := errors.New("internal error")

	for _, s := range []struct {
		tn         string
		callErr    error
		resolveErr bool
		ncalled    int
	}{{tn: tname, ncalled: 3},
		{tn: tname, callErr: err1},
		{tn: tname, callErr: err1, resolveErr: true},
	} {
		t.Run(fmt.Sprintf("%#v", s), func(t *testing.T) {
			fp := &fakeProc{
				err: s.callErr,
			}
			var resolveErr error
			if s.resolveErr {
				resolveErr = s.callErr
			}
			rt := newFakeResolver(s.tn, resolveErr, false, fp)
			m := newDispatcher(nil, rt, gp)

			err := m.Report(context.Background(), nil)
			checkError(t, s.callErr, err)
			if s.callErr != nil {
				return
			}
			if fp.called != s.ncalled {
				t.Errorf("got %v, want %v", fp.called, s.ncalled)
			}
		})
	}
	gp.Close()
}

func TestCheck(t *testing.T) {
	gp := pool.NewGoroutinePool(1, true)
	tname := "metric1"
	err1 := errors.New("internal error")

	for _, s := range []struct {
		tn         string
		callErr    error
		resolveErr bool
		ncalled    int
		cr         adapter.CheckResult
	}{{tn: tname, ncalled: 6},
		{tn: tname, ncalled: 6, cr: adapter.CheckResult{ValidUseCount: 200}},
		{tn: tname, ncalled: 6, cr: adapter.CheckResult{ValidUseCount: 200, Status: status.WithPermissionDenied("bad user")}},
		{tn: tname, callErr: err1},
		{tn: tname, callErr: err1, resolveErr: true},
	} {
		t.Run(fmt.Sprintf("%#v", s), func(t *testing.T) {
			fp := &fakeProc{
				err:         s.callErr,
				checkResult: s.cr,
			}
			var resolveErr error
			if s.resolveErr {
				resolveErr = s.callErr
			}
			rt := newFakeResolver(s.tn, resolveErr, false, fp)
			m := newDispatcher(nil, rt, gp)

			cr, err := m.Check(context.Background(), nil)

			checkError(t, s.callErr, err)

			if s.callErr != nil {
				return
			}
			if fp.called != s.ncalled {
				t.Fatalf("got %v, want %v", fp.called, s.ncalled)
			}
			if s.ncalled == 0 {
				return
			}
			if cr == nil {
				t.Fatalf("got %v, want %v", cr, fp.checkResult)
			}
			if !reflect.DeepEqual(fp.checkResult.Status.Code, cr.Status.Code) {
				t.Fatalf("got %v, want %v", *cr, fp.checkResult)
			}
		})
	}
	gp.Close()
}

func TestQuota(t *testing.T) {
	gp := pool.NewGoroutinePool(1, true)
	tname := "metric1"
	err1 := errors.New("internal error")

	for _, s := range []struct {
		tn          string
		callErr     error
		resolveErr  bool
		ncalled     int
		cr          adapter.QuotaResult2
		emptyResult bool
	}{{tn: tname, ncalled: 1},
		{tn: tname, ncalled: 1, cr: adapter.QuotaResult2{Amount: 200}},
		{tn: tname, ncalled: 1, cr: adapter.QuotaResult2{Amount: 200, Status: status.WithPermissionDenied("bad user")}},
		{tn: tname, callErr: err1},
		{tn: tname, callErr: err1, resolveErr: true},
		{tn: tname, ncalled: 0, cr: adapter.QuotaResult2{Amount: 200}, emptyResult: true},
	} {
		t.Run(fmt.Sprintf("%#v", s), func(t *testing.T) {
			fp := &fakeProc{
				err:         s.callErr,
				quotaResult: s.cr,
			}
			var resolveErr error
			if s.resolveErr {
				resolveErr = s.callErr
			}
			rt := newFakeResolver(s.tn, resolveErr, s.emptyResult, fp)
			m := newDispatcher(nil, rt, gp)

			cr, err := m.Quota(context.Background(), nil,
				&aspect.QuotaMethodArgs{
					Quota: "i1",
				})

			checkError(t, s.callErr, err)

			if s.callErr != nil {
				return
			}
			if fp.called != s.ncalled {
				t.Fatalf("got %v, want %v", fp.called, s.ncalled)
			}
			if s.ncalled == 0 {
				return
			}
			if cr == nil {
				t.Fatalf("got %v, want %v", cr, fp.quotaResult)
			}
			if !reflect.DeepEqual(fp.quotaResult.Status.Code, cr.Status.Code) {
				t.Fatalf("got %v, want %v", *cr, fp.quotaResult)
			}
		})
	}

	gp.Close()
}

func TestPreprocess(t *testing.T) {
	m := dispatcher{}

	err := m.Preprocess(context.TODO(), nil, nil)
	if err == nil {
		t.Fatalf("not working yet")
	}
}

// fakes

type fakeResolver struct {
	ra  []*Action
	err error
}

func checkError(t *testing.T, want error, err error) {
	if err == nil {
		if want != nil {
			t.Fatalf("got %v, want %v", err, want)
		}
	} else {
		if want == nil {
			t.Fatalf("got %v, want %v", err, want)
		}
		if !strings.Contains(err.Error(), want.Error()) {
			t.Fatalf("got %v, want %v", err, want)
		}
	}
}

// Resolve resolves configuration to a list of actions.
func (f *fakeResolver) Resolve(bag attribute.Bag, variety adptTmpl.TemplateVariety) (Actions, error) {
	return newFa(f.ra), f.err
}

func newFa(a []*Action) Actions {
	return &fakeActions{a: a}
}

type fakeActions struct {
	a    []*Action
	done bool
}

func (a *fakeActions) Get() []*Action { return a.a }
func (a *fakeActions) Done()          { a.done = true }

var _ Resolver = &fakeResolver{}

func newFakeResolver(tname string, resolveErr error, emptyResult bool, fproc *fakeProc) *fakeResolver {
	hndlr := "myhandler"
	instanceName := "i1"

	rt := &fakeResolver{
		ra: []*Action{
			{
				processor:   newTemplate(tname, fproc),
				handlerName: hndlr,
				adapterName: hndlr + "Impl",
				instanceConfig: []*cpb.Instance{
					{
						instanceName + "B",
						tname,
						&google_rpc.Status{},
					},
					{
						instanceName,
						tname,
						&google_rpc.Status{},
					},
				},
			},
			{
				processor:   newTemplate(tname, fproc),
				handlerName: hndlr + "_A",
				adapterName: hndlr + "_AImpl",
				instanceConfig: []*cpb.Instance{
					{
						instanceName,
						tname,
						&google_rpc.Status{},
					},
					{
						instanceName + "B",
						tname,
						&google_rpc.Status{},
					},
				},
			},
			{
				processor:   newTemplate(tname, fproc),
				handlerName: hndlr + "_B",
				adapterName: hndlr + "_BImpl",
				instanceConfig: []*cpb.Instance{
					{
						instanceName + "X",
						tname,
						&google_rpc.Status{},
					},
					{
						instanceName + "Y",
						tname,
						&google_rpc.Status{},
					},
				},
			},
		},
		err: resolveErr,
	}

	if emptyResult {
		rt.ra = nil
	}

	return rt
}

func newTemplate(name string, fproc *fakeProc) *template.Info {
	return &template.Info{
		Name:          name,
		ProcessReport: fproc.ProcessReport,
		ProcessCheck:  fproc.ProcessCheck,
		ProcessQuota:  fproc.ProcessQuota,
	}
}

type fakeProc struct {
	called      int
	err         error
	checkResult adapter.CheckResult
	quotaResult adapter.QuotaResult2
}

func (f *fakeProc) ProcessReport(_ context.Context, _ map[string]proto.Message,
	_ attribute.Bag, _ expr.Evaluator, _ adapter.Handler) error {
	f.called++
	return f.err
}
func (f *fakeProc) ProcessCheck(_ context.Context, _ string, _ proto.Message, _ attribute.Bag,
	_ expr.Evaluator, _ adapter.Handler) (adapter.CheckResult, error) {
	f.called++
	return f.checkResult, f.err
}

func (f *fakeProc) ProcessQuota(_ context.Context, _ string, _ proto.Message, _ attribute.Bag,
	_ expr.Evaluator, _ adapter.Handler, _ adapter.QuotaRequestArgs) (adapter.QuotaResult2, error) {
	f.called++
	return f.quotaResult, f.err
}

var _ = flag.Lookup("v").Value.Set("99")
