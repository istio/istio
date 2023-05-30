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

package slices

import (
	"reflect"
	"testing"
)

func TestFindFunc(t *testing.T) {
	emptyElement := []string{}
	elements := []string{"a", "b", "c"}
	tests := []struct {
		name     string
		elements []string
		fn       func(string) bool
		want     *string
	}{
		{
			elements: emptyElement,
			fn: func(s string) bool {
				return s == "b"
			},
			want: nil,
		},
		{
			elements: elements,
			fn: func(s string) bool {
				return s == "bb"
			},
			want: nil,
		},
		{
			elements: elements,
			fn: func(s string) bool {
				return s == "b"
			},
			want: &elements[1],
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FindFunc(tt.elements, tt.fn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindFunc got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	tests := []struct {
		name     string
		elements []string
		fn       func(string) bool
		want     []string
	}{
		{
			name:     "empty element",
			elements: []string{},
			fn: func(s string) bool {
				return len(s) > 1
			},
			want: []string{},
		},
		{
			name:     "element length equals 0",
			elements: []string{"", "", ""},
			fn: func(s string) bool {
				return len(s) > 1
			},
			want: []string{},
		},
		{
			name:     "filter elements with length greater than 1",
			elements: []string{"a", "bbb", "ccc", ""},
			fn: func(s string) bool {
				return len(s) > 1
			},
			want: []string{"bbb", "ccc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := Filter(tt.elements, tt.fn)
			if !reflect.DeepEqual(filter, tt.want) {
				t.Errorf("Filter got %v, want %v", filter, tt.want)
			}
			filterInPlace := FilterInPlace(tt.elements, tt.fn)
			if !reflect.DeepEqual(filterInPlace, tt.want) {
				t.Errorf("FilterInPlace got %v, want %v", filterInPlace, tt.want)
			}
			if !reflect.DeepEqual(filter, filterInPlace) {
				t.Errorf("Filter got %v, FilterInPlace got %v", filter, filterInPlace)
			}
		})
	}
}

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		elements []int
		fn       func(int) int
		want     []int
	}{
		{
			name:     "empty element",
			elements: []int{},
			fn: func(s int) int {
				return s + 10
			},
			want: []int{},
		},
		{
			name:     "add ten to each element",
			elements: []int{0, 1, 2, 3},
			fn: func(s int) int {
				return s + 10
			},
			want: []int{10, 11, 12, 13},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Map(tt.elements, tt.fn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Map got %v, want %v", got, tt.want)
			}
		})
	}
}
