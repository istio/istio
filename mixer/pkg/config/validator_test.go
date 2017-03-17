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
	"errors"
	"fmt"
	"strings"
	"testing"

	"istio.io/mixer/pkg/adapter"
	listcheckerpb "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
)

type fakeVFinder struct {
	v     map[string]adapter.ConfigValidator
	kinds []string
}

func (f *fakeVFinder) FindValidator(name string) (adapter.ConfigValidator, bool) {
	v, found := f.v[name]
	return v, found
}

func (f *fakeVFinder) AdapterToAspectMapperFunc(impl string) []string {
	return f.kinds
}

type lc struct {
	ce *adapter.ConfigErrors
}

func (m *lc) DefaultConfig() (c adapter.AspectConfig) {
	return &listcheckerpb.ListsParams{}
}

// ValidateConfig determines whether the given configuration meets all correctness requirements.
func (m *lc) ValidateConfig(c adapter.AspectConfig) *adapter.ConfigErrors {
	return m.ce
}

type configTable struct {
	cerr     *adapter.ConfigErrors
	v        map[string]adapter.ConfigValidator
	nerrors  int
	selector string
	strict   bool
	cfg      string
}

func newVfinder(v map[string]adapter.ConfigValidator) *fakeVFinder {
	kinds := []string{}
	for k := range v {
		kinds = append(kinds, k)
	}
	return &fakeVFinder{v: v, kinds: kinds}
}

func TestConfigValidatorError(t *testing.T) {
	var ct *adapter.ConfigErrors
	evaluator := newFakeExpr()
	cerr := ct.Appendf("Url", "Must have a valid URL")

	ctable := []*configTable{
		{nil,
			map[string]adapter.ConfigValidator{
				"metrics":  &lc{},
				"metrics2": &lc{},
			}, 0, "service.name == “*”", false, sSvcConfig},
		{nil,
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics2":    &lc{},
			}, 0, "service.name == “*”", false, sGlobalConfig},
		{nil,
			map[string]adapter.ConfigValidator{
				"metrics":  &lc{},
				"metrics2": &lc{},
			}, 1, "service.name == “*”", false, sGlobalConfig},
		{nil,
			map[string]adapter.ConfigValidator{
				"metrics":  &lc{},
				"metrics2": &lc{},
			}, 1, "service.name == “*”", true, sSvcConfig},
		{cerr,
			map[string]adapter.ConfigValidator{
				"metrics": &lc{ce: cerr},
			}, 2, "service.name == “*”", false, sSvcConfig},
		{ct.Append("/:metrics", UnknownValidator("metrics")),
			nil, 2, "\"\"", false, sSvcConfig},
	}

	for idx, ctx := range ctable {
		var ce *adapter.ConfigErrors
		mgr := newVfinder(ctx.v)
		p := NewValidator(mgr.FindValidator, mgr.FindValidator, mgr.AdapterToAspectMapperFunc, ctx.strict, evaluator)
		if ctx.cfg == sSvcConfig {
			ce = p.validateServiceConfig(fmt.Sprintf(ctx.cfg, ctx.selector), false)
		} else {
			ce = p.validateGlobalConfig(ctx.cfg)
		}
		cok := ce == nil
		ok := ctx.nerrors == 0

		if ok != cok {
			t.Errorf("%d Expected %t Got %t", idx, ok, cok)
		}
		if ce == nil {
			continue
		}

		if len(ce.Multi.Errors) != ctx.nerrors {
			t.Error(idx, "\nExpected:", ctx.cerr.Error(), "\nGot:", ce.Error(), len(ce.Multi.Errors), ctx.nerrors)
		}
	}
}

func TestFullConfigValidator(tt *testing.T) {
	fe := newFakeExpr()
	ctable := []struct {
		cerr     *adapter.ConfigError
		v        map[string]adapter.ConfigValidator
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
			}, "service.name == “*”", false, sSvcConfig2, nil},
		{nil,
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			}, "", false, sSvcConfig2, nil},
		{&adapter.ConfigError{Field: "NamedAdapter", Underlying: errors.New("listchecker//denychecker.2 not available")},
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			}, "", false, sSvcConfig3, nil},
		{&adapter.ConfigError{Field: ":Selector service.name == “*”", Underlying: errors.New("invalid expression")},
			map[string]adapter.ConfigValidator{
				"denyChecker": &lc{},
				"metrics":     &lc{},
				"listchecker": &lc{},
			}, "service.name == “*”", false, sSvcConfig1, errors.New("invalid expression")},
	}
	for idx, ctx := range ctable {
		tt.Run(fmt.Sprintf("%s", ctx.v), func(t *testing.T) {
			mgr := newVfinder(ctx.v)
			fe.err = ctx.exprErr
			p := NewValidator(mgr.FindValidator, mgr.FindValidator, mgr.AdapterToAspectMapperFunc, ctx.strict, fe)
			// sGlobalConfig only defines 1 adapter: denyChecker
			_, ce := p.Validate(ctx.cfg, sGlobalConfig)
			cok := ce == nil
			ok := ctx.cerr == nil
			if ok != cok {
				t.Errorf("%d Expected %t Got %t", idx, ok, cok)

			}
			if ce == nil {
				return
			}
			if len(ce.Multi.Errors) < 2 {
				t.Error("expected at least 2 errors reported")
				return
			}
			if !strings.Contains(ce.Multi.Errors[1].Error(), ctx.cerr.Error()) {
				t.Errorf("%d got: %#v\nwant: %#v\n", idx, ce.Multi.Errors[1].Error(), ctx.cerr.Error())
			}
		})
	}
}

func TestConfigParseError(t *testing.T) {
	mgr := &fakeVFinder{}
	evaluator := newFakeExpr()
	p := NewValidator(mgr.FindValidator, mgr.FindValidator, mgr.AdapterToAspectMapperFunc, false, evaluator)
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
func (e *fakeExpr) Eval(mapExpression string, attrs attribute.Bag) (v interface{}, err error) {
	var found bool

	v, found = attrs.String(mapExpression)
	if found {
		return
	}

	v, found = attrs.Bool(mapExpression)
	if found {
		return
	}

	v, found = attrs.Int64(mapExpression)
	if found {
		return
	}

	v, found = attrs.Float64(mapExpression)
	if found {
		return
	}

	return v, UnboundVariable(mapExpression)
}

// EvalString evaluates given expression using the attribute bag to a string
func (e *fakeExpr) EvalString(mapExpression string, attrs attribute.Bag) (v string, err error) {
	var found bool
	v, found = attrs.String(mapExpression)
	if found {
		return
	}
	return v, UnboundVariable(mapExpression)
}

// EvalPredicate evaluates given predicate using the attribute bag
func (e *fakeExpr) EvalPredicate(mapExpression string, attrs attribute.Bag) (v bool, err error) {
	var found bool
	v, found = attrs.Bool(mapExpression)
	if found {
		return
	}
	return v, UnboundVariable(mapExpression)
}

func (e *fakeExpr) Validate(expression string) error { return e.err }
