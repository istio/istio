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
	"testing"
)

func TestGetInstancesGroupedByHandlers_Empty(t *testing.T) {
	s := Empty()
	m := GetInstancesGroupedByHandlers(s)
	if len(m) > 0 {
		t.Fail()
	}
}

func TestGetInstancesGroupedByHandlers_NoRuleRef(t *testing.T) {
	s := Empty()

	h1 := &Handler{Name: "h1"}
	s.Handlers["h1"] = h1

	i1 := &Instance{Name: "i1"}
	s.Instances["i1"] = i1

	m := GetInstancesGroupedByHandlers(s)
	if len(m) > 0 {
		t.Fail()
	}
}

func TestGetInstancesGroupedByHandlers_SingleRuleRef(t *testing.T) {
	s := Empty()
	h1 := &Handler{Name: "h1"}
	s.Handlers["h1"] = h1

	i1 := &Instance{Name: "i1"}
	s.Instances["i1"] = i1
	s.Rules = append(s.Rules, &Rule{
		Name: "r1",
		Actions: []*Action{
			{
				Handler:   h1,
				Instances: []*Instance{i1},
			},
		},
	})
	m := GetInstancesGroupedByHandlers(s)
	if len(m) != 1 {
		t.Fail()
	}

	if _, found := m[h1]; !found {
		t.Fail()
	}

	if len(m[h1]) != 1 {
		t.Fail()
	}
}

func TestGetInstancesGroupedByHandlers_MultiRuleRef(t *testing.T) {
	s := Empty()
	h1 := &Handler{Name: "h1"}
	s.Handlers["h1"] = h1

	i1 := &Instance{Name: "i1"}
	s.Instances["i1"] = i1
	s.Rules = append(s.Rules,
		&Rule{
			Name: "r1",
			Actions: []*Action{
				{
					Handler:   h1,
					Instances: []*Instance{i1},
				},
			},
		},
		&Rule{
			Name: "r2",
			Actions: []*Action{
				{
					Handler:   h1,
					Instances: []*Instance{i1},
				},
			},
		})

	m := GetInstancesGroupedByHandlers(s)
	if len(m) != 1 {
		t.Fail()
	}

	if _, found := m[h1]; !found {
		t.Fail()
	}

	if len(m[h1]) != 1 {
		t.Fail()
	}
}

func TestGetInstancesGroupedByHandlers_Multiiple(t *testing.T) {
	s := Empty()
	h1 := &Handler{Name: "h1"}
	s.Handlers["h1"] = h1

	h2 := &Handler{Name: "h2"}
	s.Handlers["h2"] = h2

	i1 := &Instance{Name: "i1"}
	s.Instances["i1"] = i1

	i2 := &Instance{Name: "i2"}
	s.Instances["i2"] = i2

	i3 := &Instance{Name: "i3"}
	s.Instances["i3"] = i3

	s.Rules = append(s.Rules,
		&Rule{
			Name: "r1",
			Actions: []*Action{
				{
					Handler:   h1,
					Instances: []*Instance{i1, i2},
				},
			},
		},
		&Rule{
			Name: "r2",
			Actions: []*Action{
				{
					Handler:   h2,
					Instances: []*Instance{i2, i3},
				},
			},
		})

	m := GetInstancesGroupedByHandlers(s)
	if len(m) != 2 {
		t.Fail()
	}

	if _, found := m[h1]; !found {
		t.Fail()
	}
	if len(m[h1]) != 2 {
		t.Fail()
	}
	if len(m[h1]) != 2 {
		t.Fail()
	}

	// h1 should be referenced by i1 and i2
	if !((m[h1][0] == i1 && m[h1][1] == i2) || (m[h1][1] == i1 && m[h1][0] == i2)) {
		t.Fail()
	}

	if _, found := m[h2]; !found {
		t.Fail()
	}
	if len(m[h2]) != 2 {
		t.Fail()
	}

	// h2 should be referenced by i2 and i3
	if !((m[h2][0] == i2 && m[h2][1] == i3) || (m[h2][1] == i2 && m[h2][0] == i3)) {
		t.Fail()
	}
}
