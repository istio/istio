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

package template

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"

	pb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	adpTmpl "istio.io/mixer/pkg/adapter/template"
	"istio.io/mixer/pkg/expr"
	sample_check "istio.io/mixer/template/sample/check"
	sample_quota "istio.io/mixer/template/sample/quota"
	sample_report "istio.io/mixer/template/sample/report"
)

// Does not implement any template interfaces.
type fakeBadHandler struct{}

func (h fakeBadHandler) Close() error { return nil }
func (h fakeBadHandler) Build(proto.Message, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}

type fakeReportHandler struct {
	adapter.Handler
	retProcError  error
	cnfgCallInput interface{}
	procCallInput interface{}
}

func (h *fakeReportHandler) Close() error { return nil }
func (h *fakeReportHandler) ReportSample(instances []*sample_report.Instance) error {
	h.procCallInput = instances
	return h.retProcError
}
func (h *fakeReportHandler) Build(proto.Message, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeReportHandler) ConfigureSample(t map[string]*sample_report.Type) error {
	h.cnfgCallInput = t
	return nil
}

type fakeCheckHandler struct {
	adapter.Handler
	retProcError  error
	cnfgCallInput interface{}
	procCallInput interface{}
	ret           bool
	retCache      adapter.CacheabilityInfo
}

func (h *fakeCheckHandler) Close() error { return nil }
func (h *fakeCheckHandler) CheckSample(instance []*sample_check.Instance) (bool, adapter.CacheabilityInfo, error) {
	h.procCallInput = instance
	return h.ret, h.retCache, h.retProcError
}
func (h *fakeCheckHandler) Build(proto.Message, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeCheckHandler) ConfigureSample(t map[string]*sample_check.Type) error {
	h.cnfgCallInput = t
	return nil
}

type fakeQuotaHandler struct {
	adapter.Handler
	retProcError  error
	cnfgCallInput interface{}
	procCallInput interface{}
	retQuotaRes   adapter.QuotaResult
	retCache      adapter.CacheabilityInfo
}

func (h *fakeQuotaHandler) Close() error { return nil }
func (h *fakeQuotaHandler) AllocQuota(instance *sample_quota.Instance, qra adapter.QuotaRequestArgs) (adapter.QuotaResult, adapter.CacheabilityInfo, error) {
	h.procCallInput = instance
	return h.retQuotaRes, h.retCache, h.retProcError
}
func (h *fakeQuotaHandler) Build(proto.Message, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeQuotaHandler) ConfigureQuota(t map[string]*sample_quota.Type) error {
	h.cnfgCallInput = t
	return nil
}

type fakeBag struct{}

func (f fakeBag) Get(name string) (value interface{}, found bool) { return nil, false }
func (f fakeBag) Names() []string                                 { return []string{} }
func (f fakeBag) Done()                                           {}

func TestGeneratedFields(t *testing.T) {
	for _, tst := range []struct {
		tmpl      string
		ctrCfg    proto.Message
		variety   adpTmpl.TemplateVariety
		bldrName  string
		hndlrName string
	}{
		{
			tmpl:      sample_report.TemplateName,
			ctrCfg:    &sample_report.InstanceParam{},
			variety:   adpTmpl.TEMPLATE_VARIETY_REPORT,
			bldrName:  "istio.io/mixer/template/sample/report.SampleProcessorBuilder",
			hndlrName: "istio.io/mixer/template/sample/report.SampleProcessor",
		},
		{
			tmpl:      sample_check.TemplateName,
			ctrCfg:    &sample_check.InstanceParam{},
			variety:   adpTmpl.TEMPLATE_VARIETY_CHECK,
			bldrName:  "istio.io/mixer/template/sample/check.SampleProcessorBuilder",
			hndlrName: "istio.io/mixer/template/sample/check.SampleProcessor",
		},
		{
			tmpl:      sample_quota.TemplateName,
			ctrCfg:    &sample_quota.InstanceParam{},
			variety:   adpTmpl.TEMPLATE_VARIETY_QUOTA,
			bldrName:  "istio.io/mixer/template/sample/quota.QuotaProcessorBuilder",
			hndlrName: "istio.io/mixer/template/sample/quota.QuotaProcessor",
		},
	} {
		t.Run(tst.tmpl, func(t *testing.T) {
			if !reflect.DeepEqual(SupportedTmplInfo[tst.tmpl].CtrCfg, tst.ctrCfg) {
				t.Errorf("SupportedTmplInfo[%s].CtrCfg = %T, want %T", tst.tmpl, SupportedTmplInfo[tst.tmpl].CtrCfg, tst.ctrCfg)
			}
			if SupportedTmplInfo[tst.tmpl].Variety != tst.variety {
				t.Errorf("SupportedTmplInfo[%s].Variety = %v, want %v", tst.tmpl, SupportedTmplInfo[tst.tmpl].Variety, tst.variety)
			}
			if SupportedTmplInfo[tst.tmpl].BldrName != tst.bldrName {
				t.Errorf("SupportedTmplInfo[%s].BldrName = %v, want %v", tst.tmpl, SupportedTmplInfo[tst.tmpl].BldrName, tst.bldrName)
			}
			if SupportedTmplInfo[tst.tmpl].HndlrName != tst.hndlrName {
				t.Errorf("SupportedTmplInfo[%s].HndlrName = %v, want %v", tst.tmpl, SupportedTmplInfo[tst.tmpl].HndlrName, tst.hndlrName)
			}
		})
	}
}

