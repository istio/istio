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

// GetInstancesGroupedByHandlers queries the snapshot and returns all used instances, grouped by the handlers that will
// receive them.
func GetInstancesGroupedByHandlers(s *Snapshot) map[*HandlerStatic][]*InstanceStatic {
	m := make(map[*HandlerStatic]map[*InstanceStatic]struct{})

	// Grovel over rules/actions and for each handler create a map entry and place all the instances in that action
	// as values.
	for _, r := range s.Rules {
		for _, a := range r.ActionsStatic {
			instances, found := m[a.Handler]
			if !found {
				instances = make(map[*InstanceStatic]struct{})
				m[a.Handler] = instances
			}

			for _, i := range a.Instances {
				instances[i] = struct{}{}
			}
		}
	}

	result := make(map[*HandlerStatic][]*InstanceStatic, len(m))
	for k, v := range m {
		i := 0
		instances := make([]*InstanceStatic, len(v))
		for instance := range v {
			instances[i] = instance
			i++
		}
		result[k] = instances
	}
	return result
}

// GetInstancesGroupedByHandlersDynamic queries the snapshot and returns all used instances, grouped by the handlers that will
// receive them.
func GetInstancesGroupedByHandlersDynamic(s *Snapshot) map[*HandlerDynamic][]*InstanceDynamic {
	m := make(map[*HandlerDynamic]map[*InstanceDynamic]struct{})

	// Grovel over rules/actions and for each handler create a map entry and place all the instances in that action
	// as values.
	for _, r := range s.Rules {
		for _, a := range r.ActionsDynamic {
			instances, found := m[a.Handler]
			if !found {
				instances = make(map[*InstanceDynamic]struct{})
				m[a.Handler] = instances
			}

			for _, i := range a.Instances {
				instances[i] = struct{}{}
			}
		}
	}

	result := make(map[*HandlerDynamic][]*InstanceDynamic, len(m))
	for k, v := range m {
		i := 0
		instances := make([]*InstanceDynamic, len(v))
		for instance := range v {
			instances[i] = instance
			i++
		}
		result[k] = instances
	}
	return result
}
