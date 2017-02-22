// Copyright 2017 Google Inc.
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

	config "istio.io/api/mixer/v1/config/descriptor"
	dpb "istio.io/api/mixer/v1/config/descriptor"
)

func TestGoodParse(tst *testing.T) {
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
		tst.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
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

func TestNewConstant(tst *testing.T) {
	tests := []struct {
		v        string
		vType    config.ValueType
		typedVal interface{}
		err      string
	}{
		{"3.75", config.DOUBLE, float64(3.75), "SUCCESS"},
		{"not a double", config.DOUBLE, float64(3.75), "invalid syntax"},
		{"1001", config.INT64, int64(1001), "SUCCESS"},
		{"not an int64", config.INT64, int64(1001), "invalid syntax"},
		{`"back quoted"`, config.STRING, "back quoted", "SUCCESS"},
		{`'aaa'`, config.STRING, "aaa", "invalid syntax"},
	}
	for idx, tt := range tests {
		tst.Run(fmt.Sprintf("[%d] %s", idx, tt.v), func(t *testing.T) {
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

func TestBadParse(tst *testing.T) {
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
		tst.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			_, err := Parse(tt.src)
			if err == nil {
				t.Errorf("[%d] got: <nil>\nwant: %s", idx, tt.err)
				return
			}

			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("[%d] got: %s\nwant: %s", idx, err.Error(), tt.err)
			}
		})
	}

	// nil test
	tex := &Expression{}
	if tex.String() != "<nil>" {
		tst.Errorf("got: %s\nwant: <nil>", tex.String())
	}
}

type ad struct {
	name string
	v    config.ValueType
}
type af struct {
	v map[string]*dpb.AttributeDescriptor
}

func (a *af) FindAttributeDescriptor(name string) *dpb.AttributeDescriptor { return a.v[name] }

func newAF(ds []*ad) *af {
	m := make(map[string]*dpb.AttributeDescriptor)
	for _, aa := range ds {
		m[aa.name] = &dpb.AttributeDescriptor{Name: aa.name, ValueType: aa.v}
	}
	return &af{m}
}

func TestTypeCheck(tt *testing.T) {
	success := "__SUCCESS__"
	tests := []struct {
		s       string
		retType config.ValueType
		ds      []*ad
		err     string
	}{
		{"a == 2", config.BOOL, []*ad{{"a", config.INT64}}, success},
		{"a == 2", config.BOOL, []*ad{{"a", config.BOOL}}, "typeError"},
		{"a == 2 || a == 5", config.BOOL, []*ad{{"a", config.INT64}}, success},
		{"a | b | 5", config.INT64, []*ad{{"a", config.INT64}, {"b", config.INT64}}, success},
		{`a | b | "5"`, config.INT64, []*ad{{"a", config.INT64}, {"b", config.INT64}}, "typeError"},
		{`a["5"] == "abc"`, config.BOOL, []*ad{{"a", config.STRING_MAP}, {"b", config.INT64}}, success},
		{`a["5"] == "abc"`, config.BOOL, []*ad{{"a", config.STRING}, {"b", config.INT64}}, "typeError"},
		{`a | b | "abc"`, config.STRING, []*ad{{"a", config.STRING}, {"b", config.STRING}}, success},
		{`x | y | "abc"`, config.STRING, []*ad{{"a", config.STRING}, {"b", config.STRING}}, "unresolved attribute"},
		{`EQ("abc")`, config.BOOL, []*ad{{"a", config.STRING}, {"b", config.STRING}}, "arity mismatch"},
		{`a % 5`, config.BOOL, []*ad{{"a", config.INT64}}, "unknown function"},
	}
	fMap := FuncMap()
	for idx, c := range tests {
		tt.Run(fmt.Sprintf("[%d] %s", idx, c.s), func(t *testing.T) {
			var ex *Expression
			var err error
			var retType config.ValueType
			if ex, err = Parse(c.s); err != nil {
				t.Errorf("unexpected error %s", err)
			}

			retType, err = ex.TypeCheck(newAF(c.ds), fMap)
			if err != nil {
				if !strings.Contains(err.Error(), c.err) {
					t.Errorf("unexpected error got %s want %s", err.Error(), c.err)
				}
				return
			}

			if c.err != success {
				t.Errorf("got err==nil want %s", c.err)
				return
			}

			if retType != c.retType {
				t.Errorf("incorrect return type got %s want %s", retType, c.retType)
			}

		})
	}

}
