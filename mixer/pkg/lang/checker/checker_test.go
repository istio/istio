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

package checker

import (
	"fmt"
	"strings"
	"testing"

	cfgpb "istio.io/api/policy/v1beta1"
	dpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/ast"
)

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
			ev := NewTypeChecker(af)
			vt, err := ev.EvalType(tt.in)
			if tt.err != "" || err != nil {
				if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("EvalType(%s, adf) = %v, wanted err %v", tt.in, err, tt.err)
				}
			}
			if vt != tt.out {
				t.Fatalf("EvalType(%s, adf) = %v, wanted type %v", tt.in, vt, tt.out)
			}
		})
	}
}

type ad struct {
	name string
	v    dpb.ValueType
}

func newAF(ds []*ad) ast.AttributeDescriptorFinder {
	m := make(map[string]*cfgpb.AttributeManifest_AttributeInfo)
	for _, aa := range ds {
		m[aa.name] = &cfgpb.AttributeManifest_AttributeInfo{ValueType: aa.v}
	}
	return ast.NewFinder(m)
}
