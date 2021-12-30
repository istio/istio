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

package ast

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestParse(t *testing.T) {
	cases := []struct {
		input    string
		expected *Metadata
	}{
		{
			input:    ``,
			expected: &Metadata{},
		},
		{
			input: `
collections:
  - name:  "istio/meshconfig"
    kind:  "MeshConfig"
    group: ""

resources:
  - kind:         "VirtualService"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`,
			expected: &Metadata{
				Collections: []*Collection{
					{
						Name:         "istio/meshconfig",
						VariableName: "IstioMeshconfig",
						Description:  "describes the collection istio/meshconfig",
						Kind:         "MeshConfig",
						Group:        "",
					},
				},
				Resources: []*Resource{
					{
						Kind:         "VirtualService",
						Group:        "networking.istio.io",
						Version:      "v1alpha3",
						Proto:        "istio.networking.v1alpha3.VirtualService",
						ProtoPackage: "istio.io/api/networking/v1alpha3",
						Validate:     "ValidateVirtualService",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewWithT(t)
			actual, err := Parse(c.input)
			g.Expect(err).To(BeNil())
			g.Expect(actual).To(Equal(c.expected))
		})
	}
}
