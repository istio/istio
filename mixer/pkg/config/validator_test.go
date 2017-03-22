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
	"fmt"
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	listcheckerpb "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config/descriptor"
	"istio.io/mixer/pkg/expr"
)

type fakeVFinder struct {
	ada   map[string]adapter.ConfigValidator
	asp   map[string]AspectValidator
	kinds []string
}

func (f *fakeVFinder) FindAdapterValidator(name string) (adapter.ConfigValidator, bool) {
	v, found := f.ada[name]
	return v, found
}

func (f *fakeVFinder) FindAspectValidator(name string) (AspectValidator, bool) {
	v, found := f.asp[name]
	return v, found
}

func (f *fakeVFinder) AdapterToAspectMapperFunc(impl string) []string {
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
	asp      map[string]AspectValidator
	nerrors  int
	selector string
	strict   bool
	cfg      string
}

func newVfinder(ada map[string]adapter.ConfigValidator, asp map[string]AspectValidator) *fakeVFinder {
	kinds := []string{}
	for k := range ada {
		kinds = append(kinds, k)
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
			nil, 0, "service.name == “*”", false, sGlobalConfig},
		{nil,
			map[string]adapter.ConfigValidator{
				"metrics":  &lc{},
				"metrics2": &lc{},
			},
			nil, 1, "service.name == “*”", false, sGlobalConfig},
		{nil, nil,
			map[string]AspectValidator{
				"metrics":  &ac{},
				"metrics2": &ac{},
			},
			0, "service.name == “*”", false, sSvcConfig},
		{nil, nil,
			map[string]AspectValidator{
				"metrics":  &ac{},
				"metrics2": &ac{},
			},
			1, "service.name == “*”", true, sSvcConfig},
		{cerr, nil,
			map[string]AspectValidator{
				"metrics2": &ac{ce: cerr},
			},
			2, "service.name == “*”", false, sSvcConfig},
		{ct.Append("/:metrics", UnknownValidator("metrics")),
			nil, nil, 2, "\"\"", false, sSvcConfig},
	}

	for idx, tt := range tests {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {

			var ce *adapter.ConfigErrors
			mgr := newVfinder(tt.ada, tt.asp)
			p := NewValidator(mgr.FindAspectValidator, mgr.FindAdapterValidator, mgr.AdapterToAspectMapperFunc, tt.strict, evaluator)
			if tt.cfg == sSvcConfig {
				ce = p.validateServiceConfig(fmt.Sprintf(tt.cfg, tt.selector), false)
			} else {
				ce = p.validateGlobalConfig(tt.cfg)
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
		asp      map[string]AspectValidator
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
			map[string]AspectValidator{
				"denyChecker": &ac{},
				"metrics":     &ac{},
				"listchecker": &ac{},
			},
			"service.name == “*”", false, sSvcConfig2, nil},
		{nil,
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[string]AspectValidator{
				"denyChecker": &ac{},
				"metrics":     &ac{},
				"listchecker": &ac{},
			},
			"", false, sSvcConfig2, nil},
		{&adapter.ConfigError{Field: "NamedAdapter", Underlying: fmt.Errorf("listchecker//denychecker.2 not available")},
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[string]AspectValidator{
				"denyChecker": &ac{},
				"metrics":     &ac{},
				"listchecker": &ac{},
			},
			"", false, sSvcConfig3, nil},
		{&adapter.ConfigError{Field: ":Selector service.name == “*”", Underlying: fmt.Errorf("invalid expression")},
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			},
			map[string]AspectValidator{
				"denyChecker": &ac{},
				"metrics":     &ac{},
				"listchecker": &ac{},
			},
			"service.name == “*”", false, sSvcConfig1, fmt.Errorf("invalid expression")},
	}
	for idx, ctx := range ctable {
		tt.Run(fmt.Sprintf("[%d]", idx), func(t *testing.T) {
			mgr := newVfinder(ctx.ada, ctx.asp)
			fe.err = ctx.exprErr
			p := NewValidator(mgr.FindAspectValidator, mgr.FindAdapterValidator, mgr.AdapterToAspectMapperFunc, ctx.strict, fe)
			// sGlobalConfig only defines 1 adapter: denyChecker
			_, ce := p.Validate(ctx.cfg, sGlobalConfig)
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

func TestConfigParseError(t *testing.T) {
	mgr := &fakeVFinder{}
	evaluator := newFakeExpr()
	p := NewValidator(mgr.FindAspectValidator, mgr.FindAdapterValidator, mgr.AdapterToAspectMapperFunc, false, evaluator)
	ce := p.validateServiceConfig("<config>  </config>", false)

	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}

	ce = p.validateGlobalConfig("<config>  </config>")

	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}

	_, ce = p.Validate("<config>  </config>", "<config>  </config>")
	if ce == nil || !strings.Contains(ce.Error(), "error unmarshaling") {
		t.Error("Expected unmarshal Error", ce)
	}
}

func TestDecoderError(t *testing.T) {
	err := Decode(make(chan int), nil, true)
	if err == nil {
		t.Error("Expected json encode error")
	}
}

const sGlobalConfigValid = `
subject: "namespace:ns"
revision: "2022"
adapters:
  - name: default
    kind: denials
    impl: denyChecker
    params:
      check_attribute: src.ip
      blacklist: true
`
const sGlobalConfig = sGlobalConfigValid + `
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
  - kind: listchecker
    inputs: {}
    params:
`
const sSvcConfig3 = `
subject: namespace:ns
revision: "2022"
rules:
- selector: service.name == “*”
  aspects:
  - kind: listchecker
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
      check_attribute: src.ip
      blacklist: true
      unknown_field: true
  rules:
  - selector: src.name == "abc"
    aspects:
    - kind: metrics2
      adapter: ""
      inputs: {}
      params:
        check_attribute: src.ip
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
