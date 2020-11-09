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

package host_test

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/config/host"
)

func TestNameMatches(t *testing.T) {
	tests := []struct {
		name string
		a, b host.Name
		out  bool
	}{
		{"empty", "", "", true},
		{"first empty", "", "foo.com", false},
		{"second empty", "foo.com", "", false},

		{"non-wildcard domain",
			"foo.com", "foo.com", true},
		{"non-wildcard domain",
			"bar.com", "foo.com", false},
		{"non-wildcard domain - order doesn't matter",
			"foo.com", "bar.com", false},

		{"domain does not match subdomain",
			"bar.foo.com", "foo.com", false},
		{"domain does not match subdomain - order doesn't matter",
			"foo.com", "bar.foo.com", false},

		{"wildcard matches subdomains",
			"*.com", "foo.com", true},
		{"wildcard matches subdomains",
			"*.com", "bar.com", true},
		{"wildcard matches subdomains",
			"*.foo.com", "bar.foo.com", true},

		{"wildcard matches anything", "*", "foo.com", true},
		{"wildcard matches anything", "*", "*.com", true},
		{"wildcard matches anything", "*", "com", true},
		{"wildcard matches anything", "*", "*", true},
		{"wildcard matches anything", "*", "", true},

		{"wildcarded domain matches wildcarded subdomain", "*.com", "*.foo.com", true},
		{"wildcarded sub-domain does not match domain", "foo.com", "*.foo.com", false},
		{"wildcarded sub-domain does not match domain - order doesn't matter", "*.foo.com", "foo.com", false},

		{"long wildcard does not match short host", "*.foo.bar.baz", "baz", false},
		{"long wildcard does not match short host - order doesn't matter", "baz", "*.foo.bar.baz", false},
		{"long wildcard matches short wildcard", "*.foo.bar.baz", "*.baz", true},
		{"long name matches short wildcard", "foo.bar.baz", "*.baz", true},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if tt.out != tt.a.Matches(tt.b) {
				t.Fatalf("%q.Matches(%q) = %t wanted %t", tt.a, tt.b, !tt.out, tt.out)
			}
		})
	}
}

func TestNameSubsetOf(t *testing.T) {
	tests := []struct {
		name string
		a, b host.Name
		out  bool
	}{
		{"empty", "", "", true},
		{"first empty", "", "foo.com", false},
		{"second empty", "foo.com", "", false},

		{"non-wildcard domain",
			"foo.com", "foo.com", true},
		{"non-wildcard domain",
			"bar.com", "foo.com", false},
		{"non-wildcard domain - order doesn't matter",
			"foo.com", "bar.com", false},

		{"domain does not match subdomain",
			"bar.foo.com", "foo.com", false},
		{"domain does not match subdomain - order doesn't matter",
			"foo.com", "bar.foo.com", false},

		{"wildcard matches subdomains",
			"foo.com", "*.com", true},
		{"wildcard matches subdomains",
			"bar.com", "*.com", true},
		{"wildcard matches subdomains",
			"bar.foo.com", "*.foo.com", true},

		{"wildcard matches anything", "foo.com", "*", true},
		{"wildcard matches anything", "*.com", "*", true},
		{"wildcard matches anything", "com", "*", true},
		{"wildcard matches anything", "*", "*", true},
		{"wildcard matches anything", "", "*", true},

		{"wildcarded domain matches wildcarded subdomain", "*.foo.com", "*.com", true},
		{"wildcarded sub-domain does not match domain", "*.foo.com", "foo.com", false},

		{"long wildcard does not match short host", "*.foo.bar.baz", "baz", false},
		{"long name matches short wildcard", "foo.bar.baz", "*.baz", true},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if tt.out != tt.a.SubsetOf(tt.b) {
				t.Fatalf("%q.SubsetOf(%q) = %t wanted %t", tt.a, tt.b, !tt.out, tt.out)
			}
		})
	}
}

func BenchmarkNameMatch(b *testing.B) {
	tests := []struct {
		a, z    host.Name
		matches bool
	}{
		{"foo.com", "foo.com", true},
		{"*.com", "foo.com", true},
		{"*.foo.com", "bar.foo.com", true},
		{"*", "foo.com", true},
		{"*", "*.com", true},
		{"*", "", true},
		{"*.com", "*.foo.com", true},
		{"foo.com", "*.foo.com", false},
		{"*.foo.bar.baz", "baz", false},
	}
	for n := 0; n < b.N; n++ {
		for _, test := range tests {
			doesMatch := test.a.Matches(test.z)
			if doesMatch != test.matches {
				b.Fatalf("does not match")
			}
		}
	}
}
