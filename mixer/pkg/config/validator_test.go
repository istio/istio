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

package config

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	listcheckerpb "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type fakeVFinder struct {
	ada   map[string]adapter.ConfigValidator
	asp   map[Kind]AspectValidator
	kinds KindSet
}

func (f *fakeVFinder) FindAdapterValidator(name string) (adapter.ConfigValidator, bool) {
	v, found := f.ada[name]
	return v, found
}

func (f *fakeVFinder) FindAspectValidator(kind Kind) (AspectValidator, bool) {
	v, found := f.asp[kind]
	return v, found
}

func (f *fakeVFinder) AdapterToAspectMapperFunc(builder string) KindSet {
	return f.kinds
}

type lc struct {
	ce *adapter.ConfigErrors
}

func (m *lc) DefaultConfig() (c adapter.Config) {
	return &listcheckerpb.ListsParams{}
}

// ValidateConfig determines whether the given configuration meets all correctness requirements.
func (m *lc) ValidateConfig(c adapter.Config) *adapter.ConfigErrors {
	return m.ce
}

type ac struct {
	ce *adapter.ConfigErrors
}

func (*ac) DefaultConfig() AspectParams {
	return &listcheckerpb.ListsParams{}
}

// ValidateConfig determines whether the given configuration meets all correctness requirements.
func (a *ac) ValidateConfig(AspectParams, expr.Validator, descriptor.Finder) *adapter.ConfigErrors {
	return a.ce
}

type configTable struct {
	cerr     *adapter.ConfigErrors
	ada      map[string]adapter.ConfigValidator
	asp      map[Kind]AspectValidator
	nerrors  int
	selector string
	strict   bool
	cfg      string
}

func newVfinder(ada map[string]adapter.ConfigValidator, asp map[Kind]AspectValidator) *fakeVFinder {
	var kinds KindSet
	for k := range asp {
		kinds = kinds.Set(k)
	}
	return &fakeVFinder{ada: ada, asp: asp, kinds: kinds}
}

func TestConfigValidatorError(t *testing.T) {
	var ct *adapter.ConfigErrors
	evaluator := newFakeExpr()
	cerr := ct.Appendf("Url", "Must have a valid URL")

	tests := []*configTable{
		{nil,
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics2":    &lc{},
			},
			nil, 0, "service.name == “*”", false, ConstGlobalConfig},
		{nil,
			map[string]adapter.ConfigValidator{
				"metrics":  &lc{},
				"metrics2": &lc{},
			},
			nil, 1, "service.name == “*”", false, ConstGlobalConfig},
		{nil, nil,
			map[Kind]AspectValidator{
				MetricsKind: &ac{},
				QuotasKind:  &ac{},
			},
			0, "service.name == “*”", false, sSvcConfig},
		{nil, nil,
			map[Kind]AspectValidator{
				MetricsKind: &ac{},
				QuotasKind:  &ac{},
			},
			1, "service.name == “*”", true, sSvcConfig},
		{cerr, nil,
			map[Kind]AspectValidator{
				QuotasKind: &ac{ce: cerr},
			},
			2, "service.name == “*”", false, sSvcConfig},
		{ct.Append("/:metrics", unknownValidator("metrics")),
			nil, nil, 2, "\"\"", false, sSvcConfig},
	}

	for idx, tt := range tests {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {

			var ce *adapter.ConfigErrors
			mgr := newVfinder(tt.ada, tt.asp)
			p := newValidator(mgr.FindAspectValidator, mgr.FindAdapterValidator, mgr.AdapterToAspectMapperFunc, tt.strict, evaluator)
			if tt.cfg == sSvcConfig {
				ce = p.validateServiceConfig(globalRulesKey, fmt.Sprintf(tt.cfg, tt.selector), false)
			} else {
				ce = p.validateAdapters(keyAdapters, tt.cfg)
			}
			cok := ce == nil
			ok := tt.nerrors == 0

			if ok != cok {
				t.Fatalf("Expected %t Got %t", ok, cok)
			}
			if ce == nil {
				return
			}

			if len(ce.Multi.Errors) != tt.nerrors {
				t.Fatalf("Expected: '%v' Got: %v", tt.cerr.Error(), ce.Error())
			}
		})
	}
}

