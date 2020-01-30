// Copyright 2018 Istio Authors
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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/gomega"

	authn "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	configEvent "istio.io/istio/pilot/pkg/config/event"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	collection2 "istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	resourceSchema "istio.io/istio/pkg/config/schema/resource"
)

var (
	gatewayCollection                  = collections.IstioNetworkingV1Alpha3Gateways
	serviceEntryCollection             = collections.IstioNetworkingV1Alpha3Serviceentries
	virtualServiceCollection           = collections.IstioNetworkingV1Alpha3Virtualservices
	authenticationPolicyCollection     = collections.IstioAuthenticationV1Alpha1Policies
	authenticationMeshPolicyCollection = collections.IstioAuthenticationV1Alpha1Meshpolicies

	gatewayGvk                  = gatewayCollection.Resource().GroupVersionKind()
	serviceEntryGvk             = serviceEntryCollection.Resource().GroupVersionKind()
	virtualServiceGvk           = virtualServiceCollection.Resource().GroupVersionKind()
	authenticationPolicyGvk     = authenticationPolicyCollection.Resource().GroupVersionKind()
	authenticationMeshPolicyGvk = authenticationMeshPolicyCollection.Resource().GroupVersionKind()

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
					Number:   80,
					Name:     "http",
					Protocol: "HTTP",
				},
				Hosts: []string{"*.example.com"},
			},
		},
	}

	gateway3 = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   8080,
					Name:     "http",
					Protocol: "HTTP",
				},
				Hosts: []string{"foo.example.com"},
			},
		},
	}

	authnPolicy0 = &authn.Policy{
		Targets: []*authn.TargetSelector{{
			Name: "service-foo",
		}},
		Peers: []*authn.PeerAuthenticationMethod{{
			Params: &authn.PeerAuthenticationMethod_Mtls{}},
		},
	}

	authnPolicy1 = &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{{
			Params: &authn.PeerAuthenticationMethod_Mtls{}},
		},
	}

	serviceEntry = &networking.ServiceEntry{
		Hosts: []string{"example.com"},
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   7878,
				Protocol: "http",
			},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": 4433,
				},
				Labels: map[string]string{"label": "random-label"},
			},
		},
	}
)

func TestHasSynced(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	// Sync all the collections.
	all := collections.Pilot.All()
	for i, s := range all {
		handler.Handle(event.FullSyncFor(s))

		if i < len(all)-1 {
			g.Expect(store.HasSynced()).To(BeFalse())
		} else {
			// After the last collection is synced, verify that the store is synced.
			g.Expect(store.HasSynced()).To(BeTrue())
		}
	}
}

func TestSchemas(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, _ := configEvent.NewConfigStoreAndHandler(opts)
	g.Expect(store.Schemas()).To(Equal(collections.Pilot))
}

func TestListInvalidType(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, _ := configEvent.NewConfigStoreAndHandler(opts)

	c, err := store.List(resourceSchema.GroupVersionKind{Kind: "bad-type"}, "some-phony-name-space.com")
	g.Expect(c).To(BeNil())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("list unknown type"))
}

func TestListCorrectTypeNoData(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, _ := configEvent.NewConfigStoreAndHandler(opts)

	c, err := store.List(virtualServiceGvk, "some-phony-name-space.com")
	g.Expect(c).To(BeNil())
	g.Expect(err).ToNot(HaveOccurred())
}

func TestListAllNameSpace(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	opts := newMCPOptions()
	opts.XDSUpdater = fx
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	e1 := eventFor(event.Added, gatewayCollection, gateway, "namespace1/some-gateway1", "v1")
	e2 := eventFor(event.Added, gatewayCollection, gateway2, "default/some-other-gateway", "v1")
	e3 := eventFor(event.Added, gatewayCollection, gateway3, "some-other-gateway3", "v1")

	// Handle the events.
	sendEvents(handler, e1, e2, e3)

	c, err := store.List(gatewayGvk, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(3))

	for _, conf := range c {
		g.Expect(conf.GroupVersionKind()).To(Equal(gatewayGvk))
		switch conf.Name {
		case "some-gateway1":
			g.Expect(conf.Spec).To(Equal(e1.Resource.Message))
			g.Expect(conf.Namespace).To(Equal("namespace1"))
		case "some-other-gateway":
			g.Expect(conf.Namespace).To(Equal("default"))
			g.Expect(conf.Spec).To(Equal(e2.Resource.Message))
		case "some-other-gateway3":
			g.Expect(conf.Namespace).To(Equal(""))
			g.Expect(conf.Spec).To(Equal(e3.Resource.Message))
		default:
			t.Fatalf("unexpected resource name: %s", conf.Name)
		}
	}
}

