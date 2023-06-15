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

package authz

import (
	"testing"
)

func TestExtractName(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedName  string
		expectedIndex string
	}{
		{
			name:          "policy name is some-policy.default",
			input:         "ns[default]-policy[some-policy]-rule[1]",
			expectedName:  "some-policy.default",
			expectedIndex: "1",
		},
		{
			name:          "empty rule",
			input:         "ns[default]-policy[some-policy]-rule[]",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "empty policy",
			input:         "ns[default]-policy[]-rule[1]",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "empty ns",
			input:         "ns[]-policy[some-policy]-rule[1]",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "empty ns policy rule",
			input:         "ns[]-policy[]-rule[]",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "invalid name",
			input:         "invalid-name",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "no policy",
			input:         "ns[default]-rule[1]",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "no rule",
			input:         "ns[default]-policy[some-policy]",
			expectedName:  "",
			expectedIndex: "",
		},
		{
			name:          "no ns",
			input:         "policy[some-policy]-rule[1]",
			expectedName:  "",
			expectedIndex: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actualName, actualIndex := extractName(tt.input); actualName != tt.expectedName || actualIndex != tt.expectedIndex {
				t.Errorf("extractName() got %v %v, want %v %v", actualName, actualIndex, tt.expectedName, tt.expectedIndex)
			}
		})
	}
}
