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

// GetInstancesGroupedByHandlers queries the snapshot and returns all used instances, grouped by the handlers that will
// receive them.
func GetInstancesGroupedByHandlers(s *Snapshot) map[*Handler][]*Instance {
	// There is no set in Go? map[<item>]bool to the rescue!
	m := make(map[*Handler]map[*Instance]bool)

	// Grovel over rules/actions and for each handler create a map entry and place all the instances in that action
	// as values.
	for _, r := range s.Rules {
		for _, a := range r.Actions {
			instances, found := m[a.Handler]
			if !found {
				instances = make(map[*Instance]bool)
				m[a.Handler] = instances
			}

			for _, i := range a.Instances {
				instances[i] = true
			}
		}
	}

	result := make(map[*Handler][]*Instance, len(m))
	for k, v := range m {
		i := 0
		instances := make([]*Instance, len(v))
		for instance := range v {
			instances[i] = instance
			i++
		}
		result[k] = instances
	}
	return result
}
