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

package ast

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	dpb "istio.io/api/policy/v1beta1"
)

func TestGoodParse(t *testing.T) {
	tests := []struct {
		src     string
		postfix string
	}{
		{"a.b.c == 2", "EQ($a.b.c, 2)"},
		{`!(a.b || b || "c a b" || ( a && b ))`, `NOT(LOR(LOR(LOR($a.b, $b), "c a b"), LAND($a, $b)))`},
		{`a || b || "c" || ( a && b )`, `LOR(LOR(LOR($a, $b), "c"), LAND($a, $b))`},
		{`substring(a, 5) == "abc"`, `EQ(substring($a, 5), "abc")`},
		{`a|b|c`, `OR(OR($a, $b), $c)`},
		{`200`, `200`},
		{`"истио"`, `"истио"`},
		{`origin.host == "9.0.10.1"`, `EQ($origin.host, "9.0.10.1")`},
		{`service.name == "cluster1.ns.*"`, `EQ($service.name, "cluster1.ns.*")`},
		{`a() == 200`, `EQ(a(), 200)`},
		{`true == false`, `EQ(true, false)`},
		{`a.b == 3.14`, `EQ($a.b, 3.14)`},
		{`a/b`, `QUO($a, $b)`},
		{`request.header["X-FORWARDED-HOST"] == "aaa"`, `EQ(INDEX($request.header, "X-FORWARDED-HOST"), "aaa")`},
		{`source.ip | ip("0.0.0.0")`, `OR($source.ip, ip("0.0.0.0"))`},
		{`context.time | timestamp("2015-01-02T15:04:05Z")`, `OR($context.time, timestamp("2015-01-02T15:04:05Z"))`},
		{`match(service.name, "cluster1.ns.*")`, `match($service.name, "cluster1.ns.*")`},
		{`a.b == 3.14 && c == "d" && r.h["abc"] == "pqr" || r.h["abc"] == "xyz"`,
			`LOR(LAND(LAND(EQ($a.b, 3.14), EQ($c, "d")), EQ(INDEX($r.h, "abc"), "pqr")), EQ(INDEX($r.h, "abc"), "xyz"))`},

		{`a()`, `a()`},
		{`a.b()`, `$a:b()`},
		{`a.b.c(q)`, `$a.b:c($q)`},
		{`a.b.c(q, z)`, `$a.b:c($q, $z)`},
		{`a.b(q.z(d()))`, `$a:b($q:z(d()))`},
		{`a.b(q.z(d())) == 42`, `EQ($a:b($q:z(d())), 42)`},
		{`a.b().c().d()`, `$a:b():c():d()`},
		{`a.b.c().d()`, `$a.b:c():d()`},
		{`a.b.c().d() == 23`, `EQ($a.b:c():d(), 23)`},
		{`"abc".matches("foo")`, `"abc":matches("foo")`},
		{`"abc".prefix(23).matches("foo")`, `"abc":prefix(23):matches("foo")`},
		{`"abc".matches("foo")`, `"abc":matches("foo")`},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			ex, err := Parse(tt.src)
			if err != nil {
				t.Error(err)
				return
			}
			if tt.postfix != ex.String() {
				t.Errorf("got %s\nwant: %s", ex.String(), tt.postfix)
			}
		})
	}
}

