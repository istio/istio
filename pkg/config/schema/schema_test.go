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

	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	virtualServiceResource = collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
)

func TestSchema_ParseAndBuild(t *testing.T) {
	var cases = []struct {
		Input    string
		Expected *Metadata
	}{
		{
			Input: ``,
			Expected: &Metadata{
				collections:     collection.NewSchemasBuilder().Build(),
				kubeCollections: collection.NewSchemasBuilder().Build(),
				snapshots:       map[string]*Snapshot{},
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

snapshots:
  - name: "default"
    strategy: debounce
    collections:
      - "istio/networking.istio.io/v1alpha3/virtualservices"

resources:
  - kind:         "VirtualService"
    plural:       "virtualservices"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
  
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
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
				snapshots: map[string]*Snapshot{
					"default": {
						Name:     "default",
						Strategy: "debounce",
						Collections: []collection.Name{
							collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices"),
						},
					},
				},
				transformSettings: []TransformSettings{
					&DirectTransformSettings{
						mapping: map[collection.Name]collection.Name{
							collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"): collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices"),
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			actual, err := ParseAndBuild(c.Input)
			g.Expect(err).To(BeNil())

			fixtures.ExpectEqual(t, actual, c.Expected)
		})
	}
}

func TestSchema_ParseAndBuild_Error(t *testing.T) {
	var cases = []struct {
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
			name:     "snapshot missing collection",
			expected: "collection not found",
			input: `
collections:
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
snapshots:
- name: "default"
  strategy: debounce
  collections:
  - "istio/networking.istio.io/v1alpha3/virtualservices"
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
		{
			name:     "transform missing input collection",
			expected: "collection not found",
			input: `
collections:
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
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`,
		},
		{
			name:     "transform missing output collection",
			expected: "collection not found",
			input: `
collections:
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
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

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

snapshots:
  - name: "default"
    strategy: debounce
    collections:
      - "istio/networking.istio.io/v1alpha3/virtualservices"

resources:
  - kind:         "VirtualService"
    plural:       "virtualservices"
    group:        "networking.istio.io"
    version:      "v1alpha3"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
  
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`

func TestSchemaBasic(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := ParseAndBuild(input)
	g.Expect(err).To(BeNil())

	b := collection.NewSchemasBuilder()
	b.MustAdd(collection.Builder{
		Name:     "k8s/networking.istio.io/v1alpha3/virtualservices",
		Resource: virtualServiceResource,
	}.MustBuild())
	b.MustAdd(collection.Builder{Name: "istio/networking.istio.io/v1alpha3/virtualservices",
		Resource: virtualServiceResource,
	}.MustBuild())
	fixtures.ExpectEqual(t, s.AllCollections(), b.Build())
	g.Expect(s.AllCollectionsInSnapshots([]string{"default"})).To(ConsistOf("istio/networking.istio.io/v1alpha3/virtualservices"))
	g.Expect(func() { s.AllCollectionsInSnapshots([]string{"bogus"}) }).To(Panic())

	g.Expect(s.TransformSettings()).To(HaveLen(1))
	g.Expect(s.TransformSettings()[0]).To(Equal(
		&DirectTransformSettings{
			mapping: map[collection.Name]collection.Name{
				collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"): collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices"),
			},
		}))
	g.Expect(s.DirectTransformSettings()).To(Equal(
		&DirectTransformSettings{
			mapping: map[collection.Name]collection.Name{
				collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"): collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices"),
			},
		}))

	g.Expect(s.DirectTransformSettings().Mapping()).To(Equal(
		map[collection.Name]collection.Name{
			collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"): collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices"),
		}))

	fixtures.ExpectEqual(t, s.KubeCollections().All(), []collection.Schema{
		collection.Builder{
			Name:     "k8s/networking.istio.io/v1alpha3/virtualservices",
			Resource: virtualServiceResource,
		}.MustBuild(),
	})

	g.Expect(s.AllSnapshots()).To(HaveLen(1))
	g.Expect(s.AllSnapshots()[0]).To(Equal(
		&Snapshot{
			Name:        "default",
			Strategy:    "debounce",
			Collections: []collection.Name{collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices")},
		}))

	fixtures.ExpectEqual(t, s.KubeCollections().CollectionNames(), collection.Names{
		collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"),
	})

	g.Expect(s.KubeCollections().All()[0].Resource().GroupVersionKind().String()).To(Equal("networking.istio.io/v1alpha3/VirtualService"))
}

func TestSchema_DirectTransform_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	s, err := ParseAndBuild(``)
	g.Expect(err).To(BeNil())

	_ = s.DirectTransformSettings()
}

func TestBuild_UnknownTransform(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &ast.Metadata{
		TransformSettings: []ast.TransformSettings{
			&unknownXformSettings{},
		},
	}

	_, err := Build(a)
	g.Expect(err).NotTo(BeNil())
}

type unknownXformSettings struct {
}

var _ ast.TransformSettings = &unknownXformSettings{}

func (u *unknownXformSettings) Type() string { return "unknown" }