func TestHandlerSupportsTemplate(t *testing.T) {
	for _, tst := range []struct {
		tmpl   string
		hndlr  adapter.Handler
		result bool
	}{
		{
			tmpl:   sample_report.TemplateName,
			hndlr:  fakeBadHandler{},
			result: false,
		},
		{
			tmpl:   sample_report.TemplateName,
			hndlr:  &fakeReportHandler{},
			result: true,
		},
		{
			tmpl:   sample_check.TemplateName,
			hndlr:  fakeBadHandler{},
			result: false,
		},
		{
			tmpl:   sample_check.TemplateName,
			hndlr:  &fakeCheckHandler{},
			result: true,
		},
		{
			tmpl:   sample_quota.TemplateName,
			hndlr:  fakeBadHandler{},
			result: false,
		},
		{
			tmpl:   sample_quota.TemplateName,
			hndlr:  &fakeQuotaHandler{},
			result: true,
		},
	} {
		t.Run(tst.tmpl, func(t *testing.T) {
			c := SupportedTmplInfo[tst.tmpl].HandlerSupportsTemplate(tst.hndlr)
			if c != tst.result {
				t.Errorf("SupportedTmplInfo[%s].HandlerSupportsTemplate(%T) = %t, want %t", tst.tmpl, tst.hndlr, c, tst.result)
			}
		})
	}
}

func TestBuilderSupportsTemplate(t *testing.T) {
	for _, tst := range []struct {
		tmpl      string
		hndlrBldr adapter.HandlerBuilder
		result    bool
	}{
		{
			tmpl:      sample_report.TemplateName,
			hndlrBldr: fakeBadHandler{},
			result:    false,
		},
		{
			tmpl:      sample_report.TemplateName,
			hndlrBldr: &fakeReportHandler{},
			result:    true,
		},
		{
			tmpl:      sample_check.TemplateName,
			hndlrBldr: fakeBadHandler{},
			result:    false,
		},
		{
			tmpl:      sample_check.TemplateName,
			hndlrBldr: &fakeCheckHandler{},
			result:    true,
		},
		{
			tmpl:      sample_quota.TemplateName,
			hndlrBldr: fakeBadHandler{},
			result:    false,
		},
		{
			tmpl:      sample_quota.TemplateName,
			hndlrBldr: &fakeQuotaHandler{},
			result:    true,
		},
	} {
		t.Run(tst.tmpl, func(t *testing.T) {
			c := SupportedTmplInfo[tst.tmpl].SupportsTemplate(tst.hndlrBldr)
			if c != tst.result {
				t.Errorf("SupportedTmplInfo[%s].SupportsTemplate(%T) = %t, want %t", tst.tmpl, tst.hndlrBldr, c, tst.result)
			}
		})
	}
}

type inferTypeTest struct {
	name               string
	ctrCnfg            string
	cstrParam          interface{}
	typeEvalRet        pb.ValueType
	typeEvalError      error
	wantValueType      pb.ValueType
	wantDimensionsType map[string]pb.ValueType
	wantErr            string
	willPanic          bool
}

