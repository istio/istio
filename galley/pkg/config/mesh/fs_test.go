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

package mesh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	. "github.com/onsi/gomega"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestFsSource_NoInitialFile(t *testing.T) {
	g := NewGomegaWithT(t)

	file := setupDir(t, nil)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: DefaultMeshConfig(),
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)
}

func TestFsSource_NoInitialFile_UpdateAfterStart(t *testing.T) {
	g := NewGomegaWithT(t)

	file := setupDir(t, nil)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: DefaultMeshConfig(),
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)

	acc.Clear()
	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	writeMeshCfg(t, file, mcfg)

	expected = []event.Event{
		{
			Kind:   event.Reset,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected[0])
}

func TestFsSource_InitialFile_UpdateAfterStart(t *testing.T) {
	g := NewGomegaWithT(t)

	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	file := setupDir(t, mcfg)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: mcfg,
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)

	acc.Clear()
	mcfg2 := DefaultMeshConfig()
	mcfg2.IngressClass = "bar"
	writeMeshCfg(t, file, mcfg2)

	expected = []event.Event{
		{
			Kind:   event.Reset,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected[0])
}

func TestFsSource_InitialFile(t *testing.T) {
	g := NewGomegaWithT(t)

	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	file := setupDir(t, mcfg)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: mcfg,
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)
}

func TestFsSource_StartStopStart(t *testing.T) {
	g := NewGomegaWithT(t)

	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	file := setupDir(t, mcfg)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()
	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: mcfg,
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)

	acc.Clear()
	fs.Stop()
	g.Consistently(acc.Events()).Should(HaveLen(0))

	fs.Start()
	fixtures.ExpectEventsEventually(t, acc, expected...)
}

func TestFsSource_FileRemoved_NoChange(t *testing.T) {
	g := NewGomegaWithT(t)

	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	file := setupDir(t, mcfg)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()
	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: mcfg,
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)
	acc.Clear()

	err = os.Remove(file)
	g.Expect(err).To(BeNil())
	time.Sleep(time.Millisecond * 100)
	g.Consistently(acc.Events()).Should(HaveLen(0))
}

func TestFsSource_BogusFile_NoChange(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/15987")
	g := NewGomegaWithT(t)

	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	file := setupDir(t, mcfg)

	fs, err := NewMeshConfigFS(file)
	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()
	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: mcfg,
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)
	acc.Clear()

	err = ioutil.WriteFile(file, []byte(":@#Hallo!"), os.ModePerm)
	g.Expect(err).To(BeNil())

	time.Sleep(time.Millisecond * 100)
	g.Consistently(acc.Events).Should(HaveLen(0))
}

func setupDir(t *testing.T, m *v1alpha1.MeshConfig) string {
	g := NewGomegaWithT(t)

	p, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	file := path.Join(p, "meshconfig.yaml")

	if m != nil {
		writeMeshCfg(t, file, m)
	}

	return file
}

func writeMeshCfg(t *testing.T, file string, m *v1alpha1.MeshConfig) { // nolint:interfacer
	g := NewGomegaWithT(t)
	s, err := (&jsonpb.Marshaler{Indent: "  "}).MarshalToString(m)
	g.Expect(err).To(BeNil())
	err = ioutil.WriteFile(file, []byte(s), os.ModePerm)
	g.Expect(err).To(BeNil())
}

func TestFsSource_InvalidPath(t *testing.T) {
	g := NewGomegaWithT(t)

	file := setupDir(t, nil)
	file = path.Join(file, "bogus")

	_, err := NewMeshConfigFS(file)
	g.Expect(err).NotTo(BeNil())
}

func TestFsSource_YamlToJSONError(t *testing.T) {
	g := NewGomegaWithT(t)

	mcfg := DefaultMeshConfig()
	mcfg.IngressClass = "foo"
	file := setupDir(t, mcfg)

	fs, err := newMeshConfigFS(file, func([]byte) ([]byte, error) {
		return nil, fmt.Errorf("horror")
	})

	g.Expect(err).To(BeNil())
	defer func() {
		err = fs.Close()
		g.Expect(err).To(BeNil())
	}()
	acc := &fixtures.Accumulator{}
	fs.Dispatch(acc)

	fs.Start()

	// Expect default config
	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: resource.NewFullName("istio-system", "meshconfig"),
					Schema:   collections.IstioMeshV1Alpha1MeshConfig.Resource(),
				},
				Message: DefaultMeshConfig(),
				Origin: &rt.Origin{
					Collection: collections.IstioMeshV1Alpha1MeshConfig.Name(),
					Kind:       "MeshConfig",
					FullName:   resource.NewFullName("istio-system", "meshconfig"),
				},
			},
		},
		{
			Kind:   event.FullSync,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)
}
