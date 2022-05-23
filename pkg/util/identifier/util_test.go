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

package identifier

import "testing"

func TestIsSameOrEmpty(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{
			name:     "a and b are same empty",
			expected: true,
		},
		{
			name:     "b is empty",
			a:        "foo",
			expected: true,
		},
		{
			name:     "a is empty",
			b:        "bar",
			expected: true,
		},
		{
			name:     "a and b are same",
			a:        "bar",
			b:        "bar",
			expected: true,
		},
		{
			name:     "a and b are not same",
			a:        "foo",
			b:        "bar",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := IsSameOrEmpty(tt.a, tt.b)
			if res != tt.expected {
				t.Errorf("IsSameOrEmpty() = %v, want %v", res, tt.expected)
			}
		})
	}
}
