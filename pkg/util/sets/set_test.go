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

package sets

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/test/util/assert"
)

func TestNewSet(t *testing.T) {
	elements := []string{"a", "b", "c"}
	set := New(elements...)

	if len(set) != len(elements) {
		t.Errorf("Expected length %d != %d", len(set), len(elements))
	}

	for _, e := range elements {
		if _, exist := set[e]; !exist {
			t.Errorf("%s is not in set %v", e, set)
		}
	}
}

func TestUnion(t *testing.T) {
	elements := []string{"a", "b", "c", "d"}
	elements2 := []string{"a", "b", "e"}
	want := New("a", "b", "c", "d", "e")
	for _, sets := range [][]Set[string]{
		{New(elements...), New(elements2...)},
		{New(elements2...), New(elements...)},
	} {
		s1, s2 := sets[0], sets[1]
		if got := s1.Union(s2); !got.Equals(want) {
			t.Errorf("expected %v; got %v", want, got)
		}
	}
}

func TestDifference(t *testing.T) {
	elements := []string{"a", "b", "c", "d"}
	s1 := New(elements...)

	elements2 := []string{"a", "b", "e"}
	s2 := New(elements2...)

	d := s1.Difference(s2)

	if len(d) != 2 {
		t.Errorf("Expected len=2: %d", len(d))
	}

	if _, exist := d["c"]; !exist {
		t.Errorf("c is not in %v", d)
	}
	if _, exist := d["d"]; !exist {
		t.Errorf("d is not in %v", d)
	}
}

func TestIntersection(t *testing.T) {
	elements := []string{"a", "b", "d"}
	s1 := New(elements...)

	elements2 := []string{"a", "b", "c"}
	s2 := New(elements2...)

	d := s1.Intersection(s2)

	if len(d) != 2 {
		t.Errorf("Expected len=2: %d", len(d))
	}

	if _, exist := d["a"]; !exist {
		t.Errorf("a is not in %v", d)
	}
	if _, exist := d["b"]; !exist {
		t.Errorf("b is not in %v", d)
	}
}

func TestSupersetOf(t *testing.T) {
	testCases := []struct {
		name   string
		first  Set[string]
		second Set[string]
		want   bool
	}{
		{
			name:   "both nil",
			first:  nil,
			second: nil,
			want:   true,
		},
		{
			name:   "first nil",
			first:  nil,
			second: New("a"),
			want:   false,
		},
		{
			name:   "second nil",
			first:  New("a"),
			second: nil,
			want:   true,
		},
		{
			name:   "both empty",
			first:  New[string](),
			second: New[string](),
			want:   true,
		},
		{
			name:   "first empty",
			first:  New[string](),
			second: New("a"),
			want:   false,
		},
		{
			name:   "second empty",
			first:  New("a"),
			second: New[string](),
			want:   true,
		},
		{
			name:   "equal",
			first:  New("a", "b"),
			second: New("a", "b"),
			want:   true,
		},
		{
			name:   "first contains all second",
			first:  New("a", "b", "c"),
			second: New("a", "b"),
			want:   true,
		},
		{
			name:   "second contains all first",
			first:  New("a", "b"),
			second: New("a", "b", "c"),
			want:   false,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.first.SupersetOf(tt.second); got != tt.want {
				t.Errorf("want %v, but got %v", tt.want, got)
			}
		})
	}
}

func BenchmarkSupersetOf(b *testing.B) {
	set1 := New[string]()
	for i := 0; i < 1000; i++ {
		set1.Insert(fmt.Sprint(i))
	}
	set2 := New[string]()
	for i := 0; i < 50; i++ {
		set2.Insert(fmt.Sprint(i))
	}
	b.ResetTimer()

	b.Run("SupersetOf", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			set1.SupersetOf(set2)
		}
	})
}

func TestEquals(t *testing.T) {
	tests := []struct {
		name   string
		first  Set[string]
		second Set[string]
		want   bool
	}{
		{
			"both nil",
			nil,
			nil,
			true,
		},
		{
			"unequal length",
			New("test"),
			New("test", "test1"),
			false,
		},
		{
			"equal sets",
			New("test", "test1"),
			New("test", "test1"),
			true,
		},
		{
			"unequal sets",
			New("test", "test1"),
			New("test", "test2"),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.first.Equals(tt.second); got != tt.want {
				t.Errorf("Unexpected Equal. got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	cases := []struct {
		s1, s2   Set[string]
		expected []string
	}{
		{
			s1:       New("a1", "a2"),
			s2:       New("a1", "a2"),
			expected: []string{"a1", "a2"},
		},
		{
			s1:       New("a1", "a2", "a3"),
			s2:       New("a1", "a2"),
			expected: []string{"a1", "a2", "a3"},
		},
		{
			s1:       New("a1", "a2"),
			s2:       New("a3", "a4"),
			expected: []string{"a1", "a2", "a3", "a4"},
		},
	}

	for _, tc := range cases {
		got := tc.s1.Merge(tc.s2)
		assert.Equal(t, tc.expected, SortedList(got))
	}
}

func TestInsertAll(t *testing.T) {
	tests := []struct {
		name  string
		s     Set[string]
		items []string
		want  Set[string]
	}{
		{
			name:  "insert new item",
			s:     New("a1", "a2"),
			items: []string{"a3"},
			want:  New("a1", "a2", "a3"),
		},
		{
			name:  "inserted item already exists",
			s:     New("a1", "a2"),
			items: []string{"a1"},
			want:  New("a1", "a2"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.InsertAll(tt.items...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InsertAll() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInsertContains(t *testing.T) {
	s := New[string]()
	assert.Equal(t, s.InsertContains("k1"), false)
	assert.Equal(t, s.InsertContains("k1"), true)
	assert.Equal(t, s.InsertContains("k2"), false)
	assert.Equal(t, s.InsertContains("k2"), true)
}

func BenchmarkSet(b *testing.B) {
	containsTest := New[string]()
	for i := 0; i < 1000; i++ {
		containsTest.Insert(fmt.Sprint(i))
	}
	sortOrder := []string{}
	for i := 0; i < 1000; i++ {
		sortOrder = append(sortOrder, fmt.Sprint(rand.Intn(1000)))
	}
	b.ResetTimer()
	var s Set[string] // ensure no inlining
	b.Run("insert", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			s = New[string]()
			for i := 0; i < 1000; i++ {
				s.Insert("item")
			}
		}
	})
	b.Run("contains", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			containsTest.Contains("100")
		}
	})
	b.Run("sorted", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			b.StopTimer()
			s := New(sortOrder...)
			b.StartTimer()
			SortedList(s)
		}
	})
}

func TestMapOfSet(t *testing.T) {
	m := map[int]String{}
	InsertOrNew(m, 1, "a")
	InsertOrNew(m, 1, "b")
	InsertOrNew(m, 2, "c")
	assert.Equal(t, m, map[int]String{1: New("a", "b"), 2: New("c")})

	DeleteCleanupLast(m, 1, "a")
	assert.Equal(t, m, map[int]String{1: New("b"), 2: New("c")})
	DeleteCleanupLast(m, 1, "b")
	DeleteCleanupLast(m, 1, "not found")
	assert.Equal(t, m, map[int]String{2: New("c")})
}

func TestSetString(t *testing.T) {
	elements := []string{"a"}
	set := New(elements...)
	assert.Equal(t, "[a]", set.String())
}
