// Copyright 2019 Istio Authors
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

	"istio.io/istio/galley/pkg/config/meta/schema/ast"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

func TestSchema_ParseAndBuild(t *testing.T) {
	var cases = []struct {
		Input    string
		Expected *Metadata
	}{
		{
			Input: ``,
			Expected: &Metadata{
				collections: collection.NewSpecsBuilder().Build(),
				snapshots:   map[string]*Snapshot{},
			},
		},
		{
			Input: `
collections:
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"

  - name:         "istio/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"

snapshots:
  - name: "default"
    strategy: debounce
    collections:
      - "istio/networking.istio.io/v1alpha3/virtualservices"


sources:
  - type: kubernetes
    resources:
    - collection:   "k8s/networking.istio.io/v1alpha3/virtualservices"
      kind:         "VirtualService"
      group:        "networking.istio.io"
      version:      "v1alpha3"
  
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`,
			Expected: &Metadata{
				collections: func() collection.Specs {
					b := collection.NewSpecsBuilder()
					b.MustAdd(
						collection.MustNewSpec(
							"k8s/networking.istio.io/v1alpha3/virtualservices",
							"istio.io/api/networking/v1alpha3",
							"istio.networking.v1alpha3.VirtualService"),
					)
					b.MustAdd(
						collection.MustNewSpec(
							"istio/networking.istio.io/v1alpha3/virtualservices",
							"istio.io/api/networking/v1alpha3",
							"istio.networking.v1alpha3.VirtualService"),
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
				sources: []Source{
					&KubeSource{
						resources: []*KubeResource{
							{
								Collection: collection.MustNewSpec(
									"k8s/networking.istio.io/v1alpha3/virtualservices",
									"istio.io/api/networking/v1alpha3",
									"istio.networking.v1alpha3.VirtualService"),
								Version: "v1alpha3",
								Kind:    "VirtualService",
								Group:   "networking.istio.io",
							},
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
			g.Expect(actual).To(Equal(c.Expected))
		})
	}
}

func TestSchema_ParseAndBuild_Error(t *testing.T) {
	var cases = []string{
		`
	$$$
`,

		`
collections:
  - name:         "$$$"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`,
		`
collections:
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
`,

		`
collections:
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
snapshots:
- name: "default"
  strategy: debounce
  collections:
  - "istio/networking.istio.io/v1alpha3/virtualservices"
`,
		`
collections:
sources:
  - type: kubernetes
    resources:
    - collection:   "k8s/networking.istio.io/v1alpha3/virtualservices"
      kind:         "VirtualService"
      group:        "networking.istio.io"
      version:      "v1alpha3"
`,
		`
collections:
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`,
		`
collections:
  - name:         "istio/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`,
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			_, err := ParseAndBuild(c)
			g.Expect(err).NotTo(BeNil())
		})
	}
}

var input = `
collections:
  - name:         "k8s/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"

  - name:         "istio/networking.istio.io/v1alpha3/virtualservices"
    proto:        "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"

snapshots:
  - name: "default"
    strategy: debounce
    collections:
      - "istio/networking.istio.io/v1alpha3/virtualservices"


sources:
  - type: kubernetes
    resources:
    - collection:   "k8s/networking.istio.io/v1alpha3/virtualservices"
      kind:         "VirtualService"
      group:        "networking.istio.io"
      version:      "v1alpha3"
  
transforms:
  - type: direct
    mapping:
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking.istio.io/v1alpha3/virtualservices"
`

func TestSchemaBasic(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := ParseAndBuild(input)
	g.Expect(err).To(BeNil())

	b := collection.NewSpecsBuilder()
	b.MustAdd(collection.MustNewSpec("k8s/networking.istio.io/v1alpha3/virtualservices",
		"istio.io/api/networking/v1alpha3",
		"istio.networking.v1alpha3.VirtualService"))
	b.MustAdd(collection.MustNewSpec("istio/networking.istio.io/v1alpha3/virtualservices",
		"istio.io/api/networking/v1alpha3",
		"istio.networking.v1alpha3.VirtualService"))
	g.Expect(s.AllCollections()).To(Equal(b.Build()))
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

	g.Expect(s.AllSources()).To(HaveLen(1))
	g.Expect(s.AllSources()[0]).To(Equal(
		&KubeSource{
			resources: []*KubeResource{
				{
					Collection: collection.MustNewSpec("k8s/networking.istio.io/v1alpha3/virtualservices",
						"istio.io/api/networking/v1alpha3",
						"istio.networking.v1alpha3.VirtualService"),
					Group:   "networking.istio.io",
					Version: "v1alpha3",
					Kind:    "VirtualService",
				},
			},
		}))

	g.Expect(s.AllSnapshots()).To(HaveLen(1))
	g.Expect(s.AllSnapshots()[0]).To(Equal(
		&Snapshot{
			Name:        "default",
			Strategy:    "debounce",
			Collections: []collection.Name{collection.NewName("istio/networking.istio.io/v1alpha3/virtualservices")},
		}))

	g.Expect(s.KubeSource()).To(Equal(&KubeSource{
		resources: []*KubeResource{
			{
				Collection: collection.MustNewSpec("k8s/networking.istio.io/v1alpha3/virtualservices",
					"istio.io/api/networking/v1alpha3",
					"istio.networking.v1alpha3.VirtualService"),
				Group:   "networking.istio.io",
				Version: "v1alpha3",
				Kind:    "VirtualService",
			},
		},
	}))

	g.Expect(s.KubeSource().Resources()).To(Equal(KubeResources{
		{
			Collection: collection.MustNewSpec("k8s/networking.istio.io/v1alpha3/virtualservices",
				"istio.io/api/networking/v1alpha3",
				"istio.networking.v1alpha3.VirtualService"),
			Group:   "networking.istio.io",
			Version: "v1alpha3",
			Kind:    "VirtualService",
		},
	}))

	g.Expect(s.KubeSource().Resources().Collections()).To(Equal([]collection.Name{
		collection.NewName("k8s/networking.istio.io/v1alpha3/virtualservices"),
	}))

	g.Expect(s.KubeSource().Resources()[0].CanonicalResourceName()).To(Equal("networking.istio.io/v1alpha3/VirtualService"))
}

func TestSchema_Find(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := ParseAndBuild(input)
	g.Expect(err).To(BeNil())

	k, b := s.KubeSource().Resources().Find("networking.istio.io", "VirtualService")
	g.Expect(b).To(BeTrue())
	g.Expect(k).To(Equal(KubeResource{
		Collection: collection.MustNewSpec("k8s/networking.istio.io/v1alpha3/virtualservices",
			"istio.io/api/networking/v1alpha3",
			"istio.networking.v1alpha3.VirtualService"),
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "VirtualService",
	}))

	_, b = s.KubeSource().Resources().Find("foo", "bar")
	g.Expect(b).To(BeFalse())
}

func TestSchema_MustFind(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()

	s, err := ParseAndBuild(input)
	g.Expect(err).To(BeNil())

	k := s.KubeSource().Resources().MustFind("networking.istio.io", "VirtualService")
	g.Expect(k).To(Equal(KubeResource{
		Collection: collection.MustNewSpec("k8s/networking.istio.io/v1alpha3/virtualservices",
			"istio.io/api/networking/v1alpha3",
			"istio.networking.v1alpha3.VirtualService"),
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    "VirtualService",
	}))
}

func TestSchema_MustFind_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	s, err := ParseAndBuild(input)
	g.Expect(err).To(BeNil())

	_ = s.KubeSource().Resources().MustFind("foo.istio.io", "bar")
}

func TestSchema_KubeResource_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	s, err := ParseAndBuild(``)
	g.Expect(err).To(BeNil())

	_ = s.KubeSource()
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

func TestBuild_UnknownSource(t *testing.T) {
	g := NewGomegaWithT(t)

	a := &ast.Metadata{
		Sources: []ast.Source{
			&struct{}{},
		},
	}

	_, err := Build(a)
	g.Expect(err).NotTo(BeNil())
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
