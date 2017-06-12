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

package tpr

import "testing"

var (
	camelKabobs = []struct{ in, out string }{
		{"ExampleNameX", "example-name-x"},
		{"example1", "example1"},
		{"exampleXY", "example-x-y"},
	}
)

func TestCamelKabob(t *testing.T) {
	for _, tt := range camelKabobs {
		s := camelCaseToKabobCase(tt.in)
		if s != tt.out {
			t.Errorf("camelCaseToKabobCase(%q) => %q, want %q", tt.in, s, tt.out)
		}
	}
}

func TestDecodeIngressRuleName(t *testing.T) {
	cases := []struct {
		ingressName      string
		ingressNamespace string
		ruleNum          int
		pathNum          int
	}{
		{"myingress", "test", 0, 0},
		{"myingress", "default", 1, 2},
		{"my-ingress", "test-namespace", 1, 2},
		{"my-cool-ingress", "new-space", 1, 2},
	}

	for _, c := range cases {
		encoded := encodeIngressRuleName(c.ingressName, c.ingressNamespace, c.ruleNum, c.pathNum)
		ingressName, ingressNamespace, ruleNum, pathNum, err := decodeIngressRuleName(encoded)
		if err != nil {
			t.Errorf("decodeIngressRuleName(%q) => error %v", encoded, err)
		}
		if ingressName != c.ingressName || ingressNamespace != c.ingressNamespace ||
			ruleNum != c.ruleNum || pathNum != c.pathNum {
			t.Errorf("decodeIngressRuleName(%q) => (%q, %q, %d, %d), want (%q, %q, %d, %d)",
				encoded,
				ingressName, ingressNamespace, ruleNum, pathNum,
				c.ingressName, c.ingressNamespace, c.ruleNum, c.pathNum,
			)
		}
	}
}

func TestIsRegularExpression(t *testing.T) {
	cases := []struct {
		s       string
		isRegex bool
	}{
		{"/api/v1/", false},
		{"/api/v1/.*", true},
		{"/api/.*/resource", true},
		{"/api/v[1-9]/resource", true},
		{"/api/.*/.*", true},
	}

	for _, c := range cases {
		if isRegularExpression(c.s) != c.isRegex {
			t.Errorf("isRegularExpression(%q) => %v, want %v", c.s, !c.isRegex, c.isRegex)
		}
	}
}