func TestListSpecificNameSpace(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	opts := newMCPOptions()
	opts.XDSUpdater = fx
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	e1 := eventFor(event.Added, gatewayCollection, gateway, "namespace1/some-gateway1", "v1")
	e2 := eventFor(event.Added, gatewayCollection, gateway2, "default/some-other-gateway", "v1")
	e3 := eventFor(event.Added, gatewayCollection, gateway3, "namespace1/some-other-gateway3", "v1")

	// Handle the events.
	sendEvents(handler, e1, e2, e3)

	c, err := store.List(gatewayGvk, "namespace1")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(2))

	for _, conf := range c {
		g.Expect(conf.GroupVersionKind()).To(Equal(gatewayGvk))
		switch conf.Name {
		case "some-gateway1":
			g.Expect(conf.Spec).To(Equal(e1.Resource.Message))
			g.Expect(conf.Namespace).To(Equal("namespace1"))
		case "some-other-gateway3":
			g.Expect(conf.Namespace).To(Equal("namespace1"))
			g.Expect(conf.Spec).To(Equal(e3.Resource.Message))
		default:
			t.Fatalf("unexpected resource name: %s", conf.Name)
		}
	}
}

func TestInvalidCollection(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	opts := newMCPOptions()
	opts.XDSUpdater = fx
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	// Synthetic service entries are unsupported.
	e := eventFor(event.Added, collections.IstioNetworkingV1Alpha3SyntheticServiceentries,
		serviceEntry, "ns1/se", "v1")

	// Handle the events.
	sendEvents(handler, e)
	g.Expect(store.List(serviceEntryGvk, "ns1")).To(BeEmpty())
}

func TestApplyValidKindWithNoNamespace(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	var createAndCheckGateway = func(g *GomegaWithT, version string, port uint32) {
		spec := &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   port,
						Name:     "http",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		}
		e := eventFor(event.Added, gatewayCollection, spec, "some-gateway", version)
		sendEvents(handler, e)

		c, err := store.List(gatewayGvk, "")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(c)).To(Equal(1))
		g.Expect(c[0].Name).To(Equal("some-gateway"))
		g.Expect(c[0].GroupVersionKind()).To(Equal(gatewayGvk))
		g.Expect(c[0].Spec).To(Equal(spec))
		g.Expect(c[0].Spec).To(ContainSubstring(fmt.Sprintf("number:%d", port)))
	}
	createAndCheckGateway(g, "v0", 80)
	createAndCheckGateway(g, "v1", 9999)
}

func TestApplyMetadataNameIncludesNamespace(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	e := eventFor(event.Added, gatewayCollection, gateway, "istio-namespace/some-gateway", "v1")
	sendEvents(handler, e)

	c, err := store.List(gatewayGvk, "istio-namespace")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("some-gateway"))
	g.Expect(c[0].GroupVersionKind()).To(Equal(gatewayGvk))
	g.Expect(c[0].Spec).To(Equal(gateway))
}

func TestApplyMetadataNameWithoutNamespace(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	e := eventFor(event.Added, gatewayCollection, gateway, "some-gateway", "v1")
	sendEvents(handler, e)

	c, err := store.List(gatewayGvk, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("some-gateway"))
	g.Expect(c[0].GroupVersionKind()).To(Equal(gatewayGvk))
	g.Expect(c[0].Spec).To(Equal(gateway))
}

func TestApplyConfigUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	opts := newMCPOptions()
	opts.XDSUpdater = fx
	_, handler := configEvent.NewConfigStoreAndHandler(opts)

	e := eventFor(event.Added, gatewayCollection, gateway, "some-gateway", "v1")
	sendEvents(handler, e)

	ev := <-fx.Events
	g.Expect(ev).To(Equal("ConfigUpdate"))
}

func TestDeleteNonExistentResource(t *testing.T) {
	g := NewGomegaWithT(t)

	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)
	e := eventFor(event.Deleted, authenticationPolicyCollection, authnPolicy1, "default", "v1")
	sendEvents(handler, e)

	c, err := store.List(authenticationPolicyGvk, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(c).To(BeEmpty())
}

