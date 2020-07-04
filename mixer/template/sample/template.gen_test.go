// Copyright Istio Authors
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

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"

	adpTmpl "istio.io/api/mixer/adapter/model/v1beta1"
	pb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/lang/checker"
	istio_mixer_adapter_sample_myapa "istio.io/istio/mixer/template/sample/apa"
	sample_check "istio.io/istio/mixer/template/sample/check"
	sample_quota "istio.io/istio/mixer/template/sample/quota"
	sample_report "istio.io/istio/mixer/template/sample/report"
	"istio.io/pkg/attribute"
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
		if err != nil {
			return pb.VALUE_TYPE_UNSPECIFIED, err
		}
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

		if retType == pb.VALUE_TYPE_UNSPECIFIED {
			tc := checker.NewTypeChecker(createAttributeDescriptorFinder(nil))
			retType, err = tc.EvalType(expr)
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
  Res2:
    value: source.int64
    int64Primitive: source.int64
    dns_name: source.dns
    duration: source.duration
    email_addr: source.email
    ip_addr: 'ip("0.0.0.0")'
    timeStamp: source.timestamp
    uri: source.uri
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
					Res2: &sample_report.Res2Type{
						Value:      pb.INT64,
						Dimensions: map[string]pb.ValueType{},
					},
					Res2Map: map[string]*sample_report.Res2Type{},
				},
			},
		},
		{
			name: "MissingAFieldValid",
			instYamlCfg: `
# value: source.int64 # missing ValueType field
# int64Primitive: source.int64 # missing int64Primitive
# boolPrimitive: source.bool # missing int64Primitive
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
#dimensions: # missing int64Primitive
#  source: source.string
#  target: source.string
res1:
  # value: source.int64 # missing ValueType field
  # int64Primitive: source.int64 # missing int64Primitive
  boolPrimitive: source.bool
  # doublePrimitive: source.double
  # stringPrimitive: source.string
  # timeStamp: source.timestamp
  # duration: source.duration
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "",
			willPanic:     false,
			wantType: &sample_report.Type{
				Value:      pb.VALUE_TYPE_UNSPECIFIED,
				Dimensions: map[string]pb.ValueType{},
				Res1: &sample_report.Res1Type{
					Value:      pb.VALUE_TYPE_UNSPECIFIED,
					Dimensions: map[string]pb.ValueType{},
					Res2:       nil,
					Res2Map:    map[string]*sample_report.Res2Type{},
				},
			},
		},
		{
			name: "NotValidMissingExpressionInMap",
			instYamlCfg: `
# value: source.int64 # missing ValueType field
# int64Primitive: source.int64 # missing int64Primitive
# boolPrimitive: source.bool # missing boolPrimitive
doublePrimitive: source.double
stringPrimitive: source.string
timeStamp: source.timestamp
duration: source.duration
dimensions:
# bad expression below.
  source:
`,
			cstrParam:     &sample_report.InstanceParam{},
			typeEvalError: nil,
			wantErr:       "failed to evaluate expression for field 'Dimensions[source]'",
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
			cstrParam: &sample_report.InstanceParam{},
			wantType: &sample_report.Type{
				Value:      pb.INT64,
				Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target": pb.STRING},
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
			} else if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
				t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
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
			} else if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
				t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
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
  env: destination.string
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
    env: destination.string
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
			} else if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
				t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
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
	extraAttrManifest []*pb.AttributeManifest
}

// newFakeExpr returns the basic
func newFakeExpr(extraAttrManifest []*pb.AttributeManifest) *fakeExpr {
	return &fakeExpr{extraAttrManifest: extraAttrManifest}
}

func (e *fakeExpr) Eval(mapExpression string, attrs attribute.Bag) (interface{}, error) {
	return nil, nil
}

var baseManifests = []*pb.AttributeManifest{
	{
		Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
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
			"source.email": {
				ValueType: pb.EMAIL_ADDRESS,
			},
			"source.uri": {
				ValueType: pb.URI,
			},
			"source.labels": {
				ValueType: pb.STRING_MAP,
			},
			"source.dns": {
				ValueType: pb.DNS_NAME,
			},
		},
	},
}

func createAttributeDescriptorFinder(extraAttrManifest []*pb.AttributeManifest) attribute.AttributeDescriptorFinder {
	attrs := make(map[string]*pb.AttributeManifest_AttributeInfo)
	for _, m := range baseManifests {
		for an, at := range m.Attributes {
			attrs[an] = at
		}
	}
	for _, m := range extraAttrManifest {
		for an, at := range m.Attributes {
			attrs[an] = at
		}
	}
	return attribute.NewFinder(attrs)
}

// EvalPredicate evaluates given predicate using the attribute bag
func (e *fakeExpr) EvalPredicate(mapExpression string, attrs attribute.Bag) (bool, error) {
	return true, nil
}

func (e *fakeExpr) EvalType(s string, af attribute.AttributeDescriptorFinder) (pb.ValueType, error) {
	if i := af.GetAttribute(s); i != nil {
		return i.ValueType, nil
	}
	tc := checker.NewTypeChecker(af)
	return tc.EvalType(s)
}

func (e *fakeExpr) AssertType(string, attribute.AttributeDescriptorFinder, pb.ValueType) error {
	return nil
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
email: source.email
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
  source.labels: $out.out_str_map
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
			} else if cerr == nil || !strings.Contains(cerr.Error(), tst.wantErr) {
				t.Errorf("got error %v\nwant %v", cerr, tst.wantErr)
			}
		})
	}
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
