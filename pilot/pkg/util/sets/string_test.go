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
	"testing"
)

func TestNewSet(t *testing.T) {
	elements := []string{"a", "b", "c"}
	set := NewSet(elements...)

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
	want := NewSet("a", "b")
	for _, sets := range [][]Set{
		{NewSet(elements...), NewSet(elements2...)},
		{NewSet(elements2...), NewSet(elements...)},
	} {
		s1, s2 := sets[0], sets[1]
		if got := s1.Union(s2); !got.Equals(want) {
			t.Errorf("expected %v; got %v", want, got)
		}
	}
}

func TestDifference(t *testing.T) {
	elements := []string{"a", "b", "c", "d"}
	s1 := NewSet(elements...)

	elements2 := []string{"a", "b", "e"}
	s2 := NewSet(elements2...)

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
	s1 := NewSet(elements...)

	elements2 := []string{"a", "b", "c"}
	s2 := NewSet(elements2...)

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
	elements := []string{"a", "b", "c", "d"}
	s1 := NewSet(elements...)

	elements2 := []string{"a", "b"}
	s2 := NewSet(elements2...)

	if !s1.SupersetOf(s2) {
		t.Errorf("%v should be superset of %v", s1.SortedList(), s2.SortedList())
	}

	s3 := NewSet()
	if !NewSet().SupersetOf(s3) {
		fmt.Printf("%q\n", s3.SortedList()[0])
		t.Errorf("%v should be superset of empty set", s1.SortedList())
	}
}

func TestEquals(t *testing.T) {
	tests := []struct {
		name   string
		first  Set
		second Set
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
			NewSet("test"),
			NewSet("test", "test1"),
			false,
		},
		{
			"equal sets",
			NewSet("test", "test1"),
			NewSet("test", "test1"),
			true,
		},
		{
			"unequal sets",
			NewSet("test", "test1"),
			NewSet("test", "test2"),
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
