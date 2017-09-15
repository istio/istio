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

package crd

import "testing"

var (
	camelKabobs = []struct{ in, out string }{
		{"ExampleNameX", "example-name-x"},
		{"Example1", "example1"},
		{"ExampleXY", "example-x-y"},
	}
)

func TestCamelKabob(t *testing.T) {
	for _, tt := range camelKabobs {
		s := CamelCaseToKabobCase(tt.in)
		if s != tt.out {
			t.Errorf("CamelCaseToKabobCase(%q) => %q, want %q", tt.in, s, tt.out)
		}
		u := KabobCaseToCamelCase(tt.out)
		if u != tt.in {
			t.Errorf("kabobToCamel(%q) => %q, want %q", tt.out, u, tt.in)
		}
	}
}