func TestFullConfigValidator(tt *testing.T) {
	fe := newFakeExpr()
	ctable := []struct {
		cerr     *adapter.ConfigError
		ada      map[string]adapter.ConfigValidator
		asp      map[Kind]AspectValidator
		selector string
		strict   bool
		cfg      string
		exprErr  error
	}{
		{nil,
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[Kind]AspectValidator{
				DenialsKind: &ac{},
				MetricsKind: &ac{},
				ListsKind:   &ac{},
			},
			"service.name == “*”", false, sSvcConfig2, nil},
		{nil,
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[Kind]AspectValidator{
				DenialsKind: &ac{},
				MetricsKind: &ac{},
				ListsKind:   &ac{},
			},
			"", false, sSvcConfig2, nil},
		{&adapter.ConfigError{Field: "NamedAdapter", Underlying: errors.New("lists//denychecker.2 not available")},
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[Kind]AspectValidator{
				DenialsKind: &ac{},
				MetricsKind: &ac{},
				ListsKind:   &ac{},
			},
			"", false, sSvcConfig3, nil},
		{&adapter.ConfigError{Field: ":Selector service.name == “*”", Underlying: errors.New("invalid expression")},
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[Kind]AspectValidator{
				DenialsKind: &ac{},
				MetricsKind: &ac{},
				ListsKind:   &ac{},
			},
			"service.name == “*”", false, sSvcConfig1, errors.New("invalid expression")},
	}
	for idx, ctx := range ctable {
		tt.Run(fmt.Sprintf("[%d]", idx), func(t *testing.T) {
			mgr := newVfinder(ctx.ada, ctx.asp)
			fe.err = ctx.exprErr
			p := newValidator(mgr.FindAspectValidator, mgr.FindAdapterValidator, mgr.AdapterToAspectMapperFunc, ctx.strict, fe)
			// ConstGlobalConfig only defines 1 adapter: denyChecker
			_, ce := p.validate(newFakeMap(ConstGlobalConfig, ctx.cfg))
			cok := ce == nil
			ok := ctx.cerr == nil
			if ok != cok {
				t.Fatalf("%d Expected %t Got %t", idx, ok, cok)

			}
			if ce == nil {
				return
			}
			if len(ce.Multi.Errors) < 2 {
				t.Fatal("expected at least 2 errors reported")
			}
			if !strings.Contains(ce.Multi.Errors[1].Error(), ctx.cerr.Error()) {
				t.Fatalf("%d got: %#v\nwant: %#v\n", idx, ce.Multi.Errors[1].Error(), ctx.cerr.Error())
			}
		})
	}
}

// globalRulesKey this rule set applies to all requests
// so we create a well known key for it
var globalRulesKey = rulesKey{Scope: global, Subject: global}

func TestConfigParseError(t *testing.T) {
	mgr := &fakeVFinder{}
	evaluator := newFakeExpr()
	p := newValidator(mgr.FindAspectValidator, mgr.FindAdapterValidator, mgr.AdapterToAspectMapperFunc, false, evaluator)
	ce := p.validateServiceConfig(globalRulesKey, "<config>  </config>", false)

	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}
	ce = p.validateAdapters("", "<config>  </config>")

	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}

	ce = p.validateDescriptors("", "<config>  </config>")

	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}
	_, ce = p.validate(map[string]string{
		keyGlobalServiceConfig: "<config>  </config>",
		keyAdapters:            "<config>  </config>",
		keyDescriptors:         "<config>  </config>",
	})
	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}
}

func TestDecoderError(t *testing.T) {
	err := decode(make(chan int), nil, true)
	if err == nil {
		t.Error("Expected json encode error")
	}
}

const ConstGlobalConfigValid = `
subject: "namespace:ns"
revision: "2022"
adapters:
  - name: default
    kind: denials
    impl: denyChecker
    params:
      check_expression: src.ip
      blacklist: true
`
const ConstGlobalConfig = ConstGlobalConfigValid + `
      unknown_field: true
`

const sSvcConfig1 = `
subject: "namespace:ns"
revision: "2022"
rules:
- selector: service.name == “*”
  aspects:
  - kind: metrics
    params:
      metrics:
      - name: response_time_by_consumer
        value: metric_response_time
        metric_kind: DELTA
        labels:
        - key: target_consumer_id
`
const sSvcConfig2 = `
subject: namespace:ns
revision: "2022"
rules:
- selector: service.name == “*”
  aspects:
  - kind: lists
    inputs: {}
    params:
`
const sSvcConfig3 = `
subject: namespace:ns
revision: "2022"
rules:
- selector: service.name == “*”
  aspects:
  - kind: lists
    inputs: {}
    params:
    adapter: denychecker.2
`
const sSvcConfig = `
subject: namespace:ns
revision: "2022"
rules:
- selector: %s
  aspects:
  - kind: metrics
    adapter: ""
    inputs: {}
    params:
      check_expression: src.ip
      blacklist: true
      unknown_field: true
  rules:
  - selector: src.name == "abc"
    aspects:
    - kind: quotas
      adapter: ""
      inputs: {}
      params:
        check_expression: src.ip
        blacklist: true
`

type fakeExpr struct {
	err error
}

// newFakeExpr returns the basic
func newFakeExpr() *fakeExpr {
	return &fakeExpr{}
}

