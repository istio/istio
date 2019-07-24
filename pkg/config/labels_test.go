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

import "testing"

func TestLabels(t *testing.T) {
	a := Labels{"app": "a"}
	b := Labels{"app": "b"}
	a1 := Labels{"app": "a", "prod": "env"}
	ab := LabelsCollection{a, b}
	a1b := LabelsCollection{a1, b}
	none := LabelsCollection{}

	// equivalent to empty tag list
	singleton := LabelsCollection{nil}

	if !Labels(nil).SubsetOf(a) {
		t.Errorf("nil.SubsetOf({a}) => Got false")
	}

	if a.SubsetOf(nil) {
		t.Errorf("{a}.SubsetOf(nil) => Got true")
	}

	matching := []struct {
		tag  Labels
		list LabelsCollection
	}{
		{a, ab},
		{b, ab},
		{a, none},
		{a, nil},
		{a, singleton},
		{a1, ab},
		{b, a1b},
	}

	if (LabelsCollection{a}).HasSubsetOf(b) {
		t.Errorf("{a}.HasSubsetOf(b) => Got true")
	}

	if a1.SubsetOf(a) {
		t.Errorf("%v.SubsetOf(%v) => Got true", a1, a)
	}

	for _, pair := range matching {
		if !pair.list.HasSubsetOf(pair.tag) {
			t.Errorf("%v.HasSubsetOf(%v) => Got false", pair.list, pair.tag)
		}
	}
}
