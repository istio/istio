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

var h1 = &HandlerStatic{Name: "h1", Adapter: a1}
var h2 = &HandlerStatic{Name: "h2", Adapter: a2}

var i1 = &InstanceStatic{Name: "i1", Template: t1}
var i2 = &InstanceStatic{Name: "i2", Template: t1}
var i3 = &InstanceStatic{Name: "i3", Template: t1}

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

	s.HandlersStatic = make(map[string]*HandlerStatic, 2)
	s.HandlersStatic[h1.Name] = h1
	s.HandlersStatic[h2.Name] = h2

	s.InstancesStatic = make(map[string]*InstanceStatic, 3)
	s.InstancesStatic[i1.Name] = i1
	s.InstancesStatic[i2.Name] = i2
	s.InstancesStatic[i3.Name] = i3

	return s
}

func assertEqualGrouping(t *testing.T, expected map[*HandlerStatic][]*InstanceStatic, actual map[*HandlerStatic][]*InstanceStatic) {
	// sort the instances, so that they have stable ordering matches.
	sortInstances(expected)
	sortInstances(actual)

	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("grouping differences: \n%v\n!=\n%v\n", actual, expected)
	}
}

func sortInstances(m map[*HandlerStatic][]*InstanceStatic) {
	for _, v := range m {
		names := make([]string, 0, len(v))
		instancesByName := make(map[string]*InstanceStatic)
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
	s.Rules = append(s.Rules,
		&Rule{
			Name: "r1",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h1,
					Instances: []*InstanceStatic{i1},
				},
			},
		})

	// The adapter should be referenced by the single instance
	expected := map[*HandlerStatic][]*InstanceStatic{
		h1: {i1},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}

func TestGetInstancesGroupedByHandlers_MultiRuleRef(t *testing.T) {
	s := buildBaseConfig()

	s.Rules = append(s.Rules,
		&Rule{
			Name: "r1",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h1,
					Instances: []*InstanceStatic{i1},
				},
			},
		},
		&Rule{
			Name: "r2",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h1,
					Instances: []*InstanceStatic{i1},
				},
			},
		})

	// Multiple rules referencing the same handler using the same instance.
	expected := map[*HandlerStatic][]*InstanceStatic{
		h1: {i1},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}

func TestGetInstancesGroupedByHandlers_MultipleInstances_SingleHandler(t *testing.T) {
	s := buildBaseConfig()

	s.Rules = append(s.Rules,
		&Rule{
			Name: "r1",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h1,
					Instances: []*InstanceStatic{i1, i2},
				},
			},
		},
		&Rule{
			Name: "r2",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h1,
					Instances: []*InstanceStatic{i2, i3},
				},
			},
		})

	// Multiple instances in multiple rules referencing the same handler.
	expected := map[*HandlerStatic][]*InstanceStatic{
		h1: {i1, i2, i3},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}

func TestGetInstancesGroupedByHandlers_Multiple(t *testing.T) {
	s := buildBaseConfig()

	// Multiple instances in multiple rules referencing different handlers.
	s.Rules = append(s.Rules,
		&Rule{
			Name: "r1",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h1,
					Instances: []*InstanceStatic{i1, i2},
				},
			},
		},
		&Rule{
			Name: "r2",
			ActionsStatic: []*ActionStatic{
				{
					Handler:   h2,
					Instances: []*InstanceStatic{i2, i3},
				},
			},
		})

	// Multiple instances in multiple rules referencing different handlers.
	expected := map[*HandlerStatic][]*InstanceStatic{
		h1: {i1, i2},
		h2: {i2, i3},
	}

	actual := GetInstancesGroupedByHandlers(s)
	assertEqualGrouping(t, expected, actual)
}
