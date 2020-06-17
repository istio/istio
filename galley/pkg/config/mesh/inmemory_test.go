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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestInMemorySource_Empty(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemoryMeshCfg()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	g.Expect(s.IsSynced()).To(BeFalse())

	s.Start()

	g.Consistently(s.IsSynced).Should(BeFalse())
	g.Consistently(acc.Events).Should(HaveLen(0))
}

func TestInMemorySource_SetBeforeStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemoryMeshCfg()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Set(DefaultMeshConfig())
	g.Expect(s.IsSynced()).To(BeTrue())

	s.Start()
	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: MeshConfigResourceName,
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

func TestInMemorySource_SetAfterStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemoryMeshCfg()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	s.Set(DefaultMeshConfig())
	g.Expect(s.IsSynced()).To(BeTrue())

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: MeshConfigResourceName,
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

func TestInMemorySource_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemoryMeshCfg()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Set(DefaultMeshConfig())
	s.Start()
	s.Start()
	g.Expect(s.IsSynced()).To(BeTrue())

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: MeshConfigResourceName,
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

func TestInMemorySource_StartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemoryMeshCfg()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	s.Set(DefaultMeshConfig())
	s.Stop()
	acc.Clear()

	s.Start()
	g.Expect(s.IsSynced()).To(BeTrue())

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: MeshConfigResourceName,
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

func TestInMemorySource_ResetOnUpdate(t *testing.T) {
	s := NewInmemoryMeshCfg()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	s.Set(DefaultMeshConfig())
	m := DefaultMeshConfig()
	m.IngressClass = "foo"
	s.Set(m)

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					FullName: MeshConfigResourceName,
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
		{
			Kind:   event.Reset,
			Source: collections.IstioMeshV1Alpha1MeshConfig,
		},
	}
	fixtures.ExpectEventsEventually(t, acc, expected...)
}
