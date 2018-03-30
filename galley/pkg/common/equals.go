//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package common

// MapEquals checks for equality of two maps, excluding the given keys.
func MapEquals(m1 map[string]string, m2 map[string]string, exclude ...string) bool {
	if m1 == nil || m2 == nil {
		return m1 == nil && m2 == nil
	}

	m1len := 0
l1:
	for k, v1 := range m1 {
		for _, e := range exclude {
			if k == e {
				continue l1
			}
		}

		if v2, found := m2[k]; !found || v1 != v2 {
			return false
		}
		m1len++
	}

	m2len := len(m2)
	for _, e := range exclude {
		if _, found := m2[e]; found {
			m2len--
		}
	}

	return m1len == m2len
}
