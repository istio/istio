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

package inmemory

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"

	"github.com/gogo/protobuf/types"
)

func TestInMemory_Register_Empty(t *testing.T) {
	g := NewGomegaWithT(t)

	i := New(data.CollectionNames[:1])
	h := &fixtures.Accumulator{}
	i.Dispatch(h)
	i.Start()
	defer i.Stop()

	expected := []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_Set_BeforeSync(t *testing.T) {
	g := NewGomegaWithT(t)

	r := &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("ns1", "l1"),
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	i := New(data.CollectionNames[:1])
	i.Get(data.Collection1).Set(r)

	h := &fixtures.Accumulator{}
	i.Dispatch(h)
	i.Start()
	defer i.Stop()

	expected := []event.Event{
		{
			Kind:   event.Added,
			Source: data.Collection1,
			Entry:  r,
		},
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_Set_Add(t *testing.T) {
	g := NewGomegaWithT(t)

	r := &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("ns1", "l1"),
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	i := New(data.CollectionNames[:1])

	h := &fixtures.Accumulator{}
	i.Dispatch(h)
	i.Start()
	defer i.Stop()

	expected := []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))

	i.Get(data.Collection1).Set(r)

	expected = []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
		{
			Kind:   event.Added,
			Source: data.Collection1,
			Entry:  r,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_Set_Update(t *testing.T) {
	g := NewGomegaWithT(t)

	r1 := &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("ns1", "l1"),
			Version: "v1",
		},
		Item: &types.Empty{},
	}
	r2 := &resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("ns1", "l1"),
			Version: "v2",
		},
		Item: &types.Empty{},
	}

	i := New(data.CollectionNames[:1])

	h := &fixtures.Accumulator{}
	i.Dispatch(h)
	i.Start()
	defer i.Stop()

	expected := []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))

	i.Get(data.Collection1).Set(r1)
	i.Get(data.Collection1).Set(r2)

	expected = []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
		{
			Kind:   event.Added,
			Source: data.Collection1,
			Entry:  r1,
		},
		{
			Kind:   event.Updated,
			Source: data.Collection1,
			Entry:  r2,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_Clear_BeforeSync(t *testing.T) {
	g := NewGomegaWithT(t)

	i := New(data.CollectionNames[:1])
	i.Get(data.Collection1).Set(data.EntryN1I1V1)

	h := &fixtures.Accumulator{}
	i.Dispatch(h)

	i.Clear()

	i.Start()
	defer i.Stop()

	expected := []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_Clear_AfterSync(t *testing.T) {
	g := NewGomegaWithT(t)

	i := New(data.CollectionNames[:1])
	i.Get(data.Collection1).Set(data.EntryN1I1V1)

	h := &fixtures.Accumulator{}
	i.Dispatch(h)

	i.Start()
	defer i.Stop()

	i.Clear()

	expected := []event.Event{
		data.Event1Col1AddItem1,
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
		data.Event1Col1DeleteItem1,
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	i := New(data.CollectionNames[:1])
	h := &fixtures.Accumulator{}
	i.Dispatch(h)
	i.Start()
	i.Start()
	defer i.Stop()

	expected := []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))
}

func TestInMemory_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	i := New(data.CollectionNames[:1])
	h := &fixtures.Accumulator{}
	i.Dispatch(h)
	i.Start()

	expected := []event.Event{
		{
			Kind:   event.FullSync,
			Source: data.Collection1,
		},
	}

	g.Expect(h.Events()).To(Equal(expected))

	i.Stop()
	i.Stop()
}
