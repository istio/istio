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
	"strconv"
	"testing"

	cmp2 "github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tests/util/leak"
)

type s struct {
	Junk string
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		s1   []int
		s2   []int
		want bool
	}{
		{"Empty Slices", []int{}, []int{}, true},
		{"Equal Slices", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"Unequal Slices", []int{1, 2, 3}, []int{3, 2, 1}, false},
		{"One Empty Slice", []int{}, []int{1, 2, 3}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Equal(tt.s1, tt.s2)
			if got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualFunc(t *testing.T) {
	tests := []struct {
		name string
		s1   []int
		s2   []int
		eq   func(int, int) bool
		want bool
	}{
		{
			name: "Equal slices",
			s1:   []int{1, 2, 3},
			s2:   []int{1, 2, 3},
			eq: func(a, b int) bool {
				return a == b
			},
			want: true,
		},
		{
			name: "Unequal slices",
			s1:   []int{1, 2, 3},
			s2:   []int{4, 5, 6},
			eq: func(a, b int) bool {
				return a == b
			},
			want: false,
		},
		{
			name: "Empty slices",
			s1:   []int{},
			s2:   []int{},
			eq: func(a, b int) bool {
				return a == b
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EqualFunc(tt.s1, tt.s2, tt.eq)
			if diff := cmp2.Diff(tt.want, got); diff != "" {
				t.Errorf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSortBy(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int
		extract  func(a int) int
		expected []int
	}{
		{
			name:     "Normal Case",
			input:    []int{4, 2, 3, 1},
			extract:  func(a int) int { return a },
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "Reverse Case",
			input:    []int{1, 2, 3, 4},
			extract:  func(a int) int { return -a },
			expected: []int{4, 3, 2, 1},
		},
		{
			name:     "Same Elements Case",
			input:    []int{2, 2, 2, 2},
			extract:  func(a int) int { return a },
			expected: []int{2, 2, 2, 2},
		},
		{
			name:     "Empty Slice Case",
			input:    []int{},
			extract:  func(a int) int { return a },
			expected: []int{},
		},
		{
			name:     "One Element Case",
			input:    []int{1},
			extract:  func(a int) int { return a },
			expected: []int{1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := SortBy(tc.input, tc.extract)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Fatalf("Expected: %+v, but got: %+v", tc.expected, result)
			}
		})
	}
}

func TestSort(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int
		expected []int
	}{
		{
			name:     "Single_Element",
			input:    []int{1},
			expected: []int{1},
		},
		{
			name:     "Already_Sorted",
			input:    []int{1, 2, 3, 4},
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "Reverse_Order",
			input:    []int{4, 3, 2, 1},
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "Unique_Elements",
			input:    []int{12, 3, 5, 1, 27},
			expected: []int{1, 3, 5, 12, 27},
		},
		{
			name:     "Empty_Slice",
			input:    []int{},
			expected: []int{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := Sort(tc.input)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Fatalf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestReverse(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		expected []int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			expected: []int{},
		},
		{
			name:     "single element",
			input:    []int{1},
			expected: []int{1},
		},
		{
			name:     "two elements",
			input:    []int{1, 2},
			expected: []int{2, 1},
		},
		{
			name:     "multiple elements",
			input:    []int{1, 2, 3, 4, 5},
			expected: []int{5, 4, 3, 2, 1},
		},
		{
			name:     "odd number of elements",
			input:    []int{1, 2, 3},
			expected: []int{3, 2, 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Reverse(tc.input)
			if diff := cmp2.Diff(tc.expected, result); diff != "" {
				t.Errorf("Reverse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClone(t *testing.T) {
	tests := []struct {
		name  string
		slice []interface{}
	}{
		{
			name:  "Empty",
			slice: []interface{}{},
		},
		{
			name:  "Single Element",
			slice: []interface{}{1},
		},
		{
			name:  "Multiple Elements",
			slice: []interface{}{1, "a", 3.14159},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := Clone(tt.slice)
			if !reflect.DeepEqual(clone, tt.slice) {
				t.Errorf("Clone() got = %v, want = %v", clone, tt.slice)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	type s struct {
		Junk string
	}
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

func TestContains(t *testing.T) {
	testCases := []struct {
		name  string
		slice []int
		v     int
		want  bool
	}{
		{
			name:  "ContainsElement",
			slice: []int{1, 2, 3, 4, 5},
			v:     3,
			want:  true,
		},
		{
			name:  "DoesNotContainElement",
			slice: []int{1, 2, 3, 4, 5},
			v:     6,
			want:  false,
		},
		{
			name:  "EmptySlice",
			slice: []int{},
			v:     1,
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Contains(tc.slice, tc.v); got != tc.want {
				t.Errorf("Contains(%v, %v) = %v; want %v", tc.slice, tc.v, got, tc.want)
			}
		})
	}
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

func TestGroup(t *testing.T) {
	tests := []struct {
		name     string
		elements []string
		fn       func(string) int
		want     map[int][]string
	}{
		{
			name:     "empty element",
			elements: []string{},
			fn: func(s string) int {
				return len(s)
			},
			want: map[int][]string{},
		},
		{
			name:     "group by the length of each element",
			elements: []string{"", "a", "b", "cc"},
			fn: func(s string) int {
				return len(s)
			},
			want: map[int][]string{0: {""}, 1: {"a", "b"}, 2: {"cc"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Group(tt.elements, tt.fn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GroupUnique got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupUnique(t *testing.T) {
	tests := []struct {
		name     string
		elements []string
		fn       func(string) int
		want     map[int]string
	}{
		{
			name:     "empty element",
			elements: []string{},
			fn: func(s string) int {
				return len(s)
			},
			want: map[int]string{},
		},
		{
			name:     "group by the length of each element",
			elements: []string{"", "a", "bb", "ccc"},
			fn: func(s string) int {
				return len(s)
			},
			want: map[int]string{0: "", 1: "a", 2: "bb", 3: "ccc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GroupUnique(tt.elements, tt.fn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GroupUnique got %v, want %v", got, tt.want)
			}
		})
	}
}

var (
	i1, i2, i3 = 1, 2, 3
	s1, s2, s3 = "a", "b", "c"
)

func TestMapFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		function func(int) *int
		want     []int
	}{
		{
			name:  "RegularMapping",
			input: []int{1, 2, 3, 4, 5},
			function: func(num int) *int {
				if num%2 == 0 {
					return &num
				}
				return nil
			},
			want: []int{2, 4},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MapFilter(tc.input, tc.function)

			if len(got) != len(tc.want) {
				t.Errorf("got %d, want %d", got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %d, want %d", got[i], tc.want[i])
				}
			}
		})
	}
}

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

func TestFlatten(t *testing.T) {
	testCases := []struct {
		name  string
		input [][]int
		want  []int
	}{
		{
			name:  "simple case",
			input: [][]int{{1, 2}, {3, 4}, {5, 6}},
			want:  []int{1, 2, 3, 4, 5, 6},
		},
		{
			name:  "empty inner slices",
			input: [][]int{{}, {}, {}},
			want:  []int{},
		},
		{
			name:  "nil slice",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty slice",
			input: [][]int{},
			want:  []int{},
		},
		{
			name:  "single element slices",
			input: [][]int{{1}, {2}, {3}},
			want:  []int{1, 2, 3},
		},
		{
			name:  "mixed empty and non-empty slices",
			input: [][]int{{1, 2}, {}, {3, 4}},
			want:  []int{1, 2, 3, 4},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Flatten(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Flatten(%v) = %v; want %v", tc.input, got, tc.want)
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

func BenchmarkEqualUnordered(b *testing.B) {
	size := 100
	var l []string
	for i := 0; i < size; i++ {
		l = append(l, strconv.Itoa(i))
	}
	var equal []string
	for i := 0; i < size; i++ {
		equal = append(equal, strconv.Itoa(i))
	}
	var notEqual []string
	for i := 0; i < size; i++ {
		notEqual = append(notEqual, strconv.Itoa(i))
	}
	notEqual[size-1] = "z"

	for n := 0; n < b.N; n++ {
		EqualUnordered(l, equal)
		EqualUnordered(l, notEqual)
	}
}
