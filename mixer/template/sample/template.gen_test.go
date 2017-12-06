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

package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"net"

	pb "istio.io/api/mixer/v1/config/descriptor"
	adpTmpl "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/evaluator"
	istio_mixer_adapter_sample_myapa "istio.io/istio/mixer/template/sample/apa"
	sample_check "istio.io/istio/mixer/template/sample/check"
	sample_quota "istio.io/istio/mixer/template/sample/quota"
	sample_report "istio.io/istio/mixer/template/sample/report"
)

// Does not implement any template interfaces.
type fakeBadHandler struct{}

func (h fakeBadHandler) Close() error { return nil }
func (h fakeBadHandler) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h fakeBadHandler) Validate() *adapter.ConfigErrors     { return nil }
func (h fakeBadHandler) SetAdapterConfig(cfg adapter.Config) {}

type fakeReportHandler struct {
	adapter.Handler
	retError      error
	cnfgCallInput interface{}
	procCallInput interface{}
}

func (h *fakeReportHandler) Close() error { return nil }
func (h *fakeReportHandler) HandleReport(ctx context.Context, instances []*sample_report.Instance) error {
	h.procCallInput = instances
	return h.retError
}
func (h *fakeReportHandler) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeReportHandler) SetReportTypes(t map[string]*sample_report.Type) {
	h.cnfgCallInput = t
}
func (h *fakeReportHandler) Validate() *adapter.ConfigErrors     { return nil }
func (h *fakeReportHandler) SetAdapterConfig(cfg adapter.Config) {}

type fakeMyApaHandler struct {
	adapter.Handler
	retOutput     *istio_mixer_adapter_sample_myapa.Output
	retError      error
	cnfgCallInput interface{}
	procCallInput interface{}
}

var _ istio_mixer_adapter_sample_myapa.Handler = &fakeMyApaHandler{}

func (h *fakeMyApaHandler) Close() error { return nil }
func (h *fakeMyApaHandler) GenerateMyApaAttributes(ctx context.Context, instance *istio_mixer_adapter_sample_myapa.Instance) (
	*istio_mixer_adapter_sample_myapa.Output, error) {
	h.procCallInput = instance
	return h.retOutput, h.retError
}

func (h *fakeMyApaHandler) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeMyApaHandler) Validate() *adapter.ConfigErrors     { return nil }
func (h *fakeMyApaHandler) SetAdapterConfig(cfg adapter.Config) {}

type fakeCheckHandler struct {
	adapter.Handler
	retError      error
	cnfgCallInput interface{}
	procCallInput interface{}
	retResult     adapter.CheckResult
}

func (h *fakeCheckHandler) Close() error { return nil }
func (h *fakeCheckHandler) HandleCheck(ctx context.Context, instance *sample_check.Instance) (adapter.CheckResult, error) {
	h.procCallInput = instance
	return h.retResult, h.retError
}
func (h *fakeCheckHandler) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeCheckHandler) SetCheckTypes(t map[string]*sample_check.Type) { h.cnfgCallInput = t }
func (h *fakeCheckHandler) Validate() *adapter.ConfigErrors               { return nil }
func (h *fakeCheckHandler) SetAdapterConfig(cfg adapter.Config)           {}

type fakeQuotaHandler struct {
	adapter.Handler
	retError      error
	retResult     adapter.QuotaResult
	cnfgCallInput interface{}
	procCallInput interface{}
}

func (h *fakeQuotaHandler) Close() error { return nil }
func (h *fakeQuotaHandler) HandleQuota(ctx context.Context, instance *sample_quota.Instance, qra adapter.QuotaArgs) (adapter.QuotaResult, error) {
	h.procCallInput = instance
	return h.retResult, h.retError
}
func (h *fakeQuotaHandler) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	return nil, nil
}
func (h *fakeQuotaHandler) SetQuotaTypes(t map[string]*sample_quota.Type) {
	h.cnfgCallInput = t
}
func (h *fakeQuotaHandler) Validate() *adapter.ConfigErrors     { return nil }
func (h *fakeQuotaHandler) SetAdapterConfig(cfg adapter.Config) {}

type fakeBag struct{}

func (f fakeBag) Get(name string) (value interface{}, found bool) { return nil, false }
func (f fakeBag) Names() []string                                 { return []string{} }
func (f fakeBag) Done()                                           {}
func (f fakeBag) DebugString() string                             { return "" }

