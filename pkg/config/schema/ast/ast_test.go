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
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
)

func TestParse(t *testing.T) {
	var cases = []struct {
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

snapshots:
  - name: "default"
    collections:
      - "istio/meshconfig"

resources:
  - kind:         "VirtualService"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"

transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/destinationrules": "istio/networking/v1alpha3/destinationrules"
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
				Snapshots: []*Snapshot{
					{
						Name: "default",
						Collections: []string{
							"istio/meshconfig",
						},
						VariableName: "Default",
						Description:  "describes the snapshot default",
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
				TransformSettings: []TransformSettings{
					&DirectTransformSettings{
						Mapping: map[string]string{
							"k8s/networking.istio.io/v1alpha3/destinationrules": "istio/networking/v1alpha3/destinationrules",
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)
			actual, err := Parse(c.input)
			g.Expect(err).To(BeNil())
			g.Expect(actual).To(Equal(c.expected))
		})
	}
}

func TestTransformParseError(t *testing.T) {
	var cases = []string{
		`
collections:
  - name:  "istio/meshconfig"
    kind:  "MeshConfig"
    group: ""

snapshots:
  - name: "default"
    collections:
      - "istio/meshconfig"

resources:
  - kind:         "VirtualService"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
  
transforms:
  - type: foo
    mapping:
      "k8s/networking.istio.io/v1alpha3/destinationrules": "istio/networking/v1alpha3/destinationrules"
`,
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)
			_, err := Parse(c)
			g.Expect(err).NotTo(BeNil())
		})
	}
}

func TestParseErrors_Unmarshal(t *testing.T) {
	input := `
collections:
  - name:  "istio/meshconfig"
    kind:   "VirtualService"
    group:  "networking.istio.io"

snapshots:
  - name: "default"
    collections:
      - "istio/meshconfig"

resources:
  - kind:         "VirtualService"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.mesh.v1alpha1.MeshConfig"
    protoPackage: "istio.io/api/mesh/v1alpha1"
  
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/destinationrules": "istio/networking/v1alpha3/destinationrules"
`

	// This is fragile! It assumes the exact number of calls from the code.
	expectedCalls := 3
	for i := 0; i < expectedCalls; i++ {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			g := NewGomegaWithT(t)

			var cur int
			jsonUnmarshal = func(data []byte, v interface{}) error {
				if cur >= i {
					return fmt.Errorf("err")
				}
				cur++
				return json.Unmarshal(data, v)
			}

			defer func() {
				jsonUnmarshal = json.Unmarshal
			}()

			_, err := Parse(input)
			g.Expect(err).NotTo(BeNil())
		})
	}
}
