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

package slices

import (
	"testing"
)

func TestContainsString(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		match    string
		expected bool
	}{
		{
			name:     "test for contains string with contains string",
			values:   []string{"localhost", "192.168.0.1", "128.0.0.1"},
			match:    "128.0.0.1",
			expected: true,
		},
		{
			name:     "test for contains string without contains string",
			values:   []string{"localhost", "192.168.0.1", "128.0.0.1"},
			match:    "192.168.0.2",
			expected: false,
		},
		{
			name:     "test for contains string with empty vlaues",
			values:   []string{},
			match:    "192.168.0.2",
			expected: false,
		},
		{
			name:     "test for contains string with empty string",
			values:   []string{"localhost", "192.168.0.1", "128.0.0.1"},
			match:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		result := ContainsString(tt.values, tt.match)
		if result != tt.expected {
			t.Errorf("Test %s failed, expected: %v ,got: %v", tt.name, result, tt.expected)
		}
	}
}
