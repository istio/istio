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

package webhook

import (
	"testing"

	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/ptr"
)

func TestDetectIfTagWebhookIsNeeded(t *testing.T) {
	tests := []struct {
		name            string
		revision        string
		defaultRevision *string
		operatorManage  bool
		exists          bool
		expected        bool
	}{
		{
			name:     "default installation with defaultRevision unset",
			revision: "",
			expected: true,
		},
		{
			name:            "default installation with defaultRevision set to default",
			revision:        "",
			defaultRevision: ptr.Of("default"),
			expected:        true,
		},
		{
			name:     "revisioned installation with no previous install",
			revision: "1-27-0",
			exists:   false,
			expected: true,
		},
		{
			name:     "revisioned installation with existing install",
			revision: "1-27-0",
			exists:   true,
			expected: false,
		},
		{
			name:            "default installation with defaultRevision explicitly empty",
			revision:        "",
			defaultRevision: ptr.Of(""),
			expected:        false,
		},
		{
			name:            "revisioned installation with defaultRevision explicitly empty",
			revision:        "1-27-0",
			defaultRevision: ptr.Of(""),
			expected:        false,
		},
		{
			name:            "revisioned installation with defaultRevision explicitly empty and existing installation",
			revision:        "1-27-0",
			defaultRevision: ptr.Of(""),
			exists:          true,
			expected:        false,
		},
		{
			name:           "operator managed webhooks",
			operatorManage: true,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iop := values.Map{
				"spec": values.Map{
					"values": values.Map{
						"revision": tt.revision,
						"global": values.Map{
							"operatorManageWebhooks": tt.operatorManage,
						},
					},
				},
			}

			if tt.defaultRevision != nil {
				iop["spec"].(values.Map)["values"].(values.Map)["defaultRevision"] = *tt.defaultRevision
			}

			result := detectIfTagWebhookIsNeeded(iop, tt.exists)
			if result != tt.expected {
				t.Errorf("got: %v, expected: %v", result, tt.expected)
			}
		})
	}
}