func UnboundVariable(vname string) error {
	return fmt.Errorf("unbound variable %s", vname)
}

// Eval evaluates given expression using the attribute bag
func (e *fakeExpr) Eval(mapExpression string, attrs attribute.Bag) (interface{}, error) {
	if v, found := attrs.Get(mapExpression); found {
		return v, nil
	}
	return nil, UnboundVariable(mapExpression)
}

// EvalString evaluates given expression using the attribute bag to a string
func (e *fakeExpr) EvalString(mapExpression string, attrs attribute.Bag) (string, error) {
	v, found := attrs.Get(mapExpression)
	if found {
		return v.(string), nil
	}
	return "", UnboundVariable(mapExpression)
}

// EvalPredicate evaluates given predicate using the attribute bag
func (e *fakeExpr) EvalPredicate(mapExpression string, attrs attribute.Bag) (bool, error) {
	v, found := attrs.Get(mapExpression)
	if found {
		return v.(bool), nil
	}
	return false, UnboundVariable(mapExpression)
}

func (e *fakeExpr) Validate(expression string) error { return e.err }
func (e *fakeExpr) TypeCheck(string, expr.AttributeDescriptorFinder) (dpb.ValueType, error) {
	return dpb.VALUE_TYPE_UNSPECIFIED, e.err
}
func (e *fakeExpr) AssertType(string, expr.AttributeDescriptorFinder, dpb.ValueType) error {
	return e.err
}

func TestValidated_Clone(t *testing.T) {
	aa := map[adapterKey]*pb.Adapter{
		{AccessLogsKind, "n1"}: {},
	}

	rule := map[rulesKey]*pb.ServiceConfig{
		{"global", "global"}: {},
	}

	shas := map[string][sha1.Size]byte{
		keyGlobalServiceConfig: {},
	}

	adp := map[string]*pb.GlobalConfig{
		keyAdapters: {},
	}

	desc := map[string]*pb.GlobalConfig{
		keyDescriptors: {},
	}

	v := &Validated{
		adapterByName: aa,
		rule:          rule,
		adapter:       adp,
		descriptor:    desc,
		numAspects:    1,
		shas:          shas,
	}

	v1 := v.Clone()

	if !reflect.DeepEqual(v, v1) {
		t.Errorf("got %v\nwant %v", v1, v)
	}
}

func TestParseConfigKey(t *testing.T) {
	for _, tst := range []struct {
		input string
		key   *rulesKey
	}{
		{keyGlobalServiceConfig, &rulesKey{"global", "global"}},
		{"/scopes/global/subjects/global", nil},
		{"/SCOPES/global/subjects/global/rules", nil},
		{"/scopes/global/SUBJECTS/global/rules", nil},
	} {
		t.Run(tst.input, func(t *testing.T) {
			k := parseRulesKey(tst.input)
			if !reflect.DeepEqual(k, tst.key) {
				t.Errorf("got %s\nwant %s", k, tst.key)
			}
		})
	}
}

func TestUnknownKind(t *testing.T) {
	name := "DOESNOTEXITS"
	want := unknownKind(name)
	_, ce := convertAspectParams(nil, name, "abc", true, nil)
	if ce == nil {
		t.Errorf("got nil\nwant %s", want)
	}
	if !strings.Contains(ce.Error(), want.Error()) {
		t.Errorf("got %s\nwant %s", ce, want)
	}
}

type fakeCV struct {
	err string
	cfg adapter.Config
}

func (f *fakeCV) DefaultConfig() (c adapter.Config) { return f.cfg }

// ValidateConfig determines whether the given configuration meets all correctness requirements.
func (f *fakeCV) ValidateConfig(c adapter.Config) (ce *adapter.ConfigErrors) {
	return ce.Appendf("abc", f.err)
}

func TestConvertAdapterParamsErrors(t *testing.T) {
	for _, tt := range []struct {
		params interface{}
		cv     *fakeCV
	}{{map[string]interface{}{
		"check_expression": "src.ip",
		"blacklist":        "true (this should be bool)",
	}, &fakeCV{cfg: &listcheckerpb.ListsParams{},
		err: "failed to decode"}},
		{map[string]interface{}{
			"check_expression": "src.ip",
			"blacklist":        true,
		}, &fakeCV{cfg: &listcheckerpb.ListsParams{}, err: "failed to unmarshal"}},
	} {
		t.Run(tt.cv.err, func(t *testing.T) {
			_, ce := convertAdapterParams(func(name string) (adapter.ConfigValidator, bool) {
				return tt.cv, true
			}, "ABC", tt.params, true)

			if !strings.Contains(ce.Error(), tt.cv.err) {
				t.Errorf("got %s\nwant %s", ce.Error(), tt.cv.err)
			}
		})
	}
}
