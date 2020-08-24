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

package jsonmap

// Map provides fluent access to nested map[string]interface{} usually produced
// from deserializing json/yaml.
type Map map[string]interface{}

// Map returns a nested map at the given key.
// If there is no Map at the given key, nil is returned.
func (m Map) Map(key string) Map {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		switch mm := v.(type) {
		case Map:
			return mm
		case map[string]interface{}:
			return mm
		}
	}
	return nil
}

// Ensure returns a nested Map at the given key, creating an empty one if there is no map at the key.
// If called on a nil Map there will be no effect.
func (m Map) Ensure(key string) Map {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		switch mv := v.(type) {
		case Map:
			return mv
		case map[string]interface{}:
			return mv
		}
	}
	m[key] = Map{}
	return m[key].(Map)
}

// String returns a string at the given key.
// If there is no string for the given key, the zero-value is returned.
func (m Map) String(key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
