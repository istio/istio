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
	"errors"
	"flag"
	"strings"
	"testing"

	adptTmpl "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il/compiled"
)

type testcase struct {
	desc         string
	bag          map[string]interface{}
	rules        []fakeRuleCfg
	selectReject bool
	selectError  string
	variety      adptTmpl.TemplateVariety
	callVariety  adptTmpl.TemplateVariety
	err          string
	nactions     int
}

func TestResolver_Resolve(t *testing.T) {
	ia := DefaultIdentityAttribute
	ns := DefaultConfigNamespace
	tests := []testcase{
		{
			desc: "success",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
			rules: []fakeRuleCfg{
				{ns, 5},
				{"myns", 3},
			},
			nactions: 8,
		},
		{
			desc: "success - service in default namespace",
			bag: map[string]interface{}{
				ia: "myservice." + ns,
			},
			rules: []fakeRuleCfg{
				{ns, 5},
			},
			nactions: 5,
		},
		{
			desc: "success nothing selected",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
			rules: []fakeRuleCfg{
				{ns, 5},
				{"myns", 3},
			},
			selectReject: true,
			nactions:     0,
		},
		{
			desc: "success no config for variety",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
			rules: []fakeRuleCfg{
				{ns, 5},
				{"myns", 3},
			},
			callVariety: adptTmpl.TEMPLATE_VARIETY_REPORT,
			nactions:    0,
		},
		{
			desc: "success no default ns",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
			rules: []fakeRuleCfg{
				{"myns", 3},
			},
			nactions: 3,
		},
		{
			desc: "success no namespace config",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
			rules: []fakeRuleCfg{
				{ns, 5},
			},
			nactions: 5,
		},
		{
			desc: "success no namespace config tcp",
			bag: map[string]interface{}{
				ia: "myservice.myns",
				ContextProtocolAttributeName: "tcp",
			},
			rules: []fakeRuleCfg{
				{ns, 5},
			},
			nactions: 0,
		},
		{
			desc: "success no config rules",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
		},
		{
			desc: "failure no identity",
			err:  "identity not found",
		},
		{
			desc: "failure match error",
			bag: map[string]interface{}{
				ia: "myservice.myns",
			},
			rules: []fakeRuleCfg{
				{ns, 5},
				{"myns", 3},
			},
			selectError: "invalid match syntax",
			err:         "invalid match",
			nactions:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			rules := newRules(tc.variety, tc.rules, tc.selectReject, tc.selectError)
			bag := attribute.GetFakeMutableBagForTesting(tc.bag)
			var rv Resolver = newResolver(ia, ns, rules, 1)

			act, err := rv.Resolve(bag, tc.callVariety)

			assertResolverError(t, err, tc.err)
			if err != nil {
				return
			}
			// success now check the actions that were selected.
			if len(act.Get()) != tc.nactions {
				t.Fatalf("got %d actions, want %d", len(act.Get()), tc.nactions)
			}
			rx := rv.(*resolver)
			if rx.refCount == 0 {
				t.Fatalf("refcount zero while actions are in use")
			}
			act.Done()
			if rx.refCount != 0 {
				t.Fatalf("refcount non-zero after actions are Done")
			}
		})
	}
}

// fakes and support functions
type fakeCompiledExpression struct {
	reject bool
	err    error
}

var _ compiled.Expression = fakeCompiledExpression{}

func fakeCompiledExpr(reject bool, err string) *fakeCompiledExpression {
	f := &fakeCompiledExpression{reject: reject}
	if err != "" {
		f.err = errors.New(err)
	}
	return f
}

func (f fakeCompiledExpression) Evaluate(bag attribute.Bag) (interface{}, error) {
	return f.EvaluateBoolean(bag)
}

func (f fakeCompiledExpression) EvaluateBoolean(_ attribute.Bag) (bool, error) {
	return !f.reject, f.err
}

func assertResolverError(t *testing.T, got error, want string) {
	if want == "" && got != nil {
		t.Fatalf("unexpected error %v", got)
	}

	if want != "" && got == nil {
		t.Fatalf("expected error %s, got success", want)
	}

	if want == "" && got == nil {
		return
	}
	// check for substring
	if !strings.Contains(got.Error(), want) {
		t.Fatalf("got <%s>\nwant <%s>", got.Error(), want)
	}
}

func newFakeRule(vr adptTmpl.TemplateVariety, length int, selectReject bool, selectError string) *Rule {
	return &Rule{
		originalMatchString: "request.size=2000",
		expression:          fakeCompiledExpr(selectReject, selectError),
		actions: map[adptTmpl.TemplateVariety][]*Action{
			vr: make([]*Action, length),
		},
	}
}

type fakeRuleCfg struct {
	ns         string
	ruleLength int
}

func newRules(vr adptTmpl.TemplateVariety, frule []fakeRuleCfg, selectReject bool, selectError string) map[string][]*Rule {
	rules := map[string][]*Rule{}
	for _, fr := range frule {
		rules[fr.ns] = append(rules[fr.ns], newFakeRule(vr, fr.ruleLength, selectReject, selectError))
	}
	return rules
}

var _ = flag.Lookup("v").Value.Set("99")
