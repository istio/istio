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

package event

import (
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	gatewaySchema = collections.IstioNetworkingV1Alpha3Gateways
	domainSuffix  = "test.com"
	fakeTime      = time.Now()

	gateway = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   443,
					Name:     "https",
					Protocol: "HTTP",
				},
				Hosts: []string{"*.secure.example.com"},
			},
		},
	}

	gateway2 = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   9090,
					Name:     "https",
					Protocol: "HTTP",
				},
				Hosts: []string{"*.secure.example.com"},
			},
		},
	}
)

func TestEmptyList(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(BeEmpty())
}

func TestHasSynced(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	g.Expect(store.HasSynced()).To(BeFalse())
	store.Handle(event.FullSyncFor(gatewaySchema))
	g.Expect(store.HasSynced()).To(BeTrue())
}

func TestReset(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	// Add the resource and make sure it's in the list.
	e := eventFor(event.Added, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e)
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e.Resource)))

	// Now reset and make sure that the list is empty.
	store.Handle(event.Event{Kind: event.Reset})
	out, err = store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(BeEmpty())
}

func TestAddInvalidProto(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	// Add an invalid (empty) Gateway.
	e := eventFor(event.Added, gatewaySchema, &networking.Gateway{}, "default/mygateway", "v0")
	store.Handle(e)

	// Ensure that it was not added.
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(BeEmpty())
	handler.expectNoEntries(t)
}

func TestDuplicateAddsForSameVersion(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	e := eventFor(event.Added, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e)
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e.Resource)))
	handler.expectEntry(t, eventEntryFor(e, nil))

	// Add a second (different) resource for the same version and verify that
	// no event was fired and the resource is unchanged.
	store.Handle(eventFor(event.Added, gatewaySchema, gateway2, "default/mygateway", "v0"))
	out, err = store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e.Resource)))
	handler.expectNoEntries(t)
}

func TestDuplicateAddsForDifferentVersions(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	e1 := eventFor(event.Added, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e1)
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e1.Resource)))
	expected := eventEntryFor(e1, nil)
	handler.expectEntry(t, expected)
	prev := expected.Config

	// Add a second (different) resource for a different version and verify that
	// no event was fired and the resource is unchanged.
	e2 := eventFor(event.Added, gatewaySchema, gateway2, "default/mygateway", "v1")
	store.Handle(e2)
	out, err = store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e2.Resource)))
	expectedEntry := eventEntryFor(e2, prev)
	// Make sure that the kind is properly set to "Updated", even though the original event was "Added".
	expectedEntry.Kind = event.Updated
	handler.expectEntry(t, expectedEntry)
}

func TestAddInDifferentNamespaces(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	e1 := eventFor(event.Added, gatewaySchema, gateway, "ns1/mygateway1", "v0")
	e2 := eventFor(event.Added, gatewaySchema, gateway, "ns2/mygateway2", "v0")
	e3 := eventFor(event.Added, gatewaySchema, gateway, "ns1/mygateway3", "v0")
	store.Handle(e1)
	store.Handle(e2)
	store.Handle(e3)

	// Default namespace should be empty.
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(BeEmpty())

	// ns1 should have gateways 1 and 3
	out, err = store.List("ns1")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e1.Resource), configFor(e3.Resource)))

	// ns2 should have gateways 2
	out, err = store.List("ns2")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e2.Resource)))
}

func TestUpdate(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	e1 := eventFor(event.Added, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e1)
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e1.Resource)))
	expected := eventEntryFor(e1, nil)
	handler.expectEntry(t, expected)
	prev := expected.Config

	// Update the resource and verify that it's changed.
	e2 := eventFor(event.Updated, gatewaySchema, gateway2, "default/mygateway", "v1")
	store.Handle(e2)
	out, err = store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(Equal([]model.Config{configFor(e2.Resource)}))
	handler.expectEntry(t, eventEntryFor(e2, prev))
}

func TestDelete(t *testing.T) {
	g := NewGomegaWithT(t)
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	e1 := eventFor(event.Added, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e1)
	out, err := store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ConsistOf(configFor(e1.Resource)))
	handler.expectEntry(t, eventEntryFor(e1, nil))

	// Delete the resource and verify that it's no longer there.
	e2 := eventFor(event.Deleted, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e2)
	out, err = store.List("default")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(BeEmpty())
	handler.expectEntry(t, eventEntryFor(e2, nil))
}

func TestDeleteMissingConfig(t *testing.T) {
	handler := &fakeEventHandler{}
	store := newTestCollectionStore(handler)

	e2 := eventFor(event.Deleted, gatewaySchema, gateway, "default/mygateway", "v0")
	store.Handle(e2)
	handler.expectNoEntries(t)
}

func newTestCollectionStore(handler *fakeEventHandler) *collectionStore {
	return newCollectionStore(gatewaySchema, domainSuffix, handler.onEvent)
}

type fakeEventHandler struct {
	entries []eventEntry
}

func (h *fakeEventHandler) onEvent(schema collection.Schema, eventKind event.Kind, prev, conf *model.Config) {
	h.entries = append(h.entries, eventEntry{
		Schema: schema,
		Kind:   eventKind,
		Prev:   prev,
		Config: conf,
	})
}

func (h *fakeEventHandler) expectNumEntries(t *testing.T, num int) {
	t.Helper()
	if len(h.entries) != num {
		t.Fatalf("expected %d entries. Received %v+", num, h.entries)
	}
}

func (h *fakeEventHandler) expectEntry(t *testing.T, expected eventEntry) {
	t.Helper()
	h.expectNumEntries(t, 1)
	if diff := cmp.Diff(expected, h.entries[0]); diff != "" {
		t.Fatalf("entry does not match expected:\n%s", diff)
	}
	h.clear()
}

func (h *fakeEventHandler) expectNoEntries(t *testing.T) {
	t.Helper()
	h.expectNumEntries(t, 0)
}

func (h *fakeEventHandler) clear() {
	h.entries = h.entries[:0]
}

type eventEntry struct {
	Schema collection.Schema
	Kind   event.Kind
	Prev   *model.Config
	Config *model.Config
}

func eventFor(k event.Kind, s collection.Schema, msg proto.Message, name, version string) event.Event {
	return event.Event{
		Kind:     k,
		Source:   s,
		Resource: resourceFor(s, msg, name, version),
	}
}

func resourceFor(s collection.Schema, msg proto.Message, name, version string) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			Schema:      s.Resource(),
			FullName:    resource.NewShortOrFullName("default", name),
			CreateTime:  fakeTime,
			Version:     resource.Version(version),
			Labels:      nil,
			Annotations: nil,
		},
		Message: msg,
		Origin:  nil,
	}
}

func eventEntryFor(e event.Event, prev *model.Config) eventEntry {
	cfg := configFor(e.Resource)
	return eventEntry{
		Schema: e.Source,
		Kind:   e.Kind,
		Prev:   prev,
		Config: &cfg,
	}
}

func configFor(r *resource.Instance) model.Config {
	schema := r.Metadata.Schema
	fullName := r.Metadata.FullName
	return model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schema.Kind(),
			Group:             schema.Group(),
			Version:           schema.Version(),
			Name:              fullName.Name.String(),
			Namespace:         fullName.Namespace.String(),
			ResourceVersion:   r.Metadata.Version.String(),
			CreationTimestamp: r.Metadata.CreateTime,
			Labels:            r.Metadata.Labels,
			Annotations:       r.Metadata.Annotations,
			Domain:            domainSuffix,
		},
		Spec: r.Message,
	}
}
