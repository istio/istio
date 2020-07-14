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

package event_test

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"

	"github.com/gogo/protobuf/types"
)

func TestEvent_String(t *testing.T) {
	e := resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("ns1", "rs1"),
			Version:  "v1",
		},
		Message: &types.Empty{},
	}

	tests := []struct {
		i   event.Event
		exp string
	}{
		{
			i:   event.Event{},
			exp: "[Event](None)",
		},
		{
			i:   event.Event{Kind: event.Added, Resource: &e},
			exp: "[Event](Added: /ns1/rs1)",
		},
		{
			i:   event.Event{Kind: event.Updated, Resource: &e},
			exp: "[Event](Updated: /ns1/rs1)",
		},
		{
			i:   event.Event{Kind: event.Deleted, Resource: &e},
			exp: "[Event](Deleted: /ns1/rs1)",
		},
		{
			i:   event.Event{Kind: event.FullSync, Source: data.Foo},
			exp: "[Event](FullSync: foo)",
		},
		{
			i:   event.Event{Kind: event.Kind(99), Source: data.Foo},
			exp: "[Event](<<Unknown Kind 99>>)",
		},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)
			actual := tc.i.String()
			g.Expect(strings.TrimSpace(actual)).To(Equal(strings.TrimSpace(tc.exp)))
		})
	}
}

func TestEvent_DetailedString(t *testing.T) {
	e := resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("ns1", "rs1"),
			Version:  "v1",
		},
		Message: &types.Empty{},
	}

	tests := []struct {
		i      event.Event
		prefix string
	}{
		{
			i:      event.Event{},
			prefix: "[Event](None",
		},
		{
			i:      event.Event{Kind: event.Added, Resource: &e},
			prefix: "[Event](Added: /ns1/rs1",
		},
		{
			i:      event.Event{Kind: event.Updated, Resource: &e},
			prefix: "[Event](Updated: /ns1/rs1",
		},
		{
			i:      event.Event{Kind: event.Deleted, Resource: &e},
			prefix: "[Event](Deleted: /ns1/rs1",
		},
		{
			i:      event.Event{Kind: event.FullSync, Source: data.Foo},
			prefix: "[Event](FullSync: foo",
		},
		{
			i:      event.Event{Kind: event.Kind(99), Source: data.Foo},
			prefix: "[Event](<<Unknown Kind 99>>",
		},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)
			actual := tc.i.String()
			actual = strings.TrimSpace(actual)
			expected := strings.TrimSpace(tc.prefix)
			g.Expect(actual).To(HavePrefix(expected))
		})
	}
}

func TestEvent_Clone(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Message: &types.Empty{},
	}

	e := event.Event{Kind: event.Added, Source: data.Boo, Resource: &r}

	g.Expect(e.Clone()).To(Equal(e))
}

func TestEvent_FullSyncFor(t *testing.T) {
	g := NewGomegaWithT(t)

	e := event.FullSyncFor(data.Boo)

	expected := event.Event{
		Kind:   event.FullSync,
		Source: data.Boo,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_AddFor(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Message: &types.Empty{},
	}

	e := event.AddFor(data.Boo, &r)

	expected := event.Event{
		Kind:     event.Added,
		Source:   data.Boo,
		Resource: &r,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_UpdateFor(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Message: &types.Empty{},
	}

	e := event.UpdateFor(data.Boo, &r)

	expected := event.Event{
		Kind:     event.Updated,
		Source:   data.Boo,
		Resource: &r,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_DeleteFor(t *testing.T) {
	g := NewGomegaWithT(t)

	n := resource.NewFullName("ns1", "rs1")
	v := resource.Version("v1")
	e := event.DeleteFor(data.Boo, n, v)

	expected := event.Event{
		Kind:   event.Deleted,
		Source: data.Boo,
		Resource: &resource.Instance{
			Metadata: resource.Metadata{
				FullName: n,
				Version:  v,
			},
		},
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_UpdateForResource(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Message: &types.Empty{},
	}

	e := event.DeleteForResource(data.Boo, &r)

	expected := event.Event{
		Kind:     event.Deleted,
		Source:   data.Boo,
		Resource: &r,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_IsSource(t *testing.T) {
	g := NewGomegaWithT(t)
	e := event.Event{
		Kind:   event.Deleted,
		Source: data.Boo,
	}
	g.Expect(e.IsSource(data.Boo.Name())).To(BeTrue())
	g.Expect(e.IsSource(collection.NewName("noo"))).To(BeFalse())
}

func TestEvent_IsSourceAny(t *testing.T) {
	g := NewGomegaWithT(t)
	e := event.Event{
		Kind:   event.Deleted,
		Source: data.Boo,
	}
	g.Expect(e.IsSourceAny(data.Foo.Name())).To(BeFalse())
	g.Expect(e.IsSourceAny(data.Boo.Name())).To(BeTrue())
	g.Expect(e.IsSourceAny(data.Boo.Name(), data.Foo.Name())).To(BeTrue())
}

func TestEvent_WithSource(t *testing.T) {
	g := NewGomegaWithT(t)
	oldCol := data.Boo
	e := event.Event{
		Kind:   event.Deleted,
		Source: oldCol,
	}
	newCol := data.Foo
	a := e.WithSource(newCol)
	g.Expect(a.Source.Name()).To(Equal(newCol.Name()))
	g.Expect(e.Source.Name()).To(Equal(oldCol.Name()))
}

func TestEvent_WithSource_Reset(t *testing.T) {
	g := NewGomegaWithT(t)
	e := event.Event{
		Kind: event.Reset,
	}
	newCol := data.Foo
	a := e.WithSource(newCol)
	g.Expect(a.Source).To(BeNil())
	g.Expect(e.Source).To(BeNil())
}
