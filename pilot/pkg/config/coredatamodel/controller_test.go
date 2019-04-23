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

package coredatamodel_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	authn "istio.io/api/authentication/v1alpha1"
	mcpapi "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/model"
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

	testControllerOptions = coredatamodel.Options{
		DomainSuffix:              "cluster.local",
		ClearDiscoveryServerCache: func() {},
	}
)

func TestOptions(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var cacheCleared bool
	testControllerOptions.ClearDiscoveryServerCache = func() {
		cacheCleared = true
	}
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g, model.ServiceEntry.MessageName, []proto.Message{serviceEntry})
	change := convert(
		[]proto.Message{message[0]},
		[]string{"service-bar"},
		model.ServiceEntry.Collection,
		model.ServiceEntry.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(model.ServiceEntry.Type, "")
	g.Expect(c).ToNot(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(c[0].Domain).To(gomega.Equal(testControllerOptions.DomainSuffix))
	g.Expect(cacheCleared).To(gomega.Equal(false))

	message = convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway})
	change = convert(
		[]proto.Message{message[0]},
		[]string{"gateway-foo"},
		model.Gateway.Collection,
		model.Gateway.MessageName)

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err = controller.List(model.Gateway.Type, "")
	g.Expect(c).ToNot(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(c[0].Domain).To(gomega.Equal(testControllerOptions.DomainSuffix))
	g.Expect(cacheCleared).To(gomega.Equal(true))
}

func TestHasSynced(t *testing.T) {
	t.Skip("Pending: https://github.com/istio/istio/issues/7947")
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	g.Expect(controller.HasSynced()).To(gomega.BeFalse())
}

func TestConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	descriptors := controller.ConfigDescriptor()
	g.Expect(descriptors).To(gomega.Equal(model.IstioConfigTypes))
}

func TestListInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	c, err := controller.List("bad-type", "some-phony-name-space.com")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("list unknown type"))
}

func TestListCorrectTypeNoData(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	c, err := controller.List("virtual-service", "some-phony-name-space.com")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
}

func TestListAllNameSpace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	messages := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway, gateway2, gateway3})
	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convert(
		[]proto.Message{message, message2, message3},
		[]string{"namespace1/some-gateway1", "default/some-other-gateway", "some-other-gateway3"},
		model.Gateway.Collection, model.Gateway.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(3))

	for _, conf := range c {
		g.Expect(conf.Type).To(gomega.Equal(model.Gateway.Type))
		if conf.Name == "some-gateway1" {
			g.Expect(conf.Spec).To(gomega.Equal(message))
			g.Expect(conf.Namespace).To(gomega.Equal("namespace1"))
		} else if conf.Name == "some-other-gateway" {
			g.Expect(conf.Namespace).To(gomega.Equal("default"))
			g.Expect(conf.Spec).To(gomega.Equal(message2))
		} else {
			g.Expect(conf.Name).To(gomega.Equal("some-other-gateway3"))
			g.Expect(conf.Namespace).To(gomega.Equal(""))
			g.Expect(conf.Spec).To(gomega.Equal(message3))
		}
	}
}

func TestListSpecificNameSpace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	messages := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway, gateway2, gateway3})
	message, message2, message3 := messages[0], messages[1], messages[2]

	change := convert(
		[]proto.Message{message, message2, message3},
		[]string{"namespace1/some-gateway1", "default/some-other-gateway", "namespace1/some-other-gateway3"},
		model.Gateway.Collection, model.Gateway.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "namespace1")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(2))

	for _, conf := range c {
		g.Expect(conf.Type).To(gomega.Equal(model.Gateway.Type))
		g.Expect(conf.Namespace).To(gomega.Equal("namespace1"))
		if conf.Name == "some-gateway1" {
			g.Expect(conf.Spec).To(gomega.Equal(message))
		} else {
			g.Expect(conf.Name).To(gomega.Equal("some-other-gateway3"))
			g.Expect(conf.Spec).To(gomega.Equal(message3))
		}
	}
}

func TestApplyInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway})
	change := convert([]proto.Message{message[0]}, []string{"some-gateway"},
		"bad-collection", "bad-type")

	err := controller.Apply(change)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestApplyValidTypeWithNoBaseURL(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	var createAndCheckGateway = func(g *gomega.GomegaWithT, controller coredatamodel.CoreDataModel, port uint32) {
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
		g.Expect(err).ToNot(gomega.HaveOccurred())

		message, err := makeMessage(marshaledGateway, model.Gateway.MessageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())

		change := convert([]proto.Message{message}, []string{"some-gateway"},
			model.Gateway.Collection, model.Gateway.MessageName)
		err = controller.Apply(change)
		g.Expect(err).ToNot(gomega.HaveOccurred())

		c, err := controller.List("gateway", "")
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(len(c)).To(gomega.Equal(1))
		g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
		g.Expect(c[0].Type).To(gomega.Equal(model.Gateway.Type))
		g.Expect(c[0].Spec).To(gomega.Equal(message))
		g.Expect(c[0].Spec).To(gomega.ContainSubstring(fmt.Sprintf("number:%d", port)))
	}
	createAndCheckGateway(g, controller, 80)
	createAndCheckGateway(g, controller, 9999)
}

func TestApplyMetadataNameIncludesNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway})

	change := convert([]proto.Message{message[0]}, []string{"istio-namespace/some-gateway"},
		model.Gateway.Collection, model.Gateway.MessageName)
	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "istio-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(model.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message[0]))
}

func TestApplyMetadataNameWithoutNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway})

	change := convert([]proto.Message{message[0]}, []string{"some-gateway"}, model.Gateway.Collection, model.Gateway.MessageName)
	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(model.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message[0]))
}

func TestApplyChangeNoObjects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway})
	change := convert([]proto.Message{message[0]}, []string{"some-gateway"}, model.Gateway.Collection, model.Gateway.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(model.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message[0]))

	change = convert([]proto.Message{}, []string{"some-gateway"}, model.Gateway.Collection, model.Gateway.MessageName)

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err = controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(0))
}

func TestApplyClusterScopedAuthPolicy(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message0 := convertToResource(g, model.AuthenticationPolicy.MessageName, []proto.Message{authnPolicy0})
	message1 := convertToResource(g, model.AuthenticationMeshPolicy.MessageName, []proto.Message{authnPolicy1})

	change := convert(
		[]proto.Message{message0[0]},
		[]string{"bar-namespace/foo"},
		model.AuthenticationPolicy.Collection, model.AuthenticationPolicy.MessageName)
	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	change = convert(
		[]proto.Message{message1[0]},
		[]string{"default"},
		model.AuthenticationMeshPolicy.Collection, model.AuthenticationMeshPolicy.MessageName)
	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(model.AuthenticationPolicy.Type, "bar-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("foo"))
	g.Expect(c[0].Namespace).To(gomega.Equal("bar-namespace"))
	g.Expect(c[0].Type).To(gomega.Equal(model.AuthenticationPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message0[0]))

	c, err = controller.List(model.AuthenticationMeshPolicy.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("default"))
	g.Expect(c[0].Namespace).To(gomega.Equal(""))
	g.Expect(c[0].Type).To(gomega.Equal(model.AuthenticationMeshPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message1[0]))

	// verify the namespace scoped resource can be deleted
	change = convert(
		[]proto.Message{},
		[]string{},
		model.AuthenticationPolicy.Collection, model.AuthenticationPolicy.MessageName)
	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err = controller.List(model.AuthenticationMeshPolicy.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("default"))
	g.Expect(c[0].Namespace).To(gomega.Equal(""))
	g.Expect(c[0].Type).To(gomega.Equal(model.AuthenticationMeshPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message1[0]))

	// verify the namespace scoped resource can be added and mesh-scoped resource removed
	change = convert(
		[]proto.Message{message0[0]},
		[]string{"bar-namespace/foo"},
		model.AuthenticationPolicy.Collection, model.AuthenticationPolicy.MessageName)
	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err = controller.List(model.AuthenticationPolicy.Type, "bar-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("foo"))
	g.Expect(c[0].Namespace).To(gomega.Equal("bar-namespace"))
	g.Expect(c[0].Type).To(gomega.Equal(model.AuthenticationPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message0[0]))
}

func TestEventHandler(t *testing.T) {
	controller := coredatamodel.NewController(testControllerOptions)

	makeName := func(namespace, name string) string {
		return namespace + "/" + name
	}

	gotEvents := map[model.Event]map[string]model.Config{
		model.EventAdd:    {},
		model.EventUpdate: {},
		model.EventDelete: {},
	}
	controller.RegisterEventHandler(model.ServiceEntry.Type, func(m model.Config, e model.Event) {
		gotEvents[e][makeName(m.Namespace, m.Name)] = m
	})

	typeURL := "type.googleapis.com/istio.networking.v1alpha3.ServiceEntry"
	collection := model.ServiceEntry.Collection

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
				Type:              model.ServiceEntry.Type,
				Group:             model.ServiceEntry.Group,
				Version:           model.ServiceEntry.Version,
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

func convert(resources []proto.Message, names []string, collection, responseMessageName string) *sink.Change {
	out := new(sink.Change)
	out.Collection = collection
	for i, res := range resources {
		out.Objects = append(out.Objects,
			&sink.Object{
				TypeURL: responseMessageName,
				Metadata: &mcpapi.Metadata{
					Name: names[i],
				},
				Body: res,
			},
		)
	}
	return out
}

func convertToResource(g *gomega.GomegaWithT, messageName string, resources []proto.Message) (messages []proto.Message) {
	for _, resource := range resources {
		marshaled, err := proto.Marshal(resource)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		message, err := makeMessage(marshaled, messageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		messages = append(messages, message)
	}
	return messages
}

func makeMessage(value []byte, responseMessageName string) (proto.Message, error) {
	resource := &types.Any{
		TypeUrl: fmt.Sprintf("type.googleapis.com/%s", responseMessageName),
		Value:   value,
	}

	var dynamicAny types.DynamicAny
	err := types.UnmarshalAny(resource, &dynamicAny)
	if err == nil {
		return dynamicAny.Message, nil
	}

	return nil, err
}

func TestInvalidResource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	gw := proto.Clone(gateway).(*networking.Gateway)
	gw.Servers[0].Hosts = nil

	message0 := convertToResource(g, model.Gateway.MessageName, []proto.Message{gw})

	change := convert(
		[]proto.Message{message0[0]},
		[]string{"bar-namespace/foo"},
		model.Gateway.Collection, model.Gateway.MessageName)
	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(model.Gateway.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}

func TestInvalidResource_BadTimestamp(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message0 := convertToResource(g, model.Gateway.MessageName, []proto.Message{gateway})
	change := convert(
		[]proto.Message{message0[0]},
		[]string{"bar-namespace/foo"},
		model.Gateway.Collection, model.Gateway.MessageName)
	change.Objects[0].Metadata.CreateTime = &types.Timestamp{
		Seconds: -1,
		Nanos:   -1,
	}

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(model.Gateway.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}
