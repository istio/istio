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

package mcp_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"

	mcpapi "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/mcp"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/mcp/sink"
)

var (
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
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": 4433,
				},
				Labels: map[string]string{"label": "random-label"},
			},
		},
	}

	testControllerOptions = &mcp.Options{
		DomainSuffix: "cluster.local",
		ConfigLedger: &model.DisabledLedger{},
	}
)

func TestOptions(t *testing.T) {
	g := NewGomegaWithT(t)

	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Proto(),
		serviceEntry)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"service-bar"},
		setCollection(collections.IstioNetworkingV1Alpha3Serviceentries.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	c, err := controller.List(gvk.ServiceEntry, "")
	g.Expect(c).ToNot(BeNil())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(c[0].Domain).To(Equal(testControllerOptions.DomainSuffix))
}

func TestHasSynced(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	g.Expect(controller.HasSynced()).To(BeFalse())
}

func TestConfigDescriptor(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)
	schemas := controller.Schemas()
	g.Expect(schemas.CollectionNames()).Should(ConsistOf(collections.Pilot.CollectionNames()))
}

func TestListInvalidType(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	c, err := controller.List(resource.GroupVersionKind{Kind: "bad-type"}, "some-phony-name-space.com")
	g.Expect(c).To(BeNil())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("list unknown type"))
}

func TestListCorrectTypeNoData(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	c, err := controller.List(gvk.VirtualService,
		"some-phony-name-space.com")
	g.Expect(c).To(BeNil())
	g.Expect(err).ToNot(HaveOccurred())
}

func TestListAllNameSpace(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := mcp.NewController(testControllerOptions)

	messages := convertToResources(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		[]proto.Message{gateway, gateway2, gateway3})

	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"namespace1/some-gateway1", "default/some-other-gateway", "some-other-gateway3"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	c, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(3))

	for _, conf := range c {
		g.Expect(conf.GroupVersionKind).To(Equal(gvk.Gateway))
		if conf.Name == "some-gateway1" {
			g.Expect(conf.Spec).To(Equal(message))
			g.Expect(conf.Namespace).To(Equal("namespace1"))
		} else if conf.Name == "some-other-gateway" {
			g.Expect(conf.Namespace).To(Equal("default"))
			g.Expect(conf.Spec).To(Equal(message2))
		} else {
			g.Expect(conf.Name).To(Equal("some-other-gateway3"))
			g.Expect(conf.Namespace).To(Equal(""))
			g.Expect(conf.Spec).To(Equal(message3))
		}
	}
}

func TestListSpecificNameSpace(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	messages := convertToResources(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		[]proto.Message{gateway, gateway2, gateway3})

	message, message2, message3 := messages[0], messages[1], messages[2]

	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"namespace1/some-gateway1", "default/some-other-gateway", "namespace1/some-other-gateway3"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	c, err := controller.List(gvk.Gateway, "namespace1")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(2))

	for _, conf := range c {
		g.Expect(conf.GroupVersionKind).To(Equal(gvk.Gateway))
		g.Expect(conf.Namespace).To(Equal("namespace1"))
		if conf.Name == "some-gateway1" {
			g.Expect(conf.Spec).To(Equal(message))
		} else {
			g.Expect(conf.Name).To(Equal("some-other-gateway3"))
			g.Expect(conf.Spec).To(Equal(message3))
		}
	}
}

func TestApplyInvalidType(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		gateway)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"some-gateway"},
		setCollection("bad-collection"),
		setTypeURL("bad-type"))

	err := controller.Apply(change)
	g.Expect(err).To(HaveOccurred())
}

