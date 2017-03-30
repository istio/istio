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

package expr

import (
	"fmt"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
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
		{`origin.host == "9.0.10.1"`, `EQ($origin.host, "9.0.10.1")`},
		{`service.name == "cluster1.ns.*"`, `EQ($service.name, "cluster1.ns.*")`},
		{`a() == 200`, `EQ(a(), 200)`},
		{`true == false`, `EQ(true, false)`},
		{`a.b == 3.14`, `EQ($a.b, 3.14)`},
		{`a/b`, `QUO($a, $b)`},
		{`request.header["X-FORWARDED-HOST"] == "aaa"`, `EQ(INDEX($request.header, "X-FORWARDED-HOST"), "aaa")`},
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
		{"a = bc", `parse error`},
		{`3 = 10`, "parse error"},
		{`a.b.c() == 20`, "unexpected expression"},
		{`a.b().c() == 20`, "unexpected expression"},
		{`(a.c).d == 300`, `unexpected expression`},
		{`substring(*a, 20) == 12`, `unexpected expression`},
		{`(*a == 20) && 12`, `unexpected expression`},
		{`!*a`, `unexpected expression`},
		{`request.headers[*a] == 200`, `unexpected expression`},
		{`atr == 'aaa'`, "parse error"},
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
	v map[string]*dpb.AttributeDescriptor
}

func (a *af) GetAttribute(name string) *dpb.AttributeDescriptor { return a.v[name] }

func newAF(ds []*ad) *af {
	m := make(map[string]*dpb.AttributeDescriptor)
	for _, aa := range ds {
		m[aa.name] = &dpb.AttributeDescriptor{Name: aa.name, ValueType: aa.v}
	}
	return &af{m}
}

func TestTypeCheck(t *testing.T) {
	af := newAF([]*ad{
		{"int", dpb.INT64},
		{"bool", dpb.BOOL},
		{"double", dpb.DOUBLE},
		{"string", dpb.STRING},
		{"timestamp", dpb.TIMESTAMP},
		{"ip", dpb.IP_ADDRESS},
		{"email", dpb.EMAIL_ADDRESS},
		{"uri", dpb.URI},
		{"dns", dpb.DNS_NAME},
		{"duration", dpb.DURATION},
		{"stringmap", dpb.STRING_MAP},
	})

	tests := []struct {
		in  string
		out dpb.ValueType
		err string
	}{
		// identity
		{"int", dpb.INT64, ""},
		{"bool", dpb.BOOL, ""},
		{"double", dpb.DOUBLE, ""},
		{"string", dpb.STRING, ""},
		{"timestamp", dpb.TIMESTAMP, ""},
		{"ip", dpb.IP_ADDRESS, ""},
		{"email", dpb.EMAIL_ADDRESS, ""},
		{"uri", dpb.URI, ""},
		{"dns", dpb.DNS_NAME, ""},
		{"duration", dpb.DURATION, ""},
		{"stringmap", dpb.STRING_MAP, ""},
		// expressions
		{"int == 2", dpb.BOOL, ""},
		{"double == 2.0", dpb.BOOL, ""},
		{`string | "foobar"`, dpb.STRING, ""},
		// invalid expressions
		{"int | bool", dpb.VALUE_TYPE_UNSPECIFIED, "typeError"},
		{"stringmap | ", dpb.VALUE_TYPE_UNSPECIFIED, "failed to parse"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.in), func(t *testing.T) {
			vt, err := NewCEXLEvaluator().TypeCheck(tt.in, af)
			if tt.err != "" || err != nil {
				if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("TypeCheck(%s, adf) = %v, wanted err %v", tt.in, err, tt.err)
				}
			}
			if vt != tt.out {
				t.Fatalf("TypeCheck(%s, adf) = %v, wanted type %v", tt.in, vt, tt.out)
			}
		})
	}
}

func TestAssertType(t *testing.T) {
	af := newAF([]*ad{
		{"int64", dpb.INT64},
		{"string", dpb.STRING},
		{"duration", dpb.DURATION},
	})

	tests := []struct {
		name     string
		expr     string
		expected dpb.ValueType
		err      string
	}{
		{"correct type", "string", dpb.STRING, ""},
		{"wrong type", "int64", dpb.STRING, "expected type STRING"},
		{"eval error", "duration |", dpb.DURATION, "failed to parse"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := NewCEXLEvaluator().AssertType(tt.expr, af, tt.expected); tt.err != "" || err != nil {
				if tt.err == "" {
					t.Fatalf("AssertType(%s, af, %v) = %v, wanted no err", tt.expr, tt.expected, err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("AssertType(%s, af, %v) = %v, wanted err %v", tt.expr, tt.expected, err, tt.err)
				}
			}
		})
	}
}

func TestInternalTypeCheck(t *testing.T) {
	success := "__SUCCESS__"
	tests := []struct {
		s       string
		retType dpb.ValueType
		ds      []*ad
		err     string
	}{
		{"a == 2", dpb.BOOL, []*ad{{"a", dpb.INT64}}, success},
		{"a == 2", dpb.BOOL, []*ad{{"a", dpb.BOOL}}, "typeError"},
		{"a == 2 || a == 5", dpb.BOOL, []*ad{{"a", dpb.INT64}}, success},
		{"a | b | 5", dpb.INT64, []*ad{{"a", dpb.INT64}, {"b", dpb.INT64}}, success},
		{`a | b | "5"`, dpb.INT64, []*ad{{"a", dpb.INT64}, {"b", dpb.INT64}}, "typeError"},
		{`a["5"] == "abc"`, dpb.BOOL, []*ad{{"a", dpb.STRING_MAP}, {"b", dpb.INT64}}, success},
		{`a["5"] == "abc"`, dpb.BOOL, []*ad{{"a", dpb.STRING}, {"b", dpb.INT64}}, "typeError"},
		{`a | b | "abc"`, dpb.STRING, []*ad{{"a", dpb.STRING}, {"b", dpb.STRING}}, success},
		{`x | y | "abc"`, dpb.STRING, []*ad{{"a", dpb.STRING}, {"b", dpb.STRING}}, "unresolved attribute"},
		{`EQ("abc")`, dpb.BOOL, []*ad{{"a", dpb.STRING}, {"b", dpb.STRING}}, "arity mismatch"},
		{`a % 5`, dpb.BOOL, []*ad{{"a", dpb.INT64}}, "unknown function"},
	}
	fMap := FuncMap()
	for idx, c := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, c.s), func(t *testing.T) {
			var ex *Expression
			var err error
			var retType dpb.ValueType
			if ex, err = Parse(c.s); err != nil {
				t.Fatalf("unexpected error %s", err)
			}

			retType, err = ex.TypeCheck(newAF(c.ds), fMap)
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
