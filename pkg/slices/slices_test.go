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
	"cmp"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tests/util/leak"
)

type s struct {
	Junk string
}

func TestDelete(t *testing.T) {
	var input []*s
	var output []*s
	t.Run("inner", func(t *testing.T) {
		a := &s{"a"}
		b := &s{"b"}
		// Check that we can garbage collect elements when we delete them.
		leak.MustGarbageCollect(t, b)
		input = []*s{a, b}
		output = Delete(input, 1)
	})
	assert.Equal(t, output, []*s{{"a"}})
	assert.Equal(t, input, []*s{{"a"}, nil})
}

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

func TestFilterInPlace(t *testing.T) {
	var input []*s
	var output []*s
	a := &s{"a"}
	b := &s{"b"}
	c := &s{"c"}
	input = []*s{a, b, c}

	t.Run("delete first element a", func(t *testing.T) {
		// Check that we can garbage collect elements when we delete them.
		leak.MustGarbageCollect(t, a)
		output = FilterInPlace(input, func(s *s) bool {
			return s != nil && s.Junk != "a"
		})
	})
	assert.Equal(t, output, []*s{{"b"}, {"c"}})
	assert.Equal(t, input, []*s{{"b"}, {"c"}, nil})

	t.Run("delete end element c", func(t *testing.T) {
		// Check that we can garbage collect elements when we delete them.
		leak.MustGarbageCollect(t, c)
		output = FilterInPlace(input, func(s *s) bool {
			return s != nil && s.Junk != "c"
		})
	})
	assert.Equal(t, output, []*s{{"b"}})
	assert.Equal(t, input, []*s{{"b"}, nil, nil})
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

var (
	i1, i2, i3 = 1, 2, 3
	s1, s2, s3 = "a", "b", "c"
)

func TestReference(t *testing.T) {
	type args[E any] struct {
		s []E
	}
	type testCase[E any] struct {
		name string
		args args[E]
		want []*E
	}
	stringTests := []testCase[string]{
		{
			name: "empty slice",
			args: args[string]{
				[]string{},
			},
			want: []*string{},
		},
		{
			name: "slice with 1 element",
			args: args[string]{
				[]string{s1},
			},
			want: []*string{&s1},
		},
		{
			name: "slice with many elements",
			args: args[string]{
				[]string{s1, s2, s3},
			},
			want: []*string{&s1, &s2, &s3},
		},
	}
	intTests := []testCase[int]{
		{
			name: "empty slice",
			args: args[int]{
				[]int{},
			},
			want: []*int{},
		},
		{
			name: "slice with 1 element",
			args: args[int]{
				[]int{i1},
			},
			want: []*int{&i1},
		},
		{
			name: "slice with many elements",
			args: args[int]{
				[]int{i1, i2, i3},
			},
			want: []*int{&i1, &i2, &i3},
		},
	}
	for _, tt := range stringTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Reference(tt.args.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Reference() = %v, want %v", got, tt.want)
			}
		})
	}
	for _, tt := range intTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Reference(tt.args.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Reference() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDereference(t *testing.T) {
	type args[E any] struct {
		s []*E
	}
	type testCase[E any] struct {
		name string
		args args[E]
		want []E
	}
	stringTests := []testCase[string]{
		{
			name: "empty slice",
			args: args[string]{
				[]*string{},
			},
			want: []string{},
		},
		{
			name: "slice with 1 element",
			args: args[string]{
				[]*string{&s1},
			},
			want: []string{s1},
		},
		{
			name: "slice with many elements",
			args: args[string]{
				[]*string{&s1, &s2, &s3},
			},
			want: []string{s1, s2, s3},
		},
	}
	intTests := []testCase[int]{
		{
			name: "empty slice",
			args: args[int]{
				[]*int{},
			},
			want: []int{},
		},
		{
			name: "slice with 1 element",
			args: args[int]{
				[]*int{&i1},
			},
			want: []int{i1},
		},
		{
			name: "slice with many elements",
			args: args[int]{
				[]*int{&i1, &i2, &i3},
			},
			want: []int{i1, i2, i3},
		},
	}
	for _, tt := range stringTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dereference(tt.args.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Dereference() = %v, want %v", got, tt.want)
			}
		})
	}
	for _, tt := range intTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dereference(tt.args.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Dereference() = %v, want %v", got, tt.want)
			}
		})
	}
}

// nolint: unused
type myStruct struct {
	a, b, c, d string
	n          int
}

func makeRandomStructs(n int) []*myStruct {
	rand.Seed(42) // nolint: staticcheck
	structs := make([]*myStruct, n)
	for i := 0; i < n; i++ {
		structs[i] = &myStruct{n: rand.Intn(n)} // nolint: gosec
	}
	return structs
}

const N = 100_000

func BenchmarkSort(b *testing.B) {
	b.Run("bool", func(b *testing.B) {
		cmpFunc := func(a, b *myStruct) int { return a.n - b.n }
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			ss := makeRandomStructs(N)
			b.StartTimer()
			SortFunc(ss, cmpFunc)
		}
	})
	b.Run("by", func(b *testing.B) {
		cmpFunc := func(a *myStruct) int { return a.n }
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			ss := makeRandomStructs(N)
			b.StartTimer()
			SortBy(ss, cmpFunc)
		}
	})
}

func ExampleSort() {
	// ExampleSort shows the best practices in sorting by multiple keys.
	// If you just have one key, use SortBy

	// Test has 3 values; we will sort by them in Rank < First < Last order
	type Test struct {
		Rank  int
		First string
		Last  string
	}
	l := []Test{
		{0, "b", "b"},
		{0, "b", "a"},
		{1, "a", "a"},
		{0, "c", "a"},
		{1, "c", "a"},
		{0, "a", "a"},
		{2, "z", "a"},
	}
	SortFunc(l, func(a, b Test) int {
		if r := cmp.Compare(a.Rank, b.Rank); r != 0 {
			return r
		}
		if r := cmp.Compare(a.First, b.First); r != 0 {
			return r
		}
		return cmp.Compare(a.Last, b.Last)
	})
	fmt.Println(l)

	// Output:
	// [{0 a a} {0 b a} {0 b b} {0 c a} {1 a a} {1 c a} {2 z a}]
}
