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
	"testing"
)

func TestNewStringPointerSet(t *testing.T) {
	a := "1"
	b := "2"
	c := "3"
	elements := []*string{&a, &b, &c}
	set := NewStringPointerSet(elements...)

	if len(set) != len(elements) {
		t.Errorf("Expected length %d != %d", len(set), len(elements))
	}

	for _, e := range elements {
		if _, exist := set[e]; !exist {
			t.Errorf("%s is not in set %v", *e, set)
		}
	}
}

func TestStringPointerSetContains(t *testing.T) {
	a := "1"
	b := "2"
	c := "3"
	elements := []*string{&a, &b, &c}
	nonElement := "4"
	set := NewStringPointerSet(elements...)

	for _, e := range elements {
		if !set.Contains(e) {
			t.Errorf("%d is not in set %v", e, set)
		}
	}

	if set.Contains(&nonElement) {
		t.Errorf("%s should not be in set %v, but Contains returned true", nonElement, set)
	}
}