func TestApplyValidTypeWithNoNamespace(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	var createAndCheckGateway = func(g *GomegaWithT, controller mcp.Controller, port uint32) {
		gateway := &networking.Gateway{
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
		marshaledGateway, err := proto.Marshal(gateway)
		g.Expect(err).ToNot(HaveOccurred())

		message, err := makeMessage(marshaledGateway, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto())
		g.Expect(err).ToNot(HaveOccurred())

		change := convertToChange([]proto.Message{message},
			[]string{"some-gateway"},
			setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
			setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

		err = controller.Apply(change)
		g.Expect(err).ToNot(HaveOccurred())

		c, err := controller.List(gvk.Gateway, "")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(c)).To(Equal(1))
		g.Expect(c[0].Name).To(Equal("some-gateway"))
		g.Expect(c[0].GroupVersionKind).To(Equal(gvk.Gateway))
		g.Expect(c[0].Spec).To(Equal(message))
		g.Expect(c[0].Spec).To(ContainSubstring(fmt.Sprintf("number:%d", port)))
	}
	createAndCheckGateway(g, controller, 80)
	createAndCheckGateway(g, controller, 9999)
}

func TestApplyMetadataNameIncludesNamespace(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"istio-namespace/some-gateway"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	c, err := controller.List(gvk.Gateway, "istio-namespace")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("some-gateway"))
	g.Expect(c[0].GroupVersionKind).To(Equal(gvk.Gateway))
	g.Expect(c[0].Spec).To(Equal(message))
}

func TestApplyMetadataNameWithoutNamespace(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"some-gateway"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	c, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("some-gateway"))
	g.Expect(c[0].GroupVersionKind).To(Equal(gvk.Gateway))
	g.Expect(c[0].Spec).To(Equal(message))
}

func TestApplyChangeNoObjects(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"some-gateway"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())
	c, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(1))
	g.Expect(c[0].Name).To(Equal("some-gateway"))
	g.Expect(c[0].GroupVersionKind).To(Equal(gvk.Gateway))
	g.Expect(c[0].Spec).To(Equal(message))

	change = convertToChange([]proto.Message{},
		[]string{"some-gateway"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err = controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())
	c, err = controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(c)).To(Equal(0))
}

func TestApplyConfigUpdate(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"some-gateway"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	event := <-fx.Events
	g.Expect(event).To(Equal("ConfigUpdate"))
}

func TestInvalidResource(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	gw := proto.Clone(gateway).(*networking.Gateway)
	gw.Servers[0].Hosts = nil

	message := convertToResource(g, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(), gw)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"bar-namespace/foo"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(0))
}

func TestInvalidResource_BadTimestamp(t *testing.T) {
	g := NewGomegaWithT(t)
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(), gateway)
	change := convertToChange(
		[]proto.Message{message},
		[]string{"bar-namespace/foo"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	change.Objects[0].Metadata.CreateTime = &types.Timestamp{
		Seconds: -1,
		Nanos:   -1,
	}

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(0))
}

