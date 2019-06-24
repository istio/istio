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

package meshcfg

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestInMemorySource_Empty(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemory()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	g.Expect(s.IsSynced()).To(BeFalse())

	s.Start()

	g.Consistently(s.IsSynced).Should(BeFalse())
	g.Consistently(acc.Events).Should(HaveLen(0))
}

func TestInMemorySource_SetBeforeStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemory()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Set(Default())
	g.Expect(s.IsSynced()).To(BeTrue())

	s.Start()
	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: IstioMeshconfig,
			Entry: &resource.Entry{
				Metadata: resource.Metadata{
					Name: ResourceName,
				},
				Item: Default(),
			},
		},
		{
			Kind:   event.FullSync,
			Source: IstioMeshconfig,
		},
	}
	g.Eventually(acc.Events).Should(Equal(expected))
}

func TestInMemorySource_SetAfterStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemory()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	s.Set(Default())
	g.Expect(s.IsSynced()).To(BeTrue())

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: IstioMeshconfig,
			Entry: &resource.Entry{
				Metadata: resource.Metadata{
					Name: ResourceName,
				},
				Item: Default(),
			},
		},
		{
			Kind:   event.FullSync,
			Source: IstioMeshconfig,
		},
	}
	g.Eventually(acc.Events).Should(Equal(expected))
}

func TestInMemorySource_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemory()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Set(Default())
	s.Start()
	s.Start()
	g.Expect(s.IsSynced()).To(BeTrue())

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: IstioMeshconfig,
			Entry: &resource.Entry{
				Metadata: resource.Metadata{
					Name: ResourceName,
				},
				Item: Default(),
			},
		},
		{
			Kind:   event.FullSync,
			Source: IstioMeshconfig,
		},
	}
	g.Eventually(acc.Events).Should(Equal(expected))
}

func TestInMemorySource_StartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemory()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	s.Set(Default())
	s.Stop()
	acc.Clear()

	s.Start()
	g.Expect(s.IsSynced()).To(BeTrue())

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: IstioMeshconfig,
			Entry: &resource.Entry{
				Metadata: resource.Metadata{
					Name: ResourceName,
				},
				Item: Default(),
			},
		},
		{
			Kind:   event.FullSync,
			Source: IstioMeshconfig,
		},
	}
	g.Eventually(acc.Events).Should(Equal(expected))
}

func TestInMemorySource_ResetOnUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewInmemory()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	s.Set(Default())
	m := Default()
	m.IngressClass = "foo"
	s.Set(m)

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: IstioMeshconfig,
			Entry: &resource.Entry{
				Metadata: resource.Metadata{
					Name: ResourceName,
				},
				Item: Default(),
			},
		},
		{
			Kind:   event.FullSync,
			Source: IstioMeshconfig,
		},
		{
			Kind:   event.Reset,
			Source: IstioMeshconfig,
		},
	}
	g.Eventually(acc.Events).Should(Equal(expected))
}
