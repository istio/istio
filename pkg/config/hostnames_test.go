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
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestHostnamesIntersection(t *testing.T) {
	tests := []struct {
		a, b, intersection Hostnames
	}{
		{
			Hostnames{"foo,com"},
			Hostnames{"bar.com"},
			Hostnames{},
		},
		{
			Hostnames{"foo.com", "bar.com"},
			Hostnames{"bar.com"},
			Hostnames{"bar.com"},
		},
		{
			Hostnames{"foo.com", "bar.com"},
			Hostnames{"*.com"},
			Hostnames{"foo.com", "bar.com"},
		},
		{
			Hostnames{"*.com"},
			Hostnames{"foo.com", "bar.com"},
			Hostnames{"foo.com", "bar.com"},
		},
		{
			Hostnames{"foo.com", "*.net"},
			Hostnames{"*.com", "bar.net"},
			Hostnames{"foo.com", "bar.net"},
		},
		{
			Hostnames{"foo.com", "*.net"},
			Hostnames{"*.bar.net"},
			Hostnames{"*.bar.net"},
		},
		{
			Hostnames{"foo.com", "bar.net"},
			Hostnames{"*"},
			Hostnames{"foo.com", "bar.net"},
		},
		{
			Hostnames{"foo.com"},
			Hostnames{},
			Hostnames{},
		},
		{
			Hostnames{},
			Hostnames{"bar.com"},
			Hostnames{},
		},
		{
			Hostnames{"*", "foo.com"},
			Hostnames{"foo.com"},
			Hostnames{"foo.com"},
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

func TestHostnamesForNamespace(t *testing.T) {
	tests := []struct {
		hosts     []string
		namespace string
		want      Hostnames
	}{
		{
			[]string{"ns1/foo.com", "ns2/bar.com"},
			"ns1",
			Hostnames{"foo.com"},
		},
		{
			[]string{"ns1/foo.com", "ns2/bar.com"},
			"ns3",
			Hostnames{},
		},
		{
			[]string{"ns1/foo.com", "*/bar.com"},
			"ns1",
			Hostnames{"foo.com", "bar.com"},
		},
		{
			[]string{"ns1/foo.com", "*/bar.com"},
			"ns3",
			Hostnames{"bar.com"},
		},
		{
			[]string{"foo.com", "ns2/bar.com"},
			"ns2",
			Hostnames{"foo.com", "bar.com"},
		},
		{
			[]string{"foo.com", "ns2/bar.com"},
			"ns3",
			Hostnames{"foo.com"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			result := HostnamesForNamespace(tt.hosts, tt.namespace)
			if !reflect.DeepEqual(result, tt.want) {
				t.Fatalf("HostnamesForNamespace(%v, %v) = %v, want %v", tt.hosts, tt.namespace, result, tt.want)
			}
		})
	}
}

func TestHostnamesSortOrder(t *testing.T) {
	tests := []struct {
		in, want Hostnames
	}{
		// Prove we sort alphabetically:
		{
			Hostnames{"b", "a"},
			Hostnames{"a", "b"},
		},
		{
			Hostnames{"bb", "cc", "aa"},
			Hostnames{"aa", "bb", "cc"},
		},
		// Prove we sort longest first, alphabetically:
		{
			Hostnames{"b", "a", "aa"},
			Hostnames{"aa", "a", "b"},
		},
		{
			Hostnames{"foo.com", "bar.com", "foo.bar.com"},
			Hostnames{"foo.bar.com", "bar.com", "foo.com"},
		},
		// We sort wildcards last, always
		{
			Hostnames{"a", "*", "z"},
			Hostnames{"a", "z", "*"},
		},
		{
			Hostnames{"foo.com", "bar.com", "*.com"},
			Hostnames{"bar.com", "foo.com", "*.com"},
		},
		{
			Hostnames{"foo.com", "bar.com", "*.com", "*.foo.com", "*", "baz.bar.com"},
			Hostnames{"baz.bar.com", "bar.com", "foo.com", "*.foo.com", "*.com", "*"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			// Save a copy to report errors with
			tmp := make(Hostnames, len(tt.in))
			copy(tmp, tt.in)

			sort.Sort(tt.in)
			if !reflect.DeepEqual(tt.in, tt.want) {
				t.Fatalf("sort.Sort(%v) = %v, want %v", tmp, tt.in, tt.want)
			}
		})
	}
}

func BenchmarkSort(b *testing.B) {
	unsorted := Hostnames{"foo.com", "bar.com", "*.com", "*.foo.com", "*", "baz.bar.com"}

	for n := 0; n < b.N; n++ {
		given := make(Hostnames, len(unsorted))
		copy(given, unsorted)
		sort.Sort(given)
	}
}