func TestInferTypeForSampleReport(t *testing.T) {
	for _, tst := range []inferTypeTest{
		{
			name: "SimpleValid",
			ctrCnfg: `
value: response.size
dimensions:
  source: source.ip
  target: source.ip
`,
			cstrParam:          &sample_report.InstanceParam{},
			typeEvalRet:        pb.INT64,
			typeEvalError:      nil,
			wantValueType:      pb.INT64,
			wantDimensionsType: map[string]pb.ValueType{"source": pb.INT64, "target": pb.INT64},
			wantErr:            "",
			willPanic:          false,
		},
		{
			name:      "NotValidInstanceParam",
			ctrCnfg:   ``,
			cstrParam: &empty.Empty{}, // cnstr type mismatch
			wantErr:   "is not of type",
			willPanic: true,
		},
		{
			name: "ErrorFromTypeEvaluator",
			ctrCnfg: `
value: response.size
dimensions:
  source: source.ip
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: fmt.Errorf("some expression x.y.z is invalid"),
			wantErr:       "some expression x.y.z is invalid",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			_ = fillProto(tst.ctrCnfg, cp)
			typeEvalFn := func(expr string) (pb.ValueType, error) { return tst.typeEvalRet, tst.typeEvalError }
			defer func() {
				r := recover()
				if tst.willPanic && r == nil {
					t.Errorf("Expected to recover from panic for %s, but recover was nil.", tst.name)
				} else if !tst.willPanic && r != nil {
					t.Errorf("got panic %v, expected success.", r)
				}
			}()
			cv, cerr := SupportedTmplInfo[sample_report.TemplateName].InferType(cp.(proto.Message), typeEvalFn)
			if tst.wantErr == "" {
				if cerr != nil {
					t.Errorf("got err %v\nwant <nil>", cerr)
				}
				if tst.wantValueType != cv.(*sample_report.Type).Value {
					t.Errorf("got inferTypeForSampleReport(\n%s\n).value=%v\nwant %v",
						tst.ctrCnfg, cv.(*sample_report.Type).Value, tst.wantValueType)
				}
				if len(tst.wantDimensionsType) != len(cv.(*sample_report.Type).Dimensions) {
					t.Errorf("got len ( inferTypeForSampleReport(\n%s\n).dimensions) =%v \n want %v",
						tst.ctrCnfg, len(cv.(*sample_report.Type).Dimensions), len(tst.wantDimensionsType))
				}
				for a, b := range tst.wantDimensionsType {
					if cv.(*sample_report.Type).Dimensions[a] != b {
						t.Errorf("got inferTypeForSampleReport(\n%s\n).dimensions[%s] =%v \n want %v",
							tst.ctrCnfg, a, cv.(*sample_report.Type).Dimensions[a], b)
					}
				}
			} else {
				if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
					t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
				}
			}
		})
	}
}

func TestInferTypeForSampleCheck(t *testing.T) {
	for _, tst := range []inferTypeTest{
		{
			name: "SimpleValid",
			ctrCnfg: `
check_expression: response.size
`,
			cstrParam:     &sample_check.InstanceParam{},
			typeEvalRet:   pb.STRING,
			typeEvalError: nil,
			wantValueType: pb.STRING,
			wantErr:       "",
			willPanic:     false,
		},
		{
			name:      "NotValidInstanceParam",
			ctrCnfg:   ``,
			cstrParam: &empty.Empty{}, // cnstr type mismatch
			willPanic: true,
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			_ = fillProto(tst.ctrCnfg, cp)
			typeEvalFn := func(expr string) (pb.ValueType, error) { return tst.typeEvalRet, tst.typeEvalError }
			defer func() {
				r := recover()
				if tst.willPanic && r == nil {
					t.Errorf("Expected to recover from panic for %s, but recover was nil.", tst.name)
				} else if !tst.willPanic && r != nil {
					t.Errorf("got panic %v, expected success.", r)
				}
			}()
			cv, cerr := SupportedTmplInfo[sample_check.TemplateName].InferType(cp.(proto.Message), typeEvalFn)
			if tst.willPanic {
				t.Error("Should not reach this statement due to panic.")
			}
			if tst.wantErr == "" {
				if cerr != nil {
					t.Errorf("got err %v\nwant <nil>", cerr)
				}
				if tst.wantValueType != cv.(*sample_check.Type).CheckExpression {
					t.Errorf("got inferTypeForSampleCheck(\n%s\n).value=%v\nwant %v", tst.ctrCnfg, cv.(*sample_check.Type).CheckExpression, tst.wantValueType)
				}
			} else {
				if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
					t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
				}
			}
		})
	}
}

func TestInferTypeForSampleQuota(t *testing.T) {
	for _, tst := range []inferTypeTest{
		{
			name: "SimpleValid",
			ctrCnfg: `
dimensions:
  source: source.ip
  target: source.ip
  env: target.ip
`,
			cstrParam:          &sample_quota.InstanceParam{},
			typeEvalRet:        pb.STRING,
			typeEvalError:      nil,
			wantValueType:      pb.STRING,
			wantDimensionsType: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING, "env": pb.STRING},
			wantErr:            "",
			willPanic:          false,
		},
		{
			name:      "NotValidInstanceParam",
			ctrCnfg:   ``,
			cstrParam: &empty.Empty{}, // cnstr type mismatch
			wantErr:   "is not of type",
			willPanic: true,
		},
		{
			name: "ErrorFromTypeEvaluator",
			ctrCnfg: `
dimensions:
  source: source.ip
`,
			cstrParam:     &sample_quota.InstanceParam{},
			typeEvalError: fmt.Errorf("some expression x.y.z is invalid"),
			wantErr:       "some expression x.y.z is invalid",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			_ = fillProto(tst.ctrCnfg, cp)
			typeEvalFn := func(expr string) (pb.ValueType, error) { return tst.typeEvalRet, tst.typeEvalError }
			defer func() {
				r := recover()
				if tst.willPanic && r == nil {
					t.Errorf("Expected to recover from panic for %s, but recover was nil.", tst.name)
				} else if !tst.willPanic && r != nil {
					t.Errorf("got panic %v, expected success.", r)
				}
			}()
			cv, cerr := SupportedTmplInfo[sample_quota.TemplateName].InferType(cp.(proto.Message), typeEvalFn)
			if tst.wantErr == "" {
				if cerr != nil {
					t.Errorf("got err %v\nwant <nil>", cerr)
				}
				if len(tst.wantDimensionsType) != len(cv.(*sample_quota.Type).Dimensions) {
					t.Errorf("got len ( inferTypeForSampleReport(\n%s\n).dimensions) =%v \n want %v",
						tst.ctrCnfg, len(cv.(*sample_quota.Type).Dimensions), len(tst.wantDimensionsType))
				}
				for a, b := range tst.wantDimensionsType {
					if cv.(*sample_quota.Type).Dimensions[a] != b {
						t.Errorf("got inferTypeForSampleReport(\n%s\n).dimensions[%s] =%v \n want %v",
							tst.ctrCnfg, a, cv.(*sample_quota.Type).Dimensions[a], b)
					}
				}

			} else {
				if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
					t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
				}
			}
		})
	}
}

type ConfigureTypeTest struct {
	name     string
	tmpl     string
	types    map[string]proto.Message
	hdlrBldr adapter.HandlerBuilder
	want     interface{}
}

func TestConfigureType(t *testing.T) {
	for _, tst := range []ConfigureTypeTest{
		{
			name:     "SimpleReport",
			tmpl:     sample_report.TemplateName,
			types:    map[string]proto.Message{"foo": &sample_report.Type{}},
			hdlrBldr: &fakeReportHandler{},
			want:     map[string]*sample_report.Type{"foo": {}},
		},
		{
			name:     "SimpleCheck",
			tmpl:     sample_check.TemplateName,
			types:    map[string]proto.Message{"foo": &sample_check.Type{}},
			hdlrBldr: &fakeCheckHandler{},
			want:     map[string]*sample_check.Type{"foo": {}},
		},
		{
			name:     "SimpleQuota",
			tmpl:     sample_quota.TemplateName,
			types:    map[string]proto.Message{"foo": &sample_quota.Type{}},
			hdlrBldr: &fakeQuotaHandler{},
			want:     map[string]*sample_quota.Type{"foo": {}},
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			hb := &tst.hdlrBldr
			_ = SupportedTmplInfo[tst.tmpl].ConfigureType(tst.types, hb)

			var c interface{}
			if tst.tmpl == sample_report.TemplateName {
				c = tst.hdlrBldr.(*fakeReportHandler).cnfgCallInput
			} else if tst.tmpl == sample_check.TemplateName {
				c = tst.hdlrBldr.(*fakeCheckHandler).cnfgCallInput
			} else if tst.tmpl == sample_quota.TemplateName {
				c = tst.hdlrBldr.(*fakeQuotaHandler).cnfgCallInput
			}
			if !reflect.DeepEqual(c, tst.want) {
				t.Errorf("SupportedTmplInfo[%s].ConfigureType(%v) handler invoked value = %v, want %v", tst.tmpl, tst.types, c, tst.want)
			}
		})
	}
}

type ProcessTest struct {
	name          string
	insts         map[string]proto.Message
	hdlr          adapter.Handler
	wantInstance  interface{}
	wantCache     adapter.CacheabilityInfo // not for report calls
	wantQuotaResp adapter.QuotaResult      // only for quota calls
	wantError     string
}

func TestProcessReport(t *testing.T) {
	for _, tst := range []ProcessTest{
		{
			name: "Simple",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{Value: "1", Dimensions: map[string]string{"s": "2"}},
				"bar": &sample_report.InstanceParam{Value: "2", Dimensions: map[string]string{"k": "3"}},
			},
			hdlr: &fakeReportHandler{},
			wantInstance: []*sample_report.Instance{
				{Name: "foo", Value: int64(1), Dimensions: map[string]interface{}{"s": int64(2)}},
				{Name: "bar", Value: int64(2), Dimensions: map[string]interface{}{"k": int64(3)}},
			},
		},
		{
			name: "EvalAllError",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{Value: "1", Dimensions: map[string]string{"s": "bad.attributeName"}},
			},
			hdlr:      &fakeReportHandler{},
			wantError: "unresolved attribute bad.attributeName",
		},
		{
			name: "EvalError",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{Value: "bad.attributeName", Dimensions: map[string]string{"s": "2"}},
			},
			hdlr:      &fakeReportHandler{},
			wantError: "unresolved attribute bad.attributeName",
		},
		{
			name: "ProcessError",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{Value: "1", Dimensions: map[string]string{"s": "2"}},
			},
			hdlr:      &fakeReportHandler{retProcError: fmt.Errorf("error from process method")},
			wantError: "error from process method",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			ev, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			s := SupportedTmplInfo[sample_report.TemplateName].ProcessReport(tst.insts, fakeBag{}, ev, *h)
			v := (*h).(*fakeReportHandler).procCallInput.([]*sample_report.Instance)
			if tst.wantError != "" {
				if !strings.Contains(s.Message, tst.wantError) {
					t.Errorf("SupportedTmplInfo[sample_report.TemplateName].ProcessReport(%v) got error = %s, want %s", tst.insts, s.Message, tst.wantError)
				}
			} else if !cmp(v, tst.wantInstance) {
				t.Errorf("SupportedTmplInfo[sample_report.TemplateName].ProcessReport(%v) handler invoked value = %v, want %v", tst.insts, v, tst.wantInstance)
			}
		})
	}
}

func TestProcessCheck(t *testing.T) {
	for _, tst := range []ProcessTest{
		{
			name: "Simple",
			insts: map[string]proto.Message{
				"foo": &sample_check.InstanceParam{CheckExpression: `"abcd asd"`},
				"bar": &sample_check.InstanceParam{CheckExpression: `"pqrs asd"`},
			},
			hdlr: &fakeCheckHandler{ret: true, retCache: adapter.CacheabilityInfo{ValidUseCount: 111}},
			wantInstance: []*sample_check.Instance{
				{Name: "foo", CheckExpression: "abcd asd"},
				{Name: "bar", CheckExpression: "pqrs asd"},
			},
			wantCache: adapter.CacheabilityInfo{ValidUseCount: 111},
		},
		{
			name: "EvalError",
			insts: map[string]proto.Message{
				"foo": &sample_check.InstanceParam{CheckExpression: `bad.attributeName`},
			},
			hdlr:      &fakeCheckHandler{ret: true},
			wantError: "unresolved attribute bad.attributeName",
		},
		{
			name: "ProcessError",
			insts: map[string]proto.Message{
				"foo": &sample_check.InstanceParam{CheckExpression: `"abcd asd"`},
			},
			hdlr:      &fakeCheckHandler{retProcError: fmt.Errorf("error from process method")},
			wantError: "error from process method",
		},
		{
			name: "ProcRetFalse",
			insts: map[string]proto.Message{
				"foo": &sample_check.InstanceParam{CheckExpression: `"abcd asd"`},
			},
			hdlr:      &fakeCheckHandler{ret: false},
			wantError: " rejected",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			ev, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			s, cInfo := SupportedTmplInfo[sample_check.TemplateName].ProcessCheck(tst.insts, fakeBag{}, ev, *h)

			if tst.wantError != "" {
				if !strings.Contains(s.Message, tst.wantError) {
					t.Errorf("SupportedTmplInfo[sample_check.TemplateName].CheckSample(%v) got error = %s, want %s", tst.insts, s.Message, tst.wantError)
				}
			} else {
				v := (*h).(*fakeCheckHandler).procCallInput
				if !cmp(v, tst.wantInstance) || !reflect.DeepEqual(tst.wantCache, cInfo) {
					t.Errorf("SupportedTmplInfo[sample_check.TemplateName].CheckSample(%v) handler "+
						"invoked value = %v,%v want %v,%v", tst.insts, v, cInfo, tst.wantInstance, tst.wantCache)
				}
			}
		})
	}
}

func TestProcessQuota(t *testing.T) {
	for _, tst := range []ProcessTest{
		{
			name: "Simple",
			insts: map[string]proto.Message{
				"foo": &sample_quota.InstanceParam{Dimensions: map[string]string{"s": "2"}},
			},
			hdlr: &fakeQuotaHandler{retQuotaRes: adapter.QuotaResult{Amount: 100}, retCache: adapter.CacheabilityInfo{ValidUseCount: 111}},

			wantInstance:  &sample_quota.Instance{Name: "foo", Dimensions: map[string]interface{}{"s": int64(2)}},
			wantCache:     adapter.CacheabilityInfo{ValidUseCount: 111},
			wantQuotaResp: adapter.QuotaResult{Amount: 100},
		},
		{
			name: "EvalError",
			insts: map[string]proto.Message{
				"foo": &sample_quota.InstanceParam{Dimensions: map[string]string{"s": "bad.attributeName"}},
			},
			hdlr:      &fakeQuotaHandler{},
			wantError: "unresolved attribute bad.attributeName",
		},
		{
			name: "ProcessError",
			insts: map[string]proto.Message{
				"foo": &sample_quota.InstanceParam{Dimensions: map[string]string{"s": "2"}},
			},
			hdlr:      &fakeQuotaHandler{retProcError: fmt.Errorf("error from process method")},
			wantError: "error from process method",
		},
		{
			name: "AmtZero",
			insts: map[string]proto.Message{
				"foo": &sample_quota.InstanceParam{Dimensions: map[string]string{"s": "2"}},
			},
			hdlr:      &fakeQuotaHandler{retQuotaRes: adapter.QuotaResult{Amount: 0}, retCache: adapter.CacheabilityInfo{ValidUseCount: 111}},
			wantError: "Unable to allocate",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			ev, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			s, cInfo, qr := SupportedTmplInfo[sample_quota.TemplateName].ProcessQuota("foo", tst.insts["foo"], fakeBag{}, ev, *h, adapter.QuotaRequestArgs{})

			if tst.wantError != "" {
				if !strings.Contains(s.Message, tst.wantError) {
					t.Errorf("SupportedTmplInfo[sample_quota.TemplateName].AllocQuota(%v) got error = %s, want %s", tst.insts, s.Message, tst.wantError)
				}
			} else {
				v := (*h).(*fakeQuotaHandler).procCallInput
				if !reflect.DeepEqual(v, tst.wantInstance) || !reflect.DeepEqual(tst.wantCache, cInfo) || !reflect.DeepEqual(tst.wantQuotaResp, qr) {
					t.Errorf("SupportedTmplInfo[sample_quota.TemplateName].AllocQuota(%v) "+
						"handler invoked value = %v,%v,%v  want %v,%v,%v", tst.insts, v, cInfo, qr, tst.wantInstance, tst.wantCache, tst.wantQuotaResp)
				}
			}
		})
	}
}

func cmp(m interface{}, n interface{}) bool {
	a := InterfaceSlice(m)
	b := InterfaceSlice(n)
	if len(a) != len(b) {
		return false
	}

	for _, x1 := range a {
		f := false
		for _, x2 := range b {
			if reflect.DeepEqual(x1, x2) {
				f = true
			}
		}
		if !f {
			return false
		}
	}
	return true
}

func InterfaceSlice(slice interface{}) []interface{} {
	s := reflect.ValueOf(slice)

	ret := make([]interface{}, s.Len())
	for i := 0; i < s.Len(); i++ {
		ret[i] = s.Index(i).Interface()
	}

	return ret
}

func fillProto(cfg string, o interface{}) error {
	//data []byte, m map[string]interface{}, err error
	var m map[string]interface{}
	var data []byte
	var err error

	if err = yaml.Unmarshal([]byte(cfg), &m); err != nil {
		return err
	}

	if data, err = json.Marshal(m); err != nil {
		return err
	}

	err = yaml.Unmarshal(data, o)
	return err
}
