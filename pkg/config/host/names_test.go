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
	"reflect"
	"sort"
	"testing"

	"istio.io/istio/pkg/config/host"
)

func TestNamesIntersection(t *testing.T) {
	tests := []struct {
		a, b, intersection host.Names
	}{
		{
			host.Names{"foo,com"},
			host.Names{"bar.com"},
			host.Names{},
		},
		{
			host.Names{"foo.com", "bar.com"},
			host.Names{"bar.com"},
			host.Names{"bar.com"},
		},
		{
			host.Names{"foo.com", "bar.com"},
			host.Names{"*.com"},
			host.Names{"foo.com", "bar.com"},
		},
		{
			host.Names{"*.com"},
			host.Names{"foo.com", "bar.com"},
			host.Names{"foo.com", "bar.com"},
		},
		{
			host.Names{"foo.com", "*.net"},
			host.Names{"*.com", "bar.net"},
			host.Names{"foo.com", "bar.net"},
		},
		{
			host.Names{"foo.com", "*.net"},
			host.Names{"*.bar.net"},
			host.Names{"*.bar.net"},
		},
		{
			host.Names{"foo.com", "bar.net"},
			host.Names{"*"},
			host.Names{"foo.com", "bar.net"},
		},
		{
			host.Names{"foo.com"},
			host.Names{},
			host.Names{},
		},
		{
			host.Names{},
			host.Names{"bar.com"},
			host.Names{},
		},
		{
			host.Names{"*", "foo.com"},
			host.Names{"foo.com"},
			host.Names{"foo.com"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			result := tt.a.Intersection(tt.b)
			if !reflect.DeepEqual(result, tt.intersection) {
				t.Fatalf("%v.Intersection(%v) = %v, want %v", tt.a, tt.b, result, tt.intersection)
			}
		})
	}
}

func TestNamesForNamespace(t *testing.T) {
	tests := []struct {
		hosts     []string
		namespace string
		want      host.Names
	}{
		{
			[]string{"ns1/foo.com", "ns2/bar.com"},
			"ns1",
			host.Names{"foo.com"},
		},
		{
			[]string{"ns1/foo.com", "ns2/bar.com"},
			"ns3",
			host.Names{},
		},
		{
			[]string{"ns1/foo.com", "*/bar.com"},
			"ns1",
			host.Names{"foo.com", "bar.com"},
		},
		{
			[]string{"ns1/foo.com", "*/bar.com"},
			"ns3",
			host.Names{"bar.com"},
		},
		{
			[]string{"foo.com", "ns2/bar.com"},
			"ns2",
			host.Names{"foo.com", "bar.com"},
		},
		{
			[]string{"foo.com", "ns2/bar.com"},
			"ns3",
			host.Names{"foo.com"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			result := host.NamesForNamespace(tt.hosts, tt.namespace)
			if !reflect.DeepEqual(result, tt.want) {
				t.Fatalf("host.NamesForNamespace(%v, %v) = %v, want %v", tt.hosts, tt.namespace, result, tt.want)
			}
		})
	}
}

func TestNamesSortOrder(t *testing.T) {
	tests := []struct {
		in, want host.Names
	}{
		// Prove we sort alphabetically:
		{
			host.Names{"b", "a"},
			host.Names{"a", "b"},
		},
		{
			host.Names{"bb", "cc", "aa"},
			host.Names{"aa", "bb", "cc"},
		},
		// Prove we sort longest first, alphabetically:
		{
			host.Names{"b", "a", "aa"},
			host.Names{"aa", "a", "b"},
		},
		{
			host.Names{"foo.com", "bar.com", "foo.bar.com"},
			host.Names{"foo.bar.com", "bar.com", "foo.com"},
		},
		// We sort wildcards last, always
		{
			host.Names{"a", "*", "z"},
			host.Names{"a", "z", "*"},
		},
		{
			host.Names{"foo.com", "bar.com", "*.com"},
			host.Names{"bar.com", "foo.com", "*.com"},
		},
		{
			host.Names{"foo.com", "bar.com", "*.com", "*.foo.com", "*", "baz.bar.com"},
			host.Names{"baz.bar.com", "bar.com", "foo.com", "*.foo.com", "*.com", "*"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			// Save a copy to report errors with
			tmp := make(host.Names, len(tt.in))
			copy(tmp, tt.in)

			sort.Sort(tt.in)
			if !reflect.DeepEqual(tt.in, tt.want) {
				t.Fatalf("sort.Sort(%v) = %v, want %v", tmp, tt.in, tt.want)
			}
		})
	}
}

func BenchmarkNamesSort(b *testing.B) {
	unsorted := host.Names{"foo.com", "bar.com", "*.com", "*.foo.com", "*", "baz.bar.com"}

	for n := 0; n < b.N; n++ {
		given := make(host.Names, len(unsorted))
		copy(given, unsorted)
		sort.Sort(given)
	}
}