func TestApplyClusterScopedAuthPolicy(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	e := eventFor(event.Added, authenticationPolicyCollection, authnPolicy0, "bar-namespace/foo", "v1")
	sendEvents(handler, e)
	e = eventFor(event.Added, authenticationMeshPolicyCollection, authnPolicy1, "default", "v1")
	sendEvents(handler, e)

	c, err := store.List(authenticationPolicyGvk, "bar-namespace")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("foo"))
	g.Expect(c[0].Namespace).To(Equal("bar-namespace"))
	g.Expect(c[0].GroupVersionKind()).To(Equal(authenticationPolicyGvk))
	g.Expect(c[0].Spec).To(Equal(authnPolicy0))

	c, err = store.List(authenticationMeshPolicyGvk, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("default"))
	g.Expect(c[0].Namespace).To(Equal(""))
	g.Expect(c[0].GroupVersionKind()).To(Equal(authenticationMeshPolicyGvk))
	g.Expect(c[0].Spec).To(Equal(authnPolicy1))

	// verify the namespace scoped resource can be added and mesh-scoped resource removed
	e = eventFor(event.Updated, authenticationPolicyCollection, authnPolicy0, "bar-namespace/foo", "v1")
	sendEvents(handler, e)
	e = eventFor(event.Deleted, authenticationMeshPolicyCollection, authnPolicy1, "default", "v1")
	sendEvents(handler, e)

	c, err = store.List(authenticationPolicyGvk, "bar-namespace")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("foo"))
	g.Expect(c[0].Namespace).To(Equal("bar-namespace"))
	g.Expect(c[0].GroupVersionKind()).To(Equal(authenticationPolicyGvk))
	g.Expect(c[0].Spec).To(Equal(authnPolicy0))
}

func TestInvalidResource(t *testing.T) {
	g := NewGomegaWithT(t)
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	gw := proto.Clone(gateway).(*networking.Gateway)
	gw.Servers[0].Hosts = nil

	e := eventFor(event.Added, gatewayCollection, gw, "bar-namespace/foo", "v1")
	sendEvents(handler, e)

	entries, err := store.List(gatewayGvk, "bar-namespace")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(BeEmpty())
}

func TestEventHandler(t *testing.T) {
	opts := newMCPOptions()
	store, handler := configEvent.NewConfigStoreAndHandler(opts)

	gotEvents := map[model.Event]map[string]model.Config{
		model.EventAdd:    {},
		model.EventUpdate: {},
		model.EventDelete: {},
	}

	store.RegisterEventHandler(serviceEntryGvk, func(_, m model.Config, e model.Event) {
		gotEvents[e][m.Namespace+"/"+m.Name] = m
	})

	fakeCreateTime, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	makeServiceEntryEvent := func(kind event.Kind, name, host, version string) event.Event {
		return event.Event{
			Kind:   kind,
			Source: serviceEntryCollection,
			Resource: &resource.Instance{
				Metadata: resource.Metadata{
					Schema:      serviceEntryCollection.Resource(),
					FullName:    resource.NewShortOrFullName("default", name),
					CreateTime:  fakeCreateTime,
					Version:     resource.Version(version),
					Labels:      map[string]string{"lk1": "lv1"},
					Annotations: map[string]string{"ak1": "av1"},
				},
				Message: &networking.ServiceEntry{
					Hosts: []string{host},
				},
			},
		}
	}

	makeServiceEntryModel := func(name, host, version string) model.Config {
		return model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              serviceEntryGvk.Kind,
				Group:             serviceEntryGvk.Group,
				Version:           serviceEntryGvk.Version,
				Name:              name,
				Namespace:         "default",
				Domain:            "cluster.local",
				ResourceVersion:   version,
				CreationTimestamp: fakeCreateTime,
				Labels:            map[string]string{"lk1": "lv1"},
				Annotations:       map[string]string{"ak1": "av1"},
			},
			Spec: &networking.ServiceEntry{Hosts: []string{host}},
		}
	}

	// Note: these tests steps are cumulative
	cases := []struct {
		name   string
		events []event.Event
		want   map[model.Event]map[string]model.Config
	}{
		{
			name: "add",
			events: []event.Event{
				makeServiceEntryEvent(event.Added, "default/foo", "foo.com", "v0"),
			},
			want: map[model.Event]map[string]model.Config{
				model.EventAdd: {
					"default/foo": makeServiceEntryModel("foo", "foo.com", "v0"),
				},
			},
		},
		{
			name: "update",
			events: []event.Event{
				makeServiceEntryEvent(event.Updated, "foo", "foo.com", "v1"),
			},
			want: map[model.Event]map[string]model.Config{
				model.EventUpdate: {
					"default/foo": makeServiceEntryModel("foo", "foo.com", "v1"),
				},
			},
		},
		{
			name: "subsequent add",
			events: []event.Event{
				makeServiceEntryEvent(event.Added, "foo", "foo.com", "v1"),
				makeServiceEntryEvent(event.Added, "foo1", "foo1.com", "v0"),
			},
			want: map[model.Event]map[string]model.Config{
				model.EventAdd: {
					"default/foo1": makeServiceEntryModel("foo1", "foo1.com", "v0"),
				},
			},
		},
		{
			name: "delete",
			events: []event.Event{
				makeServiceEntryEvent(event.Deleted, "foo", "foo.com", "v1"),
				makeServiceEntryEvent(event.Updated, "foo1", "foo1.com", "v0"),
			},
			want: map[model.Event]map[string]model.Config{
				model.EventDelete: {
					"default/foo": makeServiceEntryModel("foo", "foo.com", "v1"),
				},
			},
		},
		{
			name: "update and add",
			events: []event.Event{
				makeServiceEntryEvent(event.Added, "foo1", "foo1.com", "v1"),
				makeServiceEntryEvent(event.Added, "foo2", "foo2.com", "v0"),
				makeServiceEntryEvent(event.Added, "foo3", "foo3.com", "v0"),
			},
			want: map[model.Event]map[string]model.Config{
				model.EventAdd: {
					"default/foo2": makeServiceEntryModel("foo2", "foo2.com", "v0"),
					"default/foo3": makeServiceEntryModel("foo3", "foo3.com", "v0"),
				},
				model.EventUpdate: {
					"default/foo1": makeServiceEntryModel("foo1", "foo1.com", "v1"),
				},
			},
		},
		{
			name: "update add and delete",
			events: []event.Event{
				makeServiceEntryEvent(event.Updated, "foo2", "foo2.com", "v1"),
				makeServiceEntryEvent(event.Updated, "foo3", "foo3.com", "v0"),
				makeServiceEntryEvent(event.Added, "foo4", "foo4.com", "v0"),
				makeServiceEntryEvent(event.Added, "foo5", "foo5.com", "v0"),
				makeServiceEntryEvent(event.Deleted, "foo1", "foo1.com", "v1"),
			},
			want: map[model.Event]map[string]model.Config{
				model.EventAdd: {
					"default/foo4": makeServiceEntryModel("foo4", "foo4.com", "v0"),
					"default/foo5": makeServiceEntryModel("foo5", "foo5.com", "v0"),
				},
				model.EventUpdate: {
					"default/foo2": makeServiceEntryModel("foo2", "foo2.com", "v1"),
				},
				model.EventDelete: {
					"default/foo1": makeServiceEntryModel("foo1", "foo1.com", "v1"),
				},
			},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %s", i, c.name), func(tt *testing.T) {
			sendEvents(handler, c.events...)

			for eventType, wantConfigs := range c.want {
				gotConfigs := gotEvents[eventType]
				if !reflect.DeepEqual(gotConfigs, wantConfigs) {
					tt.Fatalf("wrong %v event: \n got %+v \nwant %+v", eventType, gotConfigs, wantConfigs)
				}
			}
			// clear saved events after every step
			gotEvents = map[model.Event]map[string]model.Config{
				model.EventAdd:    {},
				model.EventUpdate: {},
				model.EventDelete: {},
			}
		})
	}
}

