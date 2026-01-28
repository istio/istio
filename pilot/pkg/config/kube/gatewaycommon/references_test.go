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

package gatewaycommon

import (
	"testing"

	"istio.io/istio/pkg/config"
)

func TestNormalizeReference(t *testing.T) {
	tests := []struct {
		name     string
		group    *string
		kind     *string
		def      config.GroupVersionKind
		expected config.GroupVersionKind
	}{
		{
			name:     "nil group and kind uses defaults",
			group:    nil,
			kind:     nil,
			def:      config.GroupVersionKind{Group: "default.group", Kind: "DefaultKind"},
			expected: config.GroupVersionKind{Group: "default.group", Kind: "DefaultKind"},
		},
		{
			name:     "provided group overrides default",
			group:    strPtr("custom.group"),
			kind:     nil,
			def:      config.GroupVersionKind{Group: "default.group", Kind: "DefaultKind"},
			expected: config.GroupVersionKind{Group: "custom.group", Kind: "DefaultKind"},
		},
		{
			name:     "provided kind overrides default",
			group:    nil,
			kind:     strPtr("CustomKind"),
			def:      config.GroupVersionKind{Group: "default.group", Kind: "DefaultKind"},
			expected: config.GroupVersionKind{Group: "default.group", Kind: "CustomKind"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeReference(tt.group, tt.kind, tt.def)
			if result.Group != tt.expected.Group || result.Kind != tt.expected.Kind {
				t.Errorf("NormalizeReference() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestNewReferenceSet(t *testing.T) {
	rs := NewReferenceSet()
	if rs == nil {
		t.Error("NewReferenceSet() returned nil")
	}
	if rs.erasedCollections == nil {
		t.Error("NewReferenceSet() did not initialize erasedCollections")
	}
}

func strPtr(s string) *string {
	return &s
}
