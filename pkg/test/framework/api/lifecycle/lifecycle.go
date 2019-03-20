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

package lifecycle

// Scope represents the scope of the lifecycle for a feature.
type Scope string

const (
	// System scope for features that are part of the Istio system.
	System Scope = "System"

	// Suite scope for features that will live for the entire test suite.
	Suite Scope = "Suite"

	// Test scope for features that will live only for the length of the current test method.
	Test Scope = "Test"
)

// IsLower indicates whether this scope is lower than the one provided.
func (s Scope) IsLower(s2 Scope) bool {
	return s.precedece() < s2.precedece()
}

func (s Scope) precedece() int {
	switch s {
	case System:
		// The longest living scope, highest precedence.
		return 2
	case Suite:
		// Suite > Test
		return 1
	case Test:
		// The shortest living scope, lowest precedence
		return 0
	default:
		// Unknown
		return -1
	}
}