func eventFor(k event.Kind, s collection2.Schema, msg proto.Message, name, version string) event.Event {
	return event.Event{
		Kind:     k,
		Source:   s,
		Resource: resourceFor(s, msg, name, version),
	}
}

func resourceFor(s collection2.Schema, msg proto.Message, name, version string) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			Schema:      s.Resource(),
			FullName:    resource.NewShortOrFullName("", name),
			CreateTime:  time.Now(),
			Version:     resource.Version(version),
			Labels:      nil,
			Annotations: nil,
		},
		Message: msg,
		Origin:  nil,
	}
}

func newMCPOptions() *configEvent.Options {
	return &configEvent.Options{
		DomainSuffix: "cluster.local",
		ConfigLedger: &model.DisabledLedger{},
		XDSUpdater:   NewFakeXDS(),
	}
}

func sendEvents(h event.Handler, events ...event.Event) {
	// Handle the events.
	for _, e := range events {
		h.Handle(e)
	}
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

type FakeXdsUpdater struct {
	Events    chan string
	Endpoints chan []*model.IstioEndpoint
	EDSErr    chan error
}

func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		EDSErr:    make(chan error, 100),
		Events:    make(chan string, 100),
		Endpoints: make(chan []*model.IstioEndpoint, 100),
	}
}

func (f *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
	f.Events <- "ConfigUpdate"
}

func (f *FakeXdsUpdater) EDSUpdate(_, _, _ string, entry []*model.IstioEndpoint) error {
	f.Events <- "EDSUpdate"
	f.Endpoints <- entry
	return <-f.EDSErr
}

func (f *FakeXdsUpdater) SvcUpdate(_, _, _ string, _ model.Event) {
}

func (f *FakeXdsUpdater) ProxyUpdate(_, _ string) {
}
