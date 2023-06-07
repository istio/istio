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

package cli

import "testing"

func Test_handleNamespace(t *testing.T) {
	ns := handleNamespace("test", "default")
	if ns != "test" {
		t.Fatalf("Get the incorrect namespace: %q back", ns)
	}

	tests := []struct {
		description      string
		namespace        string
		defaultNamespace string
		wantNamespace    string
	}{
		{
			description:      "return test namespace",
			namespace:        "test",
			defaultNamespace: "default",
			wantNamespace:    "test",
		},
		{
			description:      "return default namespace",
			namespace:        "",
			defaultNamespace: "default",
			wantNamespace:    "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			gotNs := handleNamespace(tt.namespace, tt.defaultNamespace)
			if gotNs != tt.wantNamespace {
				t.Fatalf("unexpected namespace: wanted %v got %v", tt.wantNamespace, gotNs)
			}
		})
	}
}