func TestGeneratedFields(t *testing.T) {
	for _, tst := range []struct {
		tmpl      string
		ctrCfg    proto.Message
		variety   adpTmpl.TemplateVariety
		name      string
		bldrName  string
		hndlrName string
	}{
		{
			tmpl:      sample_report.TemplateName,
			ctrCfg:    &sample_report.InstanceParam{},
			variety:   adpTmpl.TEMPLATE_VARIETY_REPORT,
			bldrName:  sample_report.TemplateName + "." + "HandlerBuilder",
			hndlrName: sample_report.TemplateName + "." + "Handler",
			name:      sample_report.TemplateName,
		},
		{
			tmpl:      sample_check.TemplateName,
			ctrCfg:    &sample_check.InstanceParam{},
			variety:   adpTmpl.TEMPLATE_VARIETY_CHECK,
			bldrName:  sample_check.TemplateName + "." + "HandlerBuilder",
			hndlrName: sample_check.TemplateName + "." + "Handler",
			name:      sample_check.TemplateName,
		},
		{
			tmpl:      sample_quota.TemplateName,
			ctrCfg:    &sample_quota.InstanceParam{},
			variety:   adpTmpl.TEMPLATE_VARIETY_QUOTA,
			bldrName:  sample_quota.TemplateName + "." + "HandlerBuilder",
			hndlrName: sample_quota.TemplateName + "." + "Handler",
			name:      sample_quota.TemplateName,
		},
	} {
		t.Run(tst.tmpl, func(t *testing.T) {
			if !reflect.DeepEqual(SupportedTmplInfo[tst.tmpl].CtrCfg, tst.ctrCfg) {
				t.Errorf("SupportedTmplInfo[%s].CtrCfg = %T, want %T", tst.tmpl, SupportedTmplInfo[tst.tmpl].CtrCfg, tst.ctrCfg)
			}
			if SupportedTmplInfo[tst.tmpl].Variety != tst.variety {
				t.Errorf("SupportedTmplInfo[%s].Variety = %v, want %v", tst.tmpl, SupportedTmplInfo[tst.tmpl].Variety, tst.variety)
			}
			if SupportedTmplInfo[tst.tmpl].BldrInterfaceName != tst.bldrName {
				t.Errorf("SupportedTmplInfo[%s].BldrName = %v, want %v", tst.tmpl, SupportedTmplInfo[tst.tmpl].BldrInterfaceName, tst.bldrName)
			}
			if SupportedTmplInfo[tst.tmpl].HndlrInterfaceName != tst.hndlrName {
				t.Errorf("SupportedTmplInfo[%s].HndlrName = %v, want %v", tst.tmpl, SupportedTmplInfo[tst.tmpl].HndlrInterfaceName, tst.hndlrName)
			}
			if SupportedTmplInfo[tst.tmpl].Name != tst.name {
				t.Errorf("SupportedTmplInfo[%s].Name = %s, want %s", tst.tmpl, SupportedTmplInfo[tst.tmpl].Name, tst.name)
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
			c := SupportedTmplInfo[tst.tmpl].BuilderSupportsTemplate(tst.hndlrBldr)
			if c != tst.result {
				t.Errorf("SupportedTmplInfo[%s].SupportsTemplate(%T) = %t, want %t", tst.tmpl, tst.hndlrBldr, c, tst.result)
			}
		})
	}
}

type inferTypeTest struct {
	name          string
	instYamlCfg   string
	cstrParam     interface{}
	typeEvalError error
	wantErr       string
	willPanic     bool
	wantType      interface{}
}

func getExprEvalFunc(err error) func(string) (pb.ValueType, error) {
	return func(expr string) (pb.ValueType, error) {
		expr = strings.ToLower(expr)
		retType := pb.VALUE_TYPE_UNSPECIFIED
		if strings.HasSuffix(expr, "string") {
			retType = pb.STRING
		}
		if strings.HasSuffix(expr, "double") {
			retType = pb.DOUBLE
		}
		if strings.HasSuffix(expr, "bool") {
			retType = pb.BOOL
		}
		if strings.HasSuffix(expr, "int64") {
			retType = pb.INT64
		}
		if strings.HasSuffix(expr, "duration") {
			retType = pb.DURATION
		}
		if strings.HasSuffix(expr, "timestamp") {
			retType = pb.TIMESTAMP
		}
		return retType, err
	}
}

func TestInferTypeForSampleReport(t *testing.T) {
	for _, tst := range []inferTypeTest{
		{
			name: "Valid",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "",
			willPanic:     false,
			wantType: &sample_report.Type{
				Value:      pb.INT64,
				Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING},
			},
		},
		{
			name: "ValidWithSubmsg",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
res1:
  value: source.int64
  int64Primitive: source.int64
  boolPrimitive: source.bool
  doublePrimitive: source.double
  stringPrimitive: source.string
  timeStamp: source.timestamp
  duration: source.duration
  dimensions:
    source: source.string
    target: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "",
			willPanic:     false,
			wantType: &sample_report.Type{
				Value:      pb.INT64,
				Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING},
				Res1: &sample_report.Res1Type{
					Value:      pb.INT64,
					Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING},
					Res2Map:    map[string]*sample_report.Res2Type{},
				},
			},
		},
		{
			name: "MissingAField",
			instYamlCfg: `
value: source.int64
# int64Primitive: source.int64 # missing int64Primitive
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "expression for field 'Int64Primitive' cannot be empty",
			willPanic:     false,
		},
		{
			name: "MissingAFieldSubMsg",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
res1:
  value: source.int64
  # int64Primitive: source.int64 # missing int64Primitive
  boolPrimitive: source.bool
  doublePrimitive: source.double
  stringPrimitive: source.string
  timeStamp: source.timestamp
  duration: source.duration
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "expression for field 'Res1.Int64Primitive' cannot be empty",
			willPanic:     false,
		},
		{
			name: "InferredTypeNotMatchStaticType",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.double # Double does not match string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "error type checking for field 'StringPrimitive': Evaluated expression type DOUBLE want STRING",
			willPanic:     false,
		},
		{
			name: "InferredTypeNotMatchStaticTypeSubMsg",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
res1:
  value: source.int64
  int64Primitive: source.int64
  boolPrimitive: source.bool
  doublePrimitive: source.double
  stringPrimitive: source.double # Double does not match string
  timeStamp: source.timestamp
  duration: source.duration
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "error type checking for field 'Res1.StringPrimitive': Evaluated expression type DOUBLE want STRING",
			willPanic:     false,
		},
		{
			name: "EmptyString",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: '""'
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "expression for field 'StringPrimitive' cannot be empty",
			willPanic:     false,
		},

		{
			name: "EmptyStringSubMsg",
			instYamlCfg: `
value: source.int64
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
res1:
  value: source.int64
  int64Primitive: source.int64
  boolPrimitive: source.bool
  doublePrimitive: source.double
  stringPrimitive: '""'
  timeStamp: source.timestamp
  duration: source.duration
  dimensions:
    source: source.string
    target: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "expression for field 'Res1.StringPrimitive' cannot be empty",
			willPanic:     false,
		},
		{
			name:        "NotValidInstanceParam",
			instYamlCfg: ``,
			cstrParam:   &empty.Empty{}, // cnstr type mismatch
			wantErr:     "is not of type",
			willPanic:   true,
		},
		{
			name: "ErrorFromTypeEvaluator",
			instYamlCfg: `
value: response.int64
dimensions:
  source: source.string
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: fmt.Errorf("some expression x.y.z is invalid"),
			wantErr:       "some expression x.y.z is invalid",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			err := fillProto(tst.instYamlCfg, cp)
			if err != nil {
				t.Fatalf("cannot load yaml %v", err)
			}
			typeEvalFn := getExprEvalFunc(tst.typeEvalError)
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
				v := cv.(*sample_report.Type)
				if !reflect.DeepEqual(v, tst.wantType) {
					t.Errorf("InferType (%s) = \n%v, want \n%v", tst.name, spew.Sdump(v), spew.Sdump(tst.wantType))
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
			instYamlCfg: `
check_expression: source.string
timeStamp: source.timestamp
duration: source.duration
res1:
  value: source.int64
  int64Primitive: source.int64
  boolPrimitive: source.bool
  doublePrimitive: source.double
  stringPrimitive: source.string
  timeStamp: source.timestamp
  duration: source.duration
  dimensions:
    source: source.string
    target: source.string
  res2_map:
    source2:
      value: source.int64
      dimensions:
        source: source.string
        target: source.string
      int64Primitive: source.int64
`,
			cstrParam:     &sample_check.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "",
			willPanic:     false,
			wantType: &sample_check.Type{
				Res1: &sample_check.Res1Type{
					Value:      pb.INT64,
					Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING},
					Res2Map: map[string]*sample_check.Res2Type{
						"source2": {
							Value:      pb.INT64,
							Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING},
						},
					},
				},
			},
		},
		{
			name:        "NotValidInstanceParam",
			instYamlCfg: ``,
			cstrParam:   &empty.Empty{}, // cnstr type mismatch
			willPanic:   true,
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			err := fillProto(tst.instYamlCfg, cp)
			if err != nil {
				t.Fatalf("cannot load yaml %v", err)
			}
			typeEvalFn := getExprEvalFunc(tst.typeEvalError)
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
				v := cv.(*sample_check.Type)
				if !reflect.DeepEqual(v, tst.wantType) {
					t.Errorf("InferType (%s) = \n%v, want \n%v", tst.name, spew.Sdump(v), spew.Sdump(tst.wantType))
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
			instYamlCfg: `
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.string
  target: source.string
  env: target.string
res1:
  value: source.int64
  int64Primitive: source.int64
  boolPrimitive: source.bool
  doublePrimitive: source.double
  stringPrimitive: source.string
  timeStamp: source.timestamp
  duration: source.duration
  dimensions:
    source: source.string
    target: source.string
    env: target.string
`,
			cstrParam: &sample_quota.InstanceParam{},
			wantType: &sample_quota.Type{
				Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING, "env": pb.STRING},
				Res1: &sample_quota.Res1Type{
					Value:      pb.INT64,
					Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING, "env": pb.STRING},
					Res2Map:    map[string]*sample_quota.Res2Type{},
				},
			},
		},
		{
			name:        "NotValidInstanceParam",
			instYamlCfg: ``,
			cstrParam:   &empty.Empty{}, // cnstr type mismatch
			wantErr:     "is not of type",
			willPanic:   true,
		},
		{
			name: "ErrorFromTypeEvaluator",
			instYamlCfg: `
timeStamp: source.timestamp
duration: source.duration
dimensions:
  source: source.badAttr
`,
			cstrParam:     &sample_quota.InstanceParam{},
			typeEvalError: fmt.Errorf("some expression x.y.z is invalid"),
			wantErr:       "some expression x.y.z is invalid",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			err := fillProto(tst.instYamlCfg, cp)
			if err != nil {
				t.Fatalf("cannot load yaml %v", err)
			}
			typeEvalFn := getExprEvalFunc(tst.typeEvalError)
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
				v := cv.(*sample_quota.Type)
				if !reflect.DeepEqual(v, tst.wantType) {
					t.Errorf("InferType (%s) = \n%v, want \n%v", tst.name, spew.Sdump(v), spew.Sdump(tst.wantType))
				}
			} else {
				if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
					t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
				}
			}
		})
	}
}

type SetTypeTest struct {
	name     string
	tmpl     string
	types    map[string]proto.Message
	hdlrBldr adapter.HandlerBuilder
	want     interface{}
}

func TestSetType(t *testing.T) {
	for _, tst := range []SetTypeTest{
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
			hb := tst.hdlrBldr
			SupportedTmplInfo[tst.tmpl].SetType(tst.types, hb)

			var c interface{}
			if tst.tmpl == sample_report.TemplateName {
				c = tst.hdlrBldr.(*fakeReportHandler).cnfgCallInput
			} else if tst.tmpl == sample_check.TemplateName {
				c = tst.hdlrBldr.(*fakeCheckHandler).cnfgCallInput
			} else if tst.tmpl == sample_quota.TemplateName {
				c = tst.hdlrBldr.(*fakeQuotaHandler).cnfgCallInput
			}
			if !reflect.DeepEqual(c, tst.want) {
				t.Errorf("SupportedTmplInfo[%s].SetType(%v) handler invoked value = %v, want %v", tst.tmpl, tst.types, c, tst.want)
			}
		})
	}
}

type fakeExpr struct {
	extraAttrManifest []*istio_mixer_v1_config.AttributeManifest
}

// newFakeExpr returns the basic
func newFakeExpr(extraAttrManifest []*istio_mixer_v1_config.AttributeManifest) *fakeExpr {
	return &fakeExpr{extraAttrManifest: extraAttrManifest}
}

// Eval evaluates given expression using the attribute bag
func (e *fakeExpr) Eval(mapExpression string, attrs attribute.Bag) (interface{}, error) {
	expr2 := mapExpression

	if strings.HasSuffix(expr2, "string") {
		return "", nil
	}
	if strings.HasSuffix(expr2, "double") {
		return 1.1, nil
	}
	if strings.HasSuffix(expr2, "bool") {
		return true, nil
	}
	if strings.HasSuffix(expr2, "int64") {
		return int64(1234), nil
	}
	if strings.HasSuffix(expr2, "duration") {
		return 10 * time.Second, nil
	}
	if strings.HasSuffix(expr2, "timestamp") {
		return time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC), nil
	}
	ev, _ := evaluator.NewILEvaluator(1024)
	ev.ChangeVocabulary(createAttributeDescriptorFinder(e.extraAttrManifest))
	return ev.Eval(expr2, attrs)
}

var baseConfig = istio_mixer_v1_config.GlobalConfig{
	Manifests: []*istio_mixer_v1_config.AttributeManifest{
		{
			Attributes: map[string]*istio_mixer_v1_config.AttributeManifest_AttributeInfo{
				"str.absent": {
					ValueType: pb.STRING,
				},
				"bool.absent": {
					ValueType: pb.BOOL,
				},
				"double.absent": {
					ValueType: pb.DOUBLE,
				},
				"int64.absent": {
					ValueType: pb.INT64,
				},
				"source.int64": {
					ValueType: pb.INT64,
				},
				"source.bool": {
					ValueType: pb.BOOL,
				},
				"source.double": {
					ValueType: pb.DOUBLE,
				},
				"source.string": {
					ValueType: pb.STRING,
				},
				"source.timestamp": {
					ValueType: pb.TIMESTAMP,
				},
				"source.duration": {
					ValueType: pb.DURATION,
				},
				"source.ip": {
					ValueType: pb.IP_ADDRESS,
				},
			},
		},
	},
}

// attributeFinder exposes expr.AttributeDescriptorFinder
type attributeFinder struct {
	attrs map[string]*istio_mixer_v1_config.AttributeManifest_AttributeInfo
}

// GetAttribute finds an attribute by name.
// This function is only called when a new handler is instantiated.
func (a attributeFinder) GetAttribute(name string) *istio_mixer_v1_config.AttributeManifest_AttributeInfo {
	return a.attrs[name]
}

func createAttributeDescriptorFinder(extraAttrManifest []*istio_mixer_v1_config.AttributeManifest) expr.AttributeDescriptorFinder {
	attrs := make(map[string]*istio_mixer_v1_config.AttributeManifest_AttributeInfo)
	for _, m := range baseConfig.Manifests {
		for an, at := range m.Attributes {
			attrs[an] = at
		}
	}
	for _, m := range extraAttrManifest {
		for an, at := range m.Attributes {
			attrs[an] = at
		}
	}
	return &attributeFinder{attrs: attrs}
}

// EvalPredicate evaluates given predicate using the attribute bag
func (e *fakeExpr) EvalPredicate(mapExpression string, attrs attribute.Bag) (bool, error) {
	return true, nil
}

func (e *fakeExpr) EvalType(s string, af expr.AttributeDescriptorFinder) (pb.ValueType, error) {
	//return pb.VALUE_TYPE_UNSPECIFIED, nil
	if i := af.GetAttribute(s); i != nil {
		return i.ValueType, nil
	}
	tc := evaluator.NewTypeChecker()
	return tc.EvalType(s, af)
}

func (e *fakeExpr) AssertType(string, expr.AttributeDescriptorFinder, pb.ValueType) error {
	return nil
}

func TestProcessReport(t *testing.T) {
	for _, tst := range []struct {
		name         string
		insts        map[string]proto.Message
		hdlr         adapter.Handler
		wantInstance interface{}
		wantError    string
	}{
		{
			name: " Valid",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "2"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
					Res1: &sample_report.Res1InstanceParam{
						Value:           "1",
						Dimensions:      map[string]string{"s": "2"},
						BoolPrimitive:   "true",
						DoublePrimitive: "1.2",
						Int64Primitive:  "54362",
						StringPrimitive: `"mystring"`,
						Int64Map:        map[string]string{"a": "1"},
						TimeStamp:       "request.timestamp",
						Duration:        "request.duration",
						Res2: &sample_report.Res2InstanceParam{
							Value:          "1",
							Dimensions:     map[string]string{"s": "2"},
							Int64Primitive: "54362",
						},
						Res2Map: map[string]*sample_report.Res2InstanceParam{
							"foo": {
								Value:          "1",
								Dimensions:     map[string]string{"s": "2"},
								Int64Primitive: "54362",
							},
						},
					},
				},
				"bar": &sample_report.InstanceParam{
					Value:           "2",
					Dimensions:      map[string]string{"k": "3"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"b": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
				},
			},
			hdlr: &fakeReportHandler{},
			wantInstance: []*sample_report.Instance{
				{
					Name:            "foo",
					Value:           int64(1),
					Dimensions:      map[string]interface{}{"s": int64(2)},
					BoolPrimitive:   true,
					DoublePrimitive: 1.2,
					Int64Primitive:  54362,
					StringPrimitive: "mystring",
					Int64Map:        map[string]int64{"a": int64(1)},
					TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
					Duration:        10 * time.Second,
					Res1: &sample_report.Res1{
						Value:           int64(1),
						Dimensions:      map[string]interface{}{"s": int64(2)},
						BoolPrimitive:   true,
						DoublePrimitive: 1.2,
						Int64Primitive:  54362,
						StringPrimitive: "mystring",
						Int64Map:        map[string]int64{"a": int64(1)},
						TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
						Duration:        10 * time.Second,
						Res2: &sample_report.Res2{
							Value:          int64(1),
							Dimensions:     map[string]interface{}{"s": int64(2)},
							Int64Primitive: 54362,
						},
						Res2Map: map[string]*sample_report.Res2{
							"foo": {
								Value:          int64(1),
								Dimensions:     map[string]interface{}{"s": int64(2)},
								Int64Primitive: 54362,
							},
						},
					},
				},
				{
					Name:            "bar",
					Value:           int64(2),
					Dimensions:      map[string]interface{}{"k": int64(3)},
					BoolPrimitive:   true,
					DoublePrimitive: 1.2,
					Int64Primitive:  54362,
					StringPrimitive: "mystring",
					Int64Map:        map[string]int64{"b": int64(1)},
					TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
					Duration:        10 * time.Second,
				},
			},
		},
		{
			name: "EvalAllError",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "bad.attribute"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
				},
			},
			hdlr:      &fakeReportHandler{},
			wantError: "unknown attribute bad.attribute",
		},
		{
			name: "EvalAllErrorSubMessage",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "1"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
					Res1: &sample_report.Res1InstanceParam{
						Value:           "1",
						Dimensions:      map[string]string{"s": "bad.attribute"},
						BoolPrimitive:   "true",
						DoublePrimitive: "1.2",
						Int64Primitive:  "54362",
						StringPrimitive: `"mystring"`,
						Int64Map:        map[string]string{"a": "1"},
						TimeStamp:       "request.timestamp",
						Duration:        "request.duration",
					},
				},
			},
			hdlr:      &fakeReportHandler{},
			wantError: "failed to evaluate field 'Res1.Dimensions' for instance 'foo'",
		},
		{
			name: "EvalError",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "bad.attribute",
					Dimensions:      map[string]string{"s": "2"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
				},
			},
			hdlr:      &fakeReportHandler{},
			wantError: "unknown attribute bad.attribute",
		},
		{
			name: "EvalErrorSubMsg",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "2"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
					Res1: &sample_report.Res1InstanceParam{
						Value:           "bad.attribute",
						Dimensions:      map[string]string{"s": "2"},
						BoolPrimitive:   "true",
						DoublePrimitive: "1.2",
						Int64Primitive:  "54362",
						StringPrimitive: `"mystring"`,
						Int64Map:        map[string]string{"a": "1"},
						TimeStamp:       "request.timestamp",
						Duration:        "request.duration",
					},
				},
			},
			hdlr:      &fakeReportHandler{},
			wantError: "failed to evaluate field 'Res1.Value' for instance 'foo'",
		},
		{
			name: "AttributeAbsentAtRuntime",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "int64.absent | 2",
					Dimensions:      map[string]string{"s": "str.absent | \"\""},
					BoolPrimitive:   "bool.absent | true",
					DoublePrimitive: "double.absent | 1.2",
					Int64Primitive:  "int64.absent | 123",
					StringPrimitive: "str.absent | \"\"",
					Int64Map:        map[string]string{"a": "int64.absent | 123"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
				},
			},
			hdlr: &fakeReportHandler{},
			wantInstance: []*sample_report.Instance{
				{
					Name:            "foo",
					Value:           int64(2),
					Dimensions:      map[string]interface{}{"s": ""},
					BoolPrimitive:   true,
					DoublePrimitive: 1.2,
					Int64Primitive:  123,
					StringPrimitive: "",
					Int64Map:        map[string]int64{"a": int64(123)},
					TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
					Duration:        10 * time.Second,
				},
			},
		},
		{
			name: "ProcessError",
			insts: map[string]proto.Message{
				"foo": &sample_report.InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "2"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
				},
			},
			hdlr:      &fakeReportHandler{retError: fmt.Errorf("error from process method")},
			wantError: "error from process method",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			err := SupportedTmplInfo[sample_report.TemplateName].ProcessReport(context.TODO(), tst.insts, fakeBag{}, newFakeExpr(nil), *h)

			if tst.wantError != "" {
				if !strings.Contains(err.Error(), tst.wantError) {
					t.Errorf("ProcessReport got error = %s, want %s", err.Error(), tst.wantError)
				}
			} else {
				if err != nil {
					t.Fatalf("ProcessReport got error %v , want success", err)
				}
				v := (*h).(*fakeReportHandler).procCallInput.([]*sample_report.Instance)
				if !cmp(v, tst.wantInstance) {
					t.Errorf("ProcessReport handler invoked value = %v, want %v", spew.Sdump(v), spew.Sdump(tst.wantInstance))
				}
			}
		})
	}
}

func TestProcessCheck(t *testing.T) {
	for _, tst := range []struct {
		name            string
		instName        string
		inst            proto.Message
		hdlr            adapter.Handler
		wantInstance    interface{}
		wantCheckResult adapter.CheckResult
		wantError       string
	}{
		{
			name:     "Valid",
			instName: "foo",
			inst: &sample_check.InstanceParam{
				CheckExpression: `"abcd asd"`,
				StringMap:       map[string]string{"a": `"aaa"`},
				Res1: &sample_check.Res1InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "2"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
					Res2: &sample_check.Res2InstanceParam{
						Value:          "1",
						Dimensions:     map[string]string{"s": "2"},
						Int64Primitive: "54362",
					},
					Res2Map: map[string]*sample_check.Res2InstanceParam{
						"foo": {
							Value:          "1",
							Dimensions:     map[string]string{"s": "2"},
							Int64Primitive: "54362",
						},
					},
				},
			},
			hdlr: &fakeCheckHandler{
				retResult: adapter.CheckResult{Status: rpc.Status{Message: "msg"}},
			},
			wantInstance: &sample_check.Instance{
				Name: "foo", CheckExpression: "abcd asd", StringMap: map[string]string{"a": "aaa"},
				Res1: &sample_check.Res1{
					Value:           int64(1),
					Dimensions:      map[string]interface{}{"s": int64(2)},
					BoolPrimitive:   true,
					DoublePrimitive: 1.2,
					Int64Primitive:  54362,
					StringPrimitive: "mystring",
					Int64Map:        map[string]int64{"a": int64(1)},
					TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
					Duration:        10 * time.Second,
					Res2: &sample_check.Res2{
						Value:          int64(1),
						Dimensions:     map[string]interface{}{"s": int64(2)},
						Int64Primitive: 54362,
					},
					Res2Map: map[string]*sample_check.Res2{
						"foo": {
							Value:          int64(1),
							Dimensions:     map[string]interface{}{"s": int64(2)},
							Int64Primitive: 54362,
						},
					},
				},
			},
			wantCheckResult: adapter.CheckResult{Status: rpc.Status{Message: "msg"}},
		},
		{
			name:     "EvalError",
			instName: "foo",
			inst: &sample_check.InstanceParam{
				CheckExpression: `"abcd asd"`,
				StringMap:       map[string]string{"a": "bad.attribute"},
			},
			wantError: "unknown attribute bad.attribute",
		},
		{
			name:     "ProcessError",
			instName: "foo",
			inst: &sample_check.InstanceParam{
				CheckExpression: `"abcd asd"`,
				StringMap:       map[string]string{"a": `"aaa"`},
			},
			hdlr: &fakeCheckHandler{
				retError: fmt.Errorf("some error"),
			},
			wantError: "some error",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			res, err := SupportedTmplInfo[sample_check.TemplateName].ProcessCheck(context.TODO(), tst.instName, tst.inst, fakeBag{}, newFakeExpr(nil), *h)
			if tst.wantError != "" {
				if !strings.Contains(err.Error(), tst.wantError) {
					t.Errorf("ProcessCheckSample got error = %s, want %s", err.Error(), tst.wantError)
				}
			} else {
				v := (*h).(*fakeCheckHandler).procCallInput
				if !reflect.DeepEqual(v, tst.wantInstance) {
					t.Errorf("CheckSample handler "+
						"invoked value = %v want %v", spew.Sdump(v), spew.Sdump(tst.wantInstance))
				}
				if !reflect.DeepEqual(tst.wantCheckResult, res) {
					t.Errorf("CheckSample result = %v want %v", res, spew.Sdump(tst.wantCheckResult))
				}
			}
		})
	}
}

func TestProcessQuota(t *testing.T) {
	for _, tst := range []struct {
		name            string
		instName        string
		inst            proto.Message
		hdlr            adapter.Handler
		wantInstance    interface{}
		wantQuotaResult adapter.QuotaResult
		wantError       string
	}{
		{
			name:     "Valid",
			instName: "foo",
			inst: &sample_quota.InstanceParam{
				Dimensions: map[string]string{"a": `"str"`},
				BoolMap:    map[string]string{"a": "true"},
				Res1: &sample_quota.Res1InstanceParam{
					Value:           "1",
					Dimensions:      map[string]string{"s": "2"},
					BoolPrimitive:   "true",
					DoublePrimitive: "1.2",
					Int64Primitive:  "54362",
					StringPrimitive: `"mystring"`,
					Int64Map:        map[string]string{"a": "1"},
					TimeStamp:       "request.timestamp",
					Duration:        "request.duration",
					Res2: &sample_quota.Res2InstanceParam{
						Value:          "1",
						Dimensions:     map[string]string{"s": "2"},
						Int64Primitive: "54362",
					},
					Res2Map: map[string]*sample_quota.Res2InstanceParam{
						"foo": {
							Value:          "1",
							Dimensions:     map[string]string{"s": "2"},
							Int64Primitive: "54362",
						},
					},
				},
			},
			hdlr: &fakeQuotaHandler{
				retResult: adapter.QuotaResult{Amount: 1},
			},
			wantInstance: &sample_quota.Instance{
				Name: "foo", Dimensions: map[string]interface{}{"a": "str"}, BoolMap: map[string]bool{"a": true},
				Res1: &sample_quota.Res1{
					Value:           int64(1),
					Dimensions:      map[string]interface{}{"s": int64(2)},
					BoolPrimitive:   true,
					DoublePrimitive: 1.2,
					Int64Primitive:  54362,
					StringPrimitive: "mystring",
					Int64Map:        map[string]int64{"a": int64(1)},
					TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
					Duration:        10 * time.Second,
					Res2: &sample_quota.Res2{
						Value:          int64(1),
						Dimensions:     map[string]interface{}{"s": int64(2)},
						Int64Primitive: 54362,
					},
					Res2Map: map[string]*sample_quota.Res2{
						"foo": {
							Value:          int64(1),
							Dimensions:     map[string]interface{}{"s": int64(2)},
							Int64Primitive: 54362,
						},
					},
				},
			},
			wantQuotaResult: adapter.QuotaResult{Amount: 1},
		},
		{
			name:     "EvalError",
			instName: "foo",
			inst: &sample_quota.InstanceParam{
				Dimensions: map[string]string{"a": "bad.attribute"},
				BoolMap:    map[string]string{"a": "true"},
			},
			wantError: "unknown attribute bad.attribute",
		},
		{
			name:     "EvalErrorSubMsg",
			instName: "foo",
			inst: &sample_quota.InstanceParam{
				Dimensions: map[string]string{"a": "source.string"},
				BoolMap:    map[string]string{"a": "true"},
				Res1: &sample_quota.Res1InstanceParam{
					Value: "bad.attribute",
				},
			},
			wantError: "unknown attribute bad.attribute",
		},
		{
			name:     "ProcessError",
			instName: "foo",
			inst: &sample_quota.InstanceParam{
				Dimensions: map[string]string{"a": `"str"`},
				BoolMap:    map[string]string{"a": "true"},
			},
			hdlr: &fakeQuotaHandler{
				retError: fmt.Errorf("some error"),
			},
			wantError: "some error",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			res, err := SupportedTmplInfo[sample_quota.TemplateName].ProcessQuota(context.TODO(), tst.instName,
				tst.inst, fakeBag{}, newFakeExpr(nil), *h, adapter.QuotaArgs{})

			if tst.wantError != "" {
				if !strings.Contains(err.Error(), tst.wantError) {
					t.Errorf("ProcessQuotaSample got error = %s, want %s", err.Error(), tst.wantError)
				}
			} else {
				v := (*h).(*fakeQuotaHandler).procCallInput
				if !reflect.DeepEqual(v, tst.wantInstance) {
					t.Errorf("ProcessQuotaSample handler "+
						"invoked value = %v want %v", spew.Sdump(v), spew.Sdump(tst.wantInstance))
				}
				if !reflect.DeepEqual(tst.wantQuotaResult, res) {
					t.Errorf("ProcessQuotaSample result = %v want %v", res, spew.Sdump(tst.wantQuotaResult))
				}
			}
		})
	}
}

func TestInferTypeForApa(t *testing.T) {
	for _, tst := range []inferTypeTest{
		{
			name: "Valid",
			instYamlCfg: `
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
optionalIP: 'ip("0.0.0.0")'
optionalString: '"unknown"'
dimensionsFixedInt64ValueDType:
 d1: source.int64
 d1: source.int64
timeStamp: source.timestamp
duration: source.duration
attribute_bindings:
  source.int64: $out.int64Primitive
  source.bool: $out.boolPrimitive
  source.double: $out.doublePrimitive
  source.string: $out.stringPrimitive
  source.timestamp: $out.timeStamp
  source.duration: $out.duration
`,
			cstrParam:     &istio_mixer_adapter_sample_myapa.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "",
			willPanic:     false,
		},
		{
			name: "InferredTypeNotMatchStaticType",
			instYamlCfg: `
int64Primitive: source.timestamp
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
dimensionsFixedInt64ValueDType:
 d1: source.int64
 d1: source.int64
timeStamp: source.timestamp
duration: source.duration
attribute_bindings:
  source.int64: $out.int64Primitive
`,
			cstrParam:     &istio_mixer_adapter_sample_myapa.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "type checking for field 'Int64Primitive': Evaluated expression type TIMESTAMP want INT64",
			willPanic:     false,
		},
		{
			name: "InferredTypeNotMatchInAttrBinding",
			instYamlCfg: `
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
dimensionsFixedInt64ValueDType:
 d1: source.int64
 d1: source.int64
timeStamp: source.timestamp
duration: source.duration
attribute_bindings:
  source.int64: $out.timeStamp
`,
			cstrParam:     &istio_mixer_adapter_sample_myapa.InstanceParam{},
			typeEvalError: nil,
			wantErr: "type 'INT64' for attribute 'source.int64' does not match type 'TIMESTAMP' for expression " +
				"'istio_mixer_adapter_sample_myapa.output.timeStamp'",
			willPanic: false,
		},
		{
			name: "InferredTypeAttrNotFoundInAttrBinding",
			instYamlCfg: `
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
dimensionsFixedInt64ValueDType:
 d1: source.int64
 d1: source.int64
timeStamp: source.timestamp
duration: source.duration
attribute_bindings:
  source.notfound: $out.timeStamp
`,
			cstrParam:     &istio_mixer_adapter_sample_myapa.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "error evaluating AttributeBinding expression for attribute key 'source.notfound': unknown attribute source.notfound",
			willPanic:     false,
		},
		{
			name: "InferredTypeAttrNotFoundInAttrBindingOutExpr",
			instYamlCfg: `
int64Primitive: source.int64
boolPrimitive: source.bool
doublePrimitive: source.double
stringPrimitive: source.string
dimensionsFixedInt64ValueDType:
 d1: source.int64
 d1: source.int64
timeStamp: source.timestamp
duration: source.duration
attribute_bindings:
  source.int64: $out.notfound
`,
			cstrParam:     &istio_mixer_adapter_sample_myapa.InstanceParam{},
			typeEvalError: nil,
			wantErr: "error evaluating AttributeBinding expression 'istio_mixer_adapter_sample_myapa.output.notfound' " +
				"for attribute 'source.int64': unknown attribute istio_mixer_adapter_sample_myapa.output.notfound",
			willPanic: false,
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			cp := tst.cstrParam
			err := fillProto(tst.instYamlCfg, cp)
			if err != nil {
				t.Fatalf("cannot load yaml %v", err)
			}
			_ = getExprEvalFunc(tst.typeEvalError)
			defer func() {
				r := recover()
				if tst.willPanic && r == nil {
					t.Errorf("Expected to recover from panic for %s, but recover was nil.", tst.name)
				} else if !tst.willPanic && r != nil {
					t.Errorf("got panic %v, expected success.", r)
				}
			}()
			ex := newFakeExpr(SupportedTmplInfo[istio_mixer_adapter_sample_myapa.TemplateName].AttributeManifests)
			cv, cerr := SupportedTmplInfo[istio_mixer_adapter_sample_myapa.TemplateName].InferType(cp.(proto.Message), func(s string) (pb.ValueType, error) {
				return ex.EvalType(s, createAttributeDescriptorFinder(SupportedTmplInfo[istio_mixer_adapter_sample_myapa.TemplateName].AttributeManifests))
			})
			if tst.wantErr == "" {
				if cerr != nil {
					t.Errorf("got err %v\nwant <nil>", cerr)
				}
				if cv != nil {
					t.Errorf("cv should be nil, there should be no type for apa instances")
				}
			} else {
				if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
					t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
				}
			}
		})
	}
}

func TestProcessApa(t *testing.T) {
	for _, tst := range []struct {
		name         string
		instName     string
		instParam    proto.Message
		wantInstance interface{}
		hdlr         adapter.Handler
		wantOutAttrs map[string]interface{}
		wantError    string
	}{
		{
			name:     "Valid",
			instName: "foo",
			instParam: &istio_mixer_adapter_sample_myapa.InstanceParam{
				BoolPrimitive:                  "true",
				DoublePrimitive:                "1.2",
				Int64Primitive:                 "54362",
				StringPrimitive:                `"mystring"`,
				TimeStamp:                      "request.timestamp",
				Duration:                       "request.duration",
				OptionalIP:                     `ip("0.0.0.0")`,
				OptionalString:                 `""`,
				DimensionsFixedInt64ValueDType: map[string]string{"a": "1"},
				Res3Map: map[string]*istio_mixer_adapter_sample_myapa.Resource3InstanceParam{
					"source2": {
						BoolPrimitive:   "true",
						DoublePrimitive: "1.2",
						Int64Primitive:  "54362",
						StringPrimitive: `"mystring"`,
						TimeStamp:       "request.timestamp",
						Duration:        "request.duration",
					},
				},
				AttributeBindings: map[string]string{
					"source.myint64Primitive":  "$out.int64Primitive",
					"source.myboolPrimitive":   "$out.boolPrimitive",
					"source.mydoublePrimitive": "$out.doublePrimitive",
					"source.mystring":          "$out.stringPrimitive",
					"source.mytimeStamp":       "$out.timeStamp",
					"source.myduration":        "$out.duration",
				},
			},
			hdlr: &fakeMyApaHandler{
				retOutput: &istio_mixer_adapter_sample_myapa.Output{
					BoolPrimitive:   true,
					DoublePrimitive: 1237,
					StringPrimitive: "1237",
					TimeStamp:       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
					Duration:        10 * time.Second,
					Int64Primitive:  1237,
				},
			},
			wantOutAttrs: map[string]interface{}{
				"source.mystring":          "1237",
				"source.mytimeStamp":       time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
				"source.myduration":        10 * time.Second,
				"source.myint64Primitive":  int64(1237),
				"source.myboolPrimitive":   true,
				"source.mydoublePrimitive": float64(1237),
			},
			wantInstance: &istio_mixer_adapter_sample_myapa.Instance{
				Name:                           "foo",
				BoolPrimitive:                  true,
				DoublePrimitive:                1.2,
				Int64Primitive:                 54362,
				StringPrimitive:                "mystring",
				TimeStamp:                      time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
				Duration:                       10 * time.Second,
				DimensionsFixedInt64ValueDType: map[string]int64{"a": int64(1)},
				OptionalIP:                     net.ParseIP("0.0.0.0"),
				OptionalString:                 "",
				Res3Map: map[string]*istio_mixer_adapter_sample_myapa.Resource3{
					"source2": {
						BoolPrimitive:                  true,
						DoublePrimitive:                1.2,
						Int64Primitive:                 54362,
						StringPrimitive:                "mystring",
						TimeStamp:                      time.Date(2017, time.January, 01, 0, 0, 0, 0, time.UTC),
						Duration:                       10 * time.Second,
						DimensionsFixedInt64ValueDType: map[string]int64{},
					},
				},
			},
		},
		{
			name:     "EvalError",
			instName: "foo",
			instParam: &istio_mixer_adapter_sample_myapa.InstanceParam{
				Int64Primitive: "bad.attribute",
			},
			wantError: "unknown attribute bad.attribute",
		},
		{
			name:     "ProcessError",
			instName: "foo",
			instParam: &istio_mixer_adapter_sample_myapa.InstanceParam{
				BoolPrimitive:                  "true",
				DoublePrimitive:                "1.2",
				Int64Primitive:                 "54362",
				StringPrimitive:                `"mystring"`,
				TimeStamp:                      "request.timestamp",
				Duration:                       "request.duration",
				DimensionsFixedInt64ValueDType: map[string]string{"a": "1"},
				OptionalIP:                     `ip("0.0.0.0")`,
				OptionalString:                 `""`,
				Res3Map: map[string]*istio_mixer_adapter_sample_myapa.Resource3InstanceParam{
					"source2": {
						BoolPrimitive:   "true",
						DoublePrimitive: "1.2",
						Int64Primitive:  "54362",
						StringPrimitive: `"mystring"`,
						TimeStamp:       "request.timestamp",
						Duration:        "request.duration",
					},
				},
				AttributeBindings: map[string]string{
					"source.myint64Primitive":  "$out.int64Primitive",
					"source.myboolPrimitive":   "$out.boolPrimitive",
					"source.mydoublePrimitive": "$out.doublePrimitive",
					"source.mystring":          "$out.stringPrimitive",
					"source.mytimeStamp":       "$out.timeStamp",
					"source.myduration":        "$out.duration",
				},
			},
			hdlr: &fakeMyApaHandler{
				retError: fmt.Errorf("some error"),
			},
			wantError: "some error",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			h := &tst.hdlr
			returnAttr, err := SupportedTmplInfo[istio_mixer_adapter_sample_myapa.TemplateName].ProcessGenAttrs(
				context.TODO(),
				tst.instName,
				tst.instParam,
				fakeBag{},
				newFakeExpr(SupportedTmplInfo[istio_mixer_adapter_sample_myapa.TemplateName].AttributeManifests),
				*h)
			if tst.wantError != "" {
				if !strings.Contains(err.Error(), tst.wantError) {
					t.Errorf("TestProcessApa got error = %s, want %s", err.Error(), tst.wantError)
				}
			} else {
				if err != nil {
					t.Fatalf("got error; want success: error %v", err)
				}
				v := (*h).(*fakeMyApaHandler).procCallInput
				if !reflect.DeepEqual(v, tst.wantInstance) {
					t.Errorf("Apa handler "+
						"invoked value = %v want %v", spew.Sdump(v), spew.Sdump(tst.wantInstance))
				}

				if len(returnAttr.Names()) != len(tst.wantOutAttrs) {
					t.Fatalf("Apa handler "+
						"return attrs = %v want %v", spew.Sdump(returnAttr), spew.Sdump(tst.wantOutAttrs))
				}
				for k, v := range tst.wantOutAttrs {
					if x, _ := returnAttr.Get(k); x != v {
						t.Errorf("Apa handler "+
							"return attattrs = %v want %v", spew.Sdump(returnAttr), spew.Sdump(tst.wantOutAttrs))
					}
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

// nolint: unparam
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
