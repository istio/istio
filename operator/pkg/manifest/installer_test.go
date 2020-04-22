// Copyright 2020 Istio Authors
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

package manifest

import (
	"testing"
)

func TestCanSkipCRD(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		result bool
	}{
		{
			"re-apply",
			`namespace/istio-system unchanged

customresourcedefinition.apiextensions.k8s.io/adapters.config.istio.io unchanged
customresourcedefinition.apiextensions.k8s.io/attributemanifests.config.istio.io unchanged
`,
			true,
		},
		{
			"first apply",
			`namespace/istio-system unchanged

customresourcedefinition.apiextensions.k8s.io/adapters.config.istio.io created
customresourcedefinition.apiextensions.k8s.io/attributemanifests.config.istio.io created
`,
			false,
		},
		{
			"re-apply",
			`namespace/istio-system unchanged

customresourcedefinition.apiextensions.k8s.io/adapters.config.istio.io configured
customresourcedefinition.apiextensions.k8s.io/attributemanifests.config.istio.io configured
`,
			false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := canSkipCrdWait(tt.in)
			if got != tt.result {
				t.Fatalf("got %v, want %v", got, tt.result)
			}
		})
	}
}
