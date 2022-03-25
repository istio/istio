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

package workloadinstances

import (
	"istio.io/istio/pkg/util/sets"
)

// MultiValueMap represents a map where each key might be associated with
// multiple values.
type MultiValueMap map[string]sets.Set

// Insert adds given (key, value) pair into the map.
func (m MultiValueMap) Insert(key, value string) MultiValueMap {
	if values, exists := m[key]; exists {
		values.Insert(value)
		return m
	}
	m[key] = sets.NewWith(value)
	return m
}

// Delete removes given (key, value) pair out of the map.
func (m MultiValueMap) Delete(key, value string) MultiValueMap {
	if values, exists := m[key]; exists {
		values.Delete(value)
		if values.IsEmpty() {
			delete(m, key)
		}
	}
	return m
}
