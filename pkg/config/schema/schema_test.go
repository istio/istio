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

package schema

import (
	"testing"

	. "github.com/onsi/gomega"

	fixtures "istio.io/istio/pkg/config/legacy/testing/fixtures"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

var virtualServiceResource = collections.IstioNetworkingV1Alpha3Virtualservices.Resource()

func TestSchema_ParseAndBuild(t *testing.T) {
	cases := []struct {
		Input    string
		Expected *Metadata
	}{
		{
			Input: ``,
			Expected: &Metadata{
				collections:     collection.NewSchemasBuilder().Build(),
				kubeCollections: collection.NewSchemasBuilder().Build(),
			},
		},
		{
			Input: `
collections:
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"

  - name:         "istio/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"

resources:
  - kind:         "VirtualService"
    plural:       "virtualservices"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`,
			Expected: &Metadata{
				collections: func() collection.Schemas {
					b := collection.NewSchemasBuilder()
					b.MustAdd(
						collection.Builder{
							Name:     "k8s/networking.istio.io/v1alpha3/virtualservices",
							Resource: virtualServiceResource,
						}.MustBuild(),
					)
					b.MustAdd(
						collection.Builder{
							Name:     "istio/networking.istio.io/v1alpha3/virtualservices",
							Resource: virtualServiceResource,
						}.MustBuild(),
					)
					return b.Build()
				}(),
				kubeCollections: func() collection.Schemas {
					b := collection.NewSchemasBuilder()
					b.MustAdd(
						collection.Builder{
							Name:     "k8s/networking.istio.io/v1alpha3/virtualservices",
							Resource: virtualServiceResource,
						}.MustBuild(),
					)
					return b.Build()
				}(),
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewWithT(t)

			actual, err := ParseAndBuild(c.Input)
			g.Expect(err).To(BeNil())

			fixtures.ExpectEqual(t, actual, c.Expected)
		})
	}
}

func TestSchema_ParseAndBuild_Error(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "invalid yaml",
			expected: "error converting YAML to JSON",
			input: `
	$$$
`,
		},
		{
			name:     "invalid collection name",
			expected: "invalid collection name",
			input: `
collections:
  - name:  "$$$"
    kind:         "VirtualService"
    group:        "networking.istio.io"
resources:
  - kind:         "VirtualService"
    plural:       "virtualservices"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`,
		},
		{
			name:     "duplicate collection",
			expected: "collection already exists",
			input: `
collections:
  - name:  "k8s/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"
  - name:  "k8s/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"
resources:
  - kind:         "VirtualService"
    plural:       "virtualservices"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`,
		},
		{
			name:     "collection missing resource",
			expected: "failed locating resource (networking.istio.io/VirtualService)",
			input: `
collections:
  - name:  "k8s/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"
`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)

			_, err := ParseAndBuild(c.input)
			g.Expect(err).ToNot(BeNil())
			g.Expect(err.Error()).To(ContainSubstring(c.expected))
		})
	}
}

var input = `
collections:
  - name:  "k8s/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"

  - name:  "istio/networking.istio.io/v1alpha3/virtualservices"
    kind:         "VirtualService"
    group:        "networking.istio.io"


resources:
  - kind:         "VirtualService"
    plural:       "virtualservices"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`

func TestSchemaBasic(t *testing.T) {
	g := NewWithT(t)

	s, err := ParseAndBuild(input)
	g.Expect(err).To(BeNil())

	b := collection.NewSchemasBuilder()
	b.MustAdd(collection.Builder{
		Name:     "k8s/networking.istio.io/v1alpha3/virtualservices",
		Resource: virtualServiceResource,
	}.MustBuild())
	b.MustAdd(collection.Builder{
		Name:     "istio/networking.istio.io/v1alpha3/virtualservices",
		Resource: virtualServiceResource,
	}.MustBuild())
	fixtures.ExpectEqual(t, s.AllCollections(), b.Build())

	fixtures.ExpectEqual(t, s.KubeCollections().All(), []collection.Schema{
		collection.Builder{
			Name:     "k8s/networking.istio.io/v1alpha3/virtualservices",
			Resource: virtualServiceResource,
		}.MustBuild(),
	})

	fixtures.ExpectEqual(t, s.KubeCollections().CollectionNames(), collection.Names{
		collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"),
	})

	g.Expect(s.KubeCollections().All()[0].Resource().GroupVersionKind().String()).To(Equal("networking.istio.io/v1alpha3/VirtualService"))
}
