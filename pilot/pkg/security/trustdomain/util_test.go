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

package trustdomain

import (
	"testing"
)

func TestStringMatch(t *testing.T) {
	tests := []struct {
		desc     string
		element  string
		list     []string
		expected bool
	}{
		{
			desc:     "wildcard",
			element:  "*",
			list:     []string{"match-me"},
			expected: true,
		},
		{
			desc:     "suffix=match",
			element:  "yo*",
			list:     []string{"yo", "yolo", "yo-yo"},
			expected: true,
		},
		{
			desc:     "no-sub",
			element:  "foo",
			list:     []string{"bar"},
			expected: false,
		},
		{
			desc:     "prefix-match",
			element:  "*yo",
			list:     []string{"yoyo", "goyo"},
			expected: true,
		},
		{
			desc:     "empty",
			element:  "",
			list:     []string{"yo", "tawg"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := stringMatch(tt.element, tt.list); got != tt.expected {
				t.Errorf("%s: expected %v got %v", tt.desc, tt.expected, got)
			}
		})
	}
}
