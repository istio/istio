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

package conformance

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/test/conformance/constraint"
)

const (
	metadata = `skip: true
exclusive: true
labels:
  - a
  - b
  - c
environments:
  - kube
  - native
`
	input = `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: tcp-echo-destination
spec:
  host: tcp-echo
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
`
	mcp = `constraints:
- collection: foo
  check:
  - exactlyOne:
    - select: foo
      exists: true
`
	meshcfg = `ingressClass: foo
`
)

func TestBasic_NoStages(t *testing.T) {
	g := NewGomegaWithT(t)

	base, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	d := path.Join(base, "basic")
	err = os.Mkdir(d, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeMetadata(g, d)
	writeStageFiles(g, d)

	m, err := Load(base)
	g.Expect(err).To(BeNil())

	msh := meshcfg
	expected := []*Test{
		{
			Metadata: &Metadata{
				Name:         "basic",
				Skip:         true,
				Exclusive:    true,
				Labels:       []string{"a", "b", "c"},
				Environments: []string{"kube", "native"},
			},
			Stages: []*Stage{
				{
					Input:      input,
					MeshConfig: &msh,
					MCP: &constraint.Constraints{
						Constraints: []*constraint.Collection{
							{
								Name: "foo",
								Check: []constraint.Range{
									&constraint.ExactlyOne{
										Constraints: []constraint.Check{
											&constraint.Select{
												Expression: "foo",
												Op:         constraint.SelectExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	g.Expect(m).To(Equal(expected))
}

func TestBasic_1Stage(t *testing.T) {
	g := NewGomegaWithT(t)

	base, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	d := path.Join(base, "basic")
	err = os.Mkdir(d, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeMetadata(g, d)

	s0 := path.Join(d, "stage0")
	err = os.Mkdir(s0, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeStageFiles(g, s0)

	m, err := Load(base)
	g.Expect(err).To(BeNil())

	msh := meshcfg
	expected := []*Test{
		{
			Metadata: &Metadata{
				Name:         "basic",
				Skip:         true,
				Exclusive:    true,
				Labels:       []string{"a", "b", "c"},
				Environments: []string{"kube", "native"},
			},
			Stages: []*Stage{
				{
					Input:      input,
					MeshConfig: &msh,
					MCP: &constraint.Constraints{
						Constraints: []*constraint.Collection{
							{
								Name: "foo",
								Check: []constraint.Range{
									&constraint.ExactlyOne{
										Constraints: []constraint.Check{
											&constraint.Select{
												Expression: "foo",
												Op:         constraint.SelectExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	g.Expect(m).To(Equal(expected))
}

func TestBasic_2Stage(t *testing.T) {
	g := NewGomegaWithT(t)

	base, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	d := path.Join(base, "basic")
	err = os.Mkdir(d, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeMetadata(g, d)

	s0 := path.Join(d, "stage0")
	err = os.Mkdir(s0, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeStageFiles(g, s0)

	s1 := path.Join(d, "stage1")
	err = os.Mkdir(s1, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeStageFiles(g, s1)

	m, err := Load(base)
	g.Expect(err).To(BeNil())

	msh := meshcfg
	expected := []*Test{
		{
			Metadata: &Metadata{
				Name:         "basic",
				Skip:         true,
				Exclusive:    true,
				Labels:       []string{"a", "b", "c"},
				Environments: []string{"kube", "native"},
			},
			Stages: []*Stage{
				{
					Input:      input,
					MeshConfig: &msh,
					MCP: &constraint.Constraints{
						Constraints: []*constraint.Collection{
							{
								Name: "foo",
								Check: []constraint.Range{
									&constraint.ExactlyOne{
										Constraints: []constraint.Check{
											&constraint.Select{
												Expression: "foo",
												Op:         constraint.SelectExists,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Input:      input,
					MeshConfig: &msh,
					MCP: &constraint.Constraints{
						Constraints: []*constraint.Collection{
							{
								Name: "foo",
								Check: []constraint.Range{
									&constraint.ExactlyOne{
										Constraints: []constraint.Check{
											&constraint.Select{
												Expression: "foo",
												Op:         constraint.SelectExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	g.Expect(m).To(Equal(expected))
}

func TestBasic_SpuriousFolder(t *testing.T) {
	g := NewGomegaWithT(t)

	base, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	d := path.Join(base, "basic")
	err = os.Mkdir(d, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeMetadata(g, d)

	s0 := path.Join(d, "stage0")
	err = os.Mkdir(s0, os.ModePerm)
	g.Expect(err).To(BeNil())

	writeStageFiles(g, s0)

	s1 := path.Join(d, "foo")
	err = os.Mkdir(s1, os.ModePerm)
	g.Expect(err).To(BeNil())

	_, err = Load(base)
	g.Expect(err).NotTo(BeNil())
}

func writeMetadata(g *GomegaWithT, d string) {
	err := ioutil.WriteFile(path.Join(d, MetadataFileName), []byte(metadata), os.ModePerm)
	g.Expect(err).To(BeNil())
}

func writeStageFiles(g *GomegaWithT, d string) {
	err := ioutil.WriteFile(path.Join(d, InputFileName), []byte(input), os.ModePerm)
	g.Expect(err).To(BeNil())

	err = ioutil.WriteFile(path.Join(d, MCPFileName), []byte(mcp), os.ModePerm)
	g.Expect(err).To(BeNil())

	err = ioutil.WriteFile(path.Join(d, MeshConfigFileName), []byte(meshcfg), os.ModePerm)
	g.Expect(err).To(BeNil())
}
