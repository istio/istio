// Copyright 2018 Istio Authors
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
	"reflect"
	"sort"
	"testing"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
)

func TestGetInstancesGroupedByHandlers_Empty(t *testing.T) {
	s := Empty()
	m := GetInstancesGroupedByHandlers(s)
	if len(m) > 0 {
		t.Fail()
	}
}

var a1 = &adapter.Info{Name: "a1"}
var a2 = &adapter.Info{Name: "a2"}

var t1 = &template.Info{Name: "t1"}

var h1 = &HandlerLegacy{Name: "h1", Adapter: a1}
var h2 = &HandlerLegacy{Name: "h2", Adapter: a2}

var i1 = &InstanceLegacy{Name: "i1", Template: t1}
var i2 = &InstanceLegacy{Name: "i2", Template: t1}
var i3 = &InstanceLegacy{Name: "i3", Template: t1}

// builds a base configuration with no rules.
func buildBaseConfig() *Snapshot {
	s := Empty()
	s.Adapters = map[string]*adapter.Info{
		a1.Name: a1,
		a2.Name: a2,
	}
	s.Templates = map[string]*template.Info{
		t1.Name: t1,
	}

	s.HandlersLegacy = make(map[string]*HandlerLegacy, 2)
	s.HandlersLegacy[h1.Name] = h1
	s.HandlersLegacy[h2.Name] = h2

	s.InstancesLegacy = make(map[string]*InstanceLegacy, 3)
	s.InstancesLegacy[i1.Name] = i1
	s.InstancesLegacy[i2.Name] = i2
	s.InstancesLegacy[i3.Name] = i3

	return s
}

func assertEqualGrouping(t *testing.T, expected map[*HandlerLegacy][]*InstanceLegacy, actual map[*HandlerLegacy][]*InstanceLegacy) {
	// sort the instances, so that they have stable ordering matches.
	sortInstances(expected)
	sortInstances(actual)

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("grouping differences: \n%v\n!=\n%v\n", actual, expected)
	}
}

func sortInstances(m map[*HandlerLegacy][]*InstanceLegacy) {
	for _, v := range m {
		names := make([]string, 0, len(v))
		instancesByName := make(map[string]*InstanceLegacy)
		for _, s := range v {
			names = append(names, s.Name)
			instancesByName[s.Name] = s
		}
		sort.Strings(names)

		// replace in place
		for i, name := range names {
			v[i] = instancesByName[name]
		}
	}
}

func TestGetInstancesGroupedByHandlers_NoRuleRef(t *testing.T) {
	s := buildBaseConfig()

	// no rules, even though there are instances and adapters. There should be no entries.
	m := GetInstancesGroupedByHandlers(s)
	if len(m) > 0 {
		t.Fail()
	}
}

func TestGetInstancesGroupedByHandlers_SingleRuleRef(t *testing.T) {
	s := buildBaseConfig()

	// single rule referencing the instance.
	s.RulesLegacy = append(s.RulesLegacy,
		&RuleLegacy{
			Name: "r1",
			Actions: []*ActionLegacy{
				{
					Handler:   h1,
					Instances: []*InstanceLegacy{i1},
				},
			},
		})

	// The adapter should be referenced by the single instance
	expected := map[*HandlerLegacy][]*InstanceLegacy{
		h1: {i1},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}

func TestGetInstancesGroupedByHandlers_MultiRuleRef(t *testing.T) {
	s := buildBaseConfig()

	s.RulesLegacy = append(s.RulesLegacy,
		&RuleLegacy{
			Name: "r1",
			Actions: []*ActionLegacy{
				{
					Handler:   h1,
					Instances: []*InstanceLegacy{i1},
				},
			},
		},
		&RuleLegacy{
			Name: "r2",
			Actions: []*ActionLegacy{
				{
					Handler:   h1,
					Instances: []*InstanceLegacy{i1},
				},
			},
		})

	// Multiple rules referencing the same handler using the same instance.
	expected := map[*HandlerLegacy][]*InstanceLegacy{
		h1: {i1},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}

func TestGetInstancesGroupedByHandlers_MultipleInstances_SingleHandler(t *testing.T) {
	s := buildBaseConfig()

	s.RulesLegacy = append(s.RulesLegacy,
		&RuleLegacy{
			Name: "r1",
			Actions: []*ActionLegacy{
				{
					Handler:   h1,
					Instances: []*InstanceLegacy{i1, i2},
				},
			},
		},
		&RuleLegacy{
			Name: "r2",
			Actions: []*ActionLegacy{
				{
					Handler:   h1,
					Instances: []*InstanceLegacy{i2, i3},
				},
			},
		})

	// Multiple instances in multiple rules referencing the same handler.
	expected := map[*HandlerLegacy][]*InstanceLegacy{
		h1: {i1, i2, i3},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}

func TestGetInstancesGroupedByHandlers_Multiple(t *testing.T) {
	s := buildBaseConfig()

	// Multiple instances in multiple rules referencing different handlers.
	s.RulesLegacy = append(s.RulesLegacy,
		&RuleLegacy{
			Name: "r1",
			Actions: []*ActionLegacy{
				{
					Handler:   h1,
					Instances: []*InstanceLegacy{i1, i2},
				},
			},
		},
		&RuleLegacy{
			Name: "r2",
			Actions: []*ActionLegacy{
				{
					Handler:   h2,
					Instances: []*InstanceLegacy{i2, i3},
				},
			},
		})

	// Multiple instances in multiple rules referencing different handlers.
	expected := map[*HandlerLegacy][]*InstanceLegacy{
		h1: {i1, i2},
		h2: {i2, i3},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}
