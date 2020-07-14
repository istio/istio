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

package resource

// StringMap is used to store labels and annotations.
type StringMap map[string]string

// Clone the StringMap
func (s StringMap) Clone() StringMap {
	if s == nil {
		return nil
	}

	m := make(map[string]string, len(s))
	for k, v := range s {
		m[k] = v
	}

	return m
}

// CloneOrCreate clones a StringMap. It creates the map if it doesn't exist.
func (s StringMap) CloneOrCreate() StringMap {
	m := s.Clone()
	if m == nil {
		m = make(map[string]string)
	}
	return m
}

// Remove the given name from the string map
func (s StringMap) Delete(name string) {
	if s != nil {
		delete(s, name)
	}
}