func TestEventHandler(t *testing.T) {
	controller := mcp.NewController(testControllerOptions)

	makeName := func(namespace, name string) string {
		return namespace + "/" + name
	}

	gotEvents := map[model.Event]map[string]model.Config{
		model.EventAdd:    {},
		model.EventUpdate: {},
		model.EventDelete: {},
	}
	controller.RegisterEventHandler(gvk.ServiceEntry, func(_, m model.Config, e model.Event) {
		gotEvents[e][makeName(m.Namespace, m.Name)] = m
	})

	typeURL := "type.googleapis.com/istio.networking.v1alpha3.ServiceEntry"
	collection := collections.IstioNetworkingV1Alpha3Serviceentries.Name().String()

	fakeCreateTime, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	fakeCreateTimeProto, err := types.TimestampProto(fakeCreateTime)
	if err != nil {
		t.Fatalf("Failed to parse create fake create time %v: %v", fakeCreateTime, err)
	}

	makeServiceEntry := func(name, host, version string) *sink.Object {
		return &sink.Object{
			TypeURL: typeURL,
			Metadata: &mcpapi.Metadata{
				Name:        fmt.Sprintf("default/%s", name),
				CreateTime:  fakeCreateTimeProto,
				Version:     version,
				Labels:      map[string]string{"lk1": "lv1"},
				Annotations: map[string]string{"ak1": "av1"},
			},
			Body: &networking.ServiceEntry{
				Hosts: []string{host},
			},
		}
	}

	makeServiceEntryModel := func(name, host, version string) model.Config {
		return model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind:  gvk.ServiceEntry,
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
	steps := []struct {
		name   string
		change *sink.Change
		want   map[model.Event]map[string]model.Config
	}{
		{
			name: "initial add",
			change: &sink.Change{
				Collection: collection,
				Objects: []*sink.Object{
					makeServiceEntry("foo", "foo.com", "v0"),
				},
			},
			want: map[model.Event]map[string]model.Config{
				model.EventAdd: {
					"default/foo": makeServiceEntryModel("foo", "foo.com", "v0"),
				},
			},
		},
		{
			name: "update initial item",
			change: &sink.Change{
				Collection: collection,
				Objects: []*sink.Object{
					makeServiceEntry("foo", "foo.com", "v1"),
				},
			},
			want: map[model.Event]map[string]model.Config{
				model.EventUpdate: {
					"default/foo": makeServiceEntryModel("foo", "foo.com", "v1"),
				},
			},
		},
		{
			name: "subsequent add",
			change: &sink.Change{
				Collection: collection,
				Objects: []*sink.Object{
					makeServiceEntry("foo", "foo.com", "v1"),
					makeServiceEntry("foo1", "foo1.com", "v0"),
				},
			},
			want: map[model.Event]map[string]model.Config{
				model.EventAdd: {
					"default/foo1": makeServiceEntryModel("foo1", "foo1.com", "v0"),
				},
			},
		},
		{
			name: "single delete",
			change: &sink.Change{
				Collection: collection,
				Objects: []*sink.Object{
					makeServiceEntry("foo1", "foo1.com", "v0"),
				},
			},
			want: map[model.Event]map[string]model.Config{
				model.EventDelete: {
					"default/foo": makeServiceEntryModel("foo", "foo.com", "v1"),
				},
			},
		},
		{
			name: "multiple update and add",
			change: &sink.Change{
				Collection: collection,
				Objects: []*sink.Object{
					makeServiceEntry("foo1", "foo1.com", "v1"),
					makeServiceEntry("foo2", "foo2.com", "v0"),
					makeServiceEntry("foo3", "foo3.com", "v0"),
				},
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
			name: "multiple deletes, updates, and adds ",
			change: &sink.Change{
				Collection: collection,
				Objects: []*sink.Object{
					makeServiceEntry("foo2", "foo2.com", "v1"),
					makeServiceEntry("foo3", "foo3.com", "v0"),
					makeServiceEntry("foo4", "foo4.com", "v0"),
					makeServiceEntry("foo5", "foo5.com", "v0"),
				},
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

	for i, s := range steps {
		t.Run(fmt.Sprintf("[%v] %s", i, s.name), func(tt *testing.T) {
			if err := controller.Apply(s.change); err != nil {
				tt.Fatalf("Apply() failed: %v", err)
			}

			for eventType, wantConfigs := range s.want {
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

func convertToChange(resources []proto.Message, names []string, options ...func(*sink.Change)) *sink.Change {
	out := new(sink.Change)
	for i, res := range resources {
		obj := &sink.Object{
			Metadata: &mcpapi.Metadata{
				Name: names[i],
			},
			Body: res,
		}
		out.Objects = append(out.Objects, obj)
	}
	// apply options
	for _, option := range options {
		option(out)
	}
	return out
}

func setCollection(collection string) func(*sink.Change) {
	return func(c *sink.Change) {
		c.Collection = collection
	}
}

func setTypeURL(url string) func(*sink.Change) {
	return func(c *sink.Change) {
		for _, obj := range c.Objects {
			obj.TypeURL = url
		}
	}
}

func convertToResource(g *GomegaWithT, typeURL string, resource proto.Message) (messages proto.Message) {
	marshaled, err := proto.Marshal(resource)
	g.Expect(err).ToNot(HaveOccurred())
	message, err := makeMessage(marshaled, typeURL)
	g.Expect(err).ToNot(HaveOccurred())
	return message
}

func convertToResources(g *GomegaWithT, typeURL string, resources []proto.Message) (messages []proto.Message) {
	for _, resource := range resources {
		message := convertToResource(g, typeURL, resource)
		messages = append(messages, message)
	}
	return messages
}

func makeMessage(value []byte, typeURL string) (proto.Message, error) {
	resource := &types.Any{
		TypeUrl: fmt.Sprintf("type.googleapis.com/%s", typeURL),
		Value:   value,
	}

	var dynamicAny types.DynamicAny
	err := types.UnmarshalAny(resource, &dynamicAny)
	if err == nil {
		return dynamicAny.Message, nil
	}

	return nil, err
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

func TestApplyIncrementalChangeRemove(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(), gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-gateway"},
		setIncremental(),
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(1))
	g.Expect(entries[0].Name).To(Equal("test-gateway"))

	update := <-fx.Events
	g.Expect(update).To(Equal("ConfigUpdate"))

	message2 := convertToResource(g, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(), gateway2)
	change = convertToChange([]proto.Message{message2},
		[]string{"random-namespace/test-gateway2"},
		setIncremental(),
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err = controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err = controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(2))

	update = <-fx.Events
	g.Expect(update).To(Equal("ConfigUpdate"))

	for _, gw := range entries {
		g.Expect(gw.GroupVersionKind).To(Equal(gvk.Gateway))
		switch gw.Name {
		case "test-gateway":
			g.Expect(gw.Spec).To(Equal(message))
		case "test-gateway2":
			g.Expect(gw.Spec).To(Equal(message2))
		}
	}

	change = convertToChange([]proto.Message{message2},
		[]string{"random-namespace/test-gateway2"},
		setIncremental(),
		setRemoved([]string{"random-namespace/test-gateway"}),
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err = controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err = controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(1))
	g.Expect(entries[0].Name).To(Equal("test-gateway2"))
	g.Expect(entries[0].Spec).To(Equal(message2))

	update = <-fx.Events
	g.Expect(update).To(Equal("ConfigUpdate"))
}

func TestApplyIncrementalChange(t *testing.T) {
	g := NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := mcp.NewController(testControllerOptions)

	message := convertToResource(g, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(), gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-gateway"},
		setIncremental(),
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err := controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err := controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(1))
	g.Expect(entries[0].Name).To(Equal("test-gateway"))

	update := <-fx.Events
	g.Expect(update).To(Equal("ConfigUpdate"))

	message2 := convertToResource(g, collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(), gateway2)
	change = convertToChange([]proto.Message{message2},
		[]string{"random-namespace/test-gateway2"},
		setIncremental(),
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto()))

	err = controller.Apply(change)
	g.Expect(err).ToNot(HaveOccurred())

	entries, err = controller.List(gvk.Gateway, "")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(entries).To(HaveLen(2))

	for _, gw := range entries {
		g.Expect(gw.GroupVersionKind).To(Equal(gvk.Gateway))
		switch gw.Name {
		case "test-gateway":
			g.Expect(gw.Spec).To(Equal(message))
		case "test-gateway2":
			g.Expect(gw.Spec).To(Equal(message2))
		}
	}

	update = <-fx.Events
	g.Expect(update).To(Equal("ConfigUpdate"))
}

func setIncremental() func(*sink.Change) {
	return func(c *sink.Change) {
		c.Incremental = true
	}
}

func setRemoved(removed []string) func(*sink.Change) {
	return func(c *sink.Change) {
		c.Removed = removed
	}
}
