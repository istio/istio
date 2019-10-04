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

package event

import (
	"strings"
	"testing"

	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"

	. "github.com/onsi/gomega"

	"github.com/gogo/protobuf/types"
)

func TestEvent_String(t *testing.T) {
	e := resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("ns1", "rs1"),
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	tests := []struct {
		i   Event
		exp string
	}{
		{
			i:   Event{},
			exp: "[Event](None)",
		},
		{
			i:   Event{Kind: Added, Entry: &e},
			exp: "[Event](Added: /ns1/rs1)",
		},
		{
			i:   Event{Kind: Updated, Entry: &e},
			exp: "[Event](Updated: /ns1/rs1)",
		},
		{
			i:   Event{Kind: Deleted, Entry: &e},
			exp: "[Event](Deleted: /ns1/rs1)",
		},
		{
			i:   Event{Kind: FullSync, Source: collection.NewName("foo")},
			exp: "[Event](FullSync: foo)",
		},
		{
			i:   Event{Kind: Kind(99), Source: collection.NewName("foo")},
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
	e := resource.Entry{
		Metadata: resource.Metadata{
			Name:    resource.NewName("ns1", "rs1"),
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	tests := []struct {
		i      Event
		prefix string
	}{
		{
			i:      Event{},
			prefix: "[Event](None",
		},
		{
			i:      Event{Kind: Added, Entry: &e},
			prefix: "[Event](Added: /ns1/rs1",
		},
		{
			i:      Event{Kind: Updated, Entry: &e},
			prefix: "[Event](Updated: /ns1/rs1",
		},
		{
			i:      Event{Kind: Deleted, Entry: &e},
			prefix: "[Event](Deleted: /ns1/rs1",
		},
		{
			i:      Event{Kind: FullSync, Source: collection.NewName("foo")},
			prefix: "[Event](FullSync: foo",
		},
		{
			i:      Event{Kind: Kind(99), Source: collection.NewName("foo")},
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

	r := resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	e := Event{Kind: Added, Source: collection.NewName("boo"), Entry: &r}

	g.Expect(e.Clone()).To(Equal(e))
}

func TestEvent_FullSyncFor(t *testing.T) {
	g := NewGomegaWithT(t)

	e := FullSyncFor(collection.NewName("boo"))

	expected := Event{
		Kind:   FullSync,
		Source: collection.NewName("boo"),
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_AddFor(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	e := AddFor(collection.NewName("boo"), &r)

	expected := Event{
		Kind:   Added,
		Source: collection.NewName("boo"),
		Entry:  &r,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_UpdateFor(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	e := UpdateFor(collection.NewName("boo"), &r)

	expected := Event{
		Kind:   Updated,
		Source: collection.NewName("boo"),
		Entry:  &r,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_DeleteFor(t *testing.T) {
	g := NewGomegaWithT(t)

	n := resource.NewName("ns1", "rs1")
	v := resource.Version("v1")
	e := DeleteFor(collection.NewName("boo"), n, v)

	expected := Event{
		Kind:   Deleted,
		Source: collection.NewName("boo"),
		Entry: &resource.Entry{
			Metadata: resource.Metadata{
				Name:    n,
				Version: v,
			},
		},
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_UpdateForResource(t *testing.T) {
	g := NewGomegaWithT(t)

	r := resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("ns1", "rs1"),
			Labels: map[string]string{
				"foo": "bar",
			},
			Version: "v1",
		},
		Item: &types.Empty{},
	}

	e := DeleteForResource(collection.NewName("boo"), &r)

	expected := Event{
		Kind:   Deleted,
		Source: collection.NewName("boo"),
		Entry:  &r,
	}
	g.Expect(e).To(Equal(expected))
}

func TestEvent_IsSource(t *testing.T) {
	g := NewGomegaWithT(t)
	e := Event{
		Kind:   Deleted,
		Source: collection.NewName("boo"),
	}
	g.Expect(e.IsSource(collection.NewName("boo"))).To(BeTrue())
	g.Expect(e.IsSource(collection.NewName("noo"))).To(BeFalse())
}

func TestEvent_IsSourceAny(t *testing.T) {
	g := NewGomegaWithT(t)
	e := Event{
		Kind:   Deleted,
		Source: collection.NewName("boo"),
	}
	g.Expect(e.IsSourceAny(collection.NewName("foo"))).To(BeFalse())
	g.Expect(e.IsSourceAny(collection.NewName("boo"))).To(BeTrue())
	g.Expect(e.IsSourceAny(collection.NewName("boo"), collection.NewName("foo"))).To(BeTrue())
}

func TestEvent_WithSource(t *testing.T) {
	g := NewGomegaWithT(t)
	oldCol := collection.NewName("boo")
	e := Event{
		Kind:   Deleted,
		Source: oldCol,
	}
	newCol := collection.NewName("far")
	a := e.WithSource(newCol)
	g.Expect(a.Source).To(Equal(newCol))
	g.Expect(e.Source).To(Equal(oldCol))
}

func TestEvent_WithSource_Reset(t *testing.T) {
	g := NewGomegaWithT(t)
	e := Event{
		Kind: Reset,
	}
	newCol := collection.NewName("far")
	a := e.WithSource(newCol)
	g.Expect(a.Source).To(Equal(collection.EmptyName))
	g.Expect(e.Source).To(Equal(collection.EmptyName))
}