func TestExtractMatches(t *testing.T) {
	for _, tc := range []struct {
		desc string
		src  string
		m    map[string]interface{}
	}{
		{
			desc: "no ANDS",
			src:  `a || b || "c" || ( a && b )`,
			m:    map[string]interface{}{},
		},
		{
			desc: "EQ check with function",
			src:  `substring(a, 5) == "abc"`,
			m:    map[string]interface{}{},
		},
		{
			desc: "single EQ check",
			src:  `origin.host == "9.0.10.1"`,
			m: map[string]interface{}{
				"origin.host": "9.0.10.1",
			},
		},
		{
			desc: "top level OR --> cannot extract equality subexpressions",
			src:  `a.b == 3.14 && c == "d" && r.h["abc"] == "pqr" || r.h["abc"] == "xyz"`,
			m:    map[string]interface{}{},
		},
		{
			desc: "yoda",
			src:  `"d" == c`,
			m: map[string]interface{}{
				"c": "d",
			},
		},
		{
			desc: "only top level ANDS",
			src:  `a.b == 3.14 && "d" == c && (r.h["abc"] == "pqr" || r.h["abc"] == "xyz")`,
			m: map[string]interface{}{
				"a.b": 3.14,
				"c":   "d",
			},
		},
		{
			desc: "only top level ANDS, attribute to attribute comparison excluded",
			src:  `a.b == 3.14 && c == d && (r.h["abc"] == "pqr" || r.h["abc"] == "xyz")`,
			m: map[string]interface{}{
				"a.b": 3.14,
			},
		},
		{
			src: `c == d && (r.h["abc"] == "pqr" || r.h["abc"] == "xyz") && a.b == 3.14`,
			m: map[string]interface{}{
				"a.b": 3.14,
			},
		},
		{ // c == d is not included because it is an attribute to attribute comparison.
			src: `c == d && (r.h["abc"] == "pqr" || r.h["abc"] == "xyz") && a.b == 3.14 && context.protocol == "TCP"`,
			m: map[string]interface{}{
				"context.protocol": "TCP",
				"a.b":              3.14,
			},
		},
		{
			src: `destination.service == "mysvc.FQDN" && request.headers["x-id"] == "AAA"`,
			m: map[string]interface{}{
				"destination.service": "mysvc.FQDN",
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			m, err := ExtractEQMatches(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(m, tc.m) {
				t.Fatalf("got %v, want %v", m, tc.m)
			}
		})
	}
}

func TestNewConstant(t *testing.T) {
	tests := []struct {
		v        string
		vType    dpb.ValueType
		typedVal interface{}
		err      string
	}{
		{"3.75", dpb.DOUBLE, float64(3.75), "SUCCESS"},
		{"not a double", dpb.DOUBLE, float64(3.75), "invalid syntax"},
		{"1001", dpb.INT64, int64(1001), "SUCCESS"},
		{"not an int64", dpb.INT64, int64(1001), "invalid syntax"},
		{`"back quoted"`, dpb.STRING, "back quoted", "SUCCESS"},
		{`'aaa'`, dpb.STRING, "aaa", "invalid syntax"},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.v), func(t *testing.T) {
			c, err := newConstant(tt.v, tt.vType)
			if err != nil {
				if !strings.Contains(err.Error(), tt.err) {
					t.Errorf("[%d] got %#v, want %s", idx, err.Error(), tt.err)
				}
				return
			}
			if c.Value != tt.typedVal {
				t.Errorf("[%d] got %#v, want %s", idx, c.Value, tt.typedVal)
			}
		})
	}
}

func TestBadParse(t *testing.T) {
	tests := []struct {
		src string
		err string
	}{
		{`*a != b`, "unexpected expression"},
		{"a = bc", `unable to parse`},
		{`3 = 10`, "unable to parse"},
		{`(a.c()).d == 300`, `unexpected expression`},
		{`substring(*a, 20) == 12`, `unexpected expression`},
		{`(*a == 20) && 12`, `unexpected expression`},
		{`!*a`, `unexpected expression`},
		{`request.headers[*a] == 200`, `unexpected expression`},
		{`atr == 'aaa'`, "unable to parse"},
		{`c().e.d()`, `unexpected expression`},
		{`foo{}`, `unexpected expression`},
		{`foo{}.bar`, `unexpected expression`},
		{`foo{}.bar()`, `unexpected expression`},
		{`(foo{}).bar()`, `unexpected expression`},
		{`a().b`, `unexpected expression`},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			_, err := Parse(tt.src)
			if err == nil {
				t.Fatalf("[%d] got: <nil>\nwant: %s", idx, tt.err)
			}

			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("[%d] got: %s\nwant: %s", idx, err.Error(), tt.err)
			}
		})
	}

	// nil test
	tex := &Expression{}
	if tex.String() != "<nil>" {
		t.Errorf("got: %s\nwant: <nil>", tex.String())
	}
}

type ad struct {
	name string
	v    dpb.ValueType
}
type af struct {
	v map[string]*dpb.AttributeManifest_AttributeInfo
}

func (a *af) GetAttribute(name string) *dpb.AttributeManifest_AttributeInfo { return a.v[name] }
func (a *af) Attributes() map[string]*dpb.AttributeManifest_AttributeInfo   { return a.v }

func newAF(ds []*ad) *af {
	m := make(map[string]*dpb.AttributeManifest_AttributeInfo)
	for _, aa := range ds {
		m[aa.name] = &dpb.AttributeManifest_AttributeInfo{ValueType: aa.v}
	}
	return &af{m}
}

func TestInternalTypeCheck(t *testing.T) {
	success := "__SUCCESS__"
	tests := []struct {
		s       string
		retType dpb.ValueType
		ds      []*ad
		fns     []FunctionMetadata
		err     string
	}{
		{"a == 2", dpb.BOOL, []*ad{{"a", dpb.INT64}}, nil, success},
		{"a == 2", dpb.BOOL, []*ad{{"a", dpb.BOOL}}, nil, "typeError"},
		{"a == 2 || a == 5", dpb.BOOL, []*ad{{"a", dpb.INT64}}, nil, success},
		{"a | b | 5", dpb.INT64, []*ad{{"a", dpb.INT64}, {"b", dpb.INT64}}, nil, success},
		{`a | b | "5"`, dpb.INT64, []*ad{{"a", dpb.INT64}, {"b", dpb.INT64}}, nil, "typeError"},
		{`a["5"] == "abc"`, dpb.BOOL, []*ad{{"a", dpb.STRING_MAP}, {"b", dpb.INT64}}, nil, success},
		{`a["5"] == "abc"`, dpb.BOOL, []*ad{{"a", dpb.STRING}, {"b", dpb.INT64}}, nil, "typeError"},
		{`a | b | "abc"`, dpb.STRING, []*ad{{"a", dpb.STRING}, {"b", dpb.STRING}}, nil, success},
		{`x | y | "abc"`, dpb.STRING, []*ad{{"a", dpb.STRING}, {"b", dpb.STRING}}, nil, "unknown attribute"},
		{`EQ("abc")`, dpb.BOOL, []*ad{{"a", dpb.STRING}, {"b", dpb.STRING}}, nil, "arity mismatch"},
		{`a % 5`, dpb.BOOL, []*ad{{"a", dpb.INT64}}, nil, "unknown function"},

		{`fn1()`, dpb.BOOL, []*ad{{}}, []FunctionMetadata{
			{Name: "fn1", Instance: false, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{}},
		}, success},
		{`fn2(true)`, dpb.BOOL, []*ad{{}}, []FunctionMetadata{
			{Name: "fn2", Instance: false, TargetType: dpb.STRING, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
				dpb.BOOL,
			}},
		}, success},
		{`fn3(true)`, dpb.BOOL, []*ad{{}}, []FunctionMetadata{
			{Name: "fn3", Instance: true, TargetType: dpb.STRING, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
				dpb.BOOL,
			}},
		}, "invoking instance method without an instance"},
		{`a.fn4(true)`, dpb.BOOL, []*ad{{}}, []FunctionMetadata{
			{Name: "fn4", Instance: false, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
				dpb.BOOL,
			}},
		}, "invoking regular function on instance method"},
		{`a.fn5()`, dpb.BOOL, []*ad{{}}, []FunctionMetadata{
			{Name: "fn5", Instance: true, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{}},
		}, "unknown attribute"},
		{`pattern.matches(str)`, dpb.BOOL,
			[]*ad{
				{"pattern", dpb.STRING},
				{"str", dpb.STRING},
			},
			[]FunctionMetadata{
				{Name: "matches", Instance: true, TargetType: dpb.STRING, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
					dpb.STRING,
				}},
			},
			success},
		{`text.prefix(3).startswith(str)`, dpb.BOOL,
			[]*ad{
				{"text", dpb.STRING},
				{"str", dpb.STRING},
			},
			[]FunctionMetadata{
				{Name: "prefix", Instance: true, TargetType: dpb.STRING, ReturnType: dpb.STRING, ArgumentTypes: []dpb.ValueType{
					dpb.INT64,
				}},
				{Name: "startswith", Instance: true, TargetType: dpb.STRING, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
					dpb.STRING,
				}},
			},
			success},
		{`no.matches("a")`, dpb.BOOL,
			[]*ad{
				{"no", dpb.INT64},
			},
			[]FunctionMetadata{
				{Name: "matches", Instance: true, TargetType: dpb.STRING, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
					dpb.STRING,
				}},
			},
			`$no:matches("a") target typeError got INT64`},
		{`"abc".genericEquals("cba")`, dpb.BOOL, []*ad{},
			[]FunctionMetadata{
				{Name: "genericEquals", Instance: true, TargetType: dpb.VALUE_TYPE_UNSPECIFIED, ReturnType: dpb.BOOL, ArgumentTypes: []dpb.ValueType{
					dpb.VALUE_TYPE_UNSPECIFIED,
				}},
			},
			success},
		{`conditional(true, "abc", "cba")`, dpb.STRING, []*ad{},
			[]FunctionMetadata{
				{Name: "conditional", Instance: false, ReturnType: dpb.VALUE_TYPE_UNSPECIFIED, ArgumentTypes: []dpb.ValueType{
					dpb.BOOL, dpb.VALUE_TYPE_UNSPECIFIED, dpb.VALUE_TYPE_UNSPECIFIED,
				}},
			},
			success},
		{`conditional(false, 23, 42)`, dpb.INT64, []*ad{},
			[]FunctionMetadata{
				{Name: "conditional", Instance: false, TargetType: dpb.INT64, ReturnType: dpb.VALUE_TYPE_UNSPECIFIED, ArgumentTypes: []dpb.ValueType{
					dpb.BOOL, dpb.VALUE_TYPE_UNSPECIFIED, dpb.VALUE_TYPE_UNSPECIFIED,
				}},
			},
			success},
	}
	for idx, c := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, c.s), func(t *testing.T) {
			fns := c.fns
			if fns == nil {
				fns = []FunctionMetadata{}
			}
			fMap := FuncMap(fns)
			var ex *Expression
			var err error
			var retType dpb.ValueType
			if ex, err = Parse(c.s); err != nil {
				t.Fatalf("unexpected error %s", err)
			}

			retType, err = ex.EvalType(newAF(c.ds), fMap)
			if err != nil {
				if !strings.Contains(err.Error(), c.err) {
					t.Errorf("unexpected error got %s want %s", err.Error(), c.err)
				}
				return
			}

			if c.err != success {
				t.Fatalf("got err==nil want %s", c.err)
			}

			if retType != c.retType {
				t.Fatalf("incorrect return type got %s want %s", retType, c.retType)
			}

		})
	}
}
