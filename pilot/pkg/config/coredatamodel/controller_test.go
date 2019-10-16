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
	"istio.io/istio/pkg/config/schemas"
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

	syntheticServiceEntry0 = &networking.ServiceEntry{
		Hosts: []string{"svc.example2.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "2.2.2.2",
				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
			},
			{
				Address: "3.3.3.3",
				Ports:   map[string]uint32{"http-port": 1080},
			},
			{
				Address: "4.4.4.4",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{"foo": "bar"},
			},
		},
	}

	syntheticServiceEntry1 = &networking.ServiceEntry{
		Hosts: []string{"example2.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "2.2.2.2",
				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
			},
			{
				Address: "3.3.3.3",
				Ports:   map[string]uint32{"http-port": 1080},
			},
			{
				Address: "5.5.5.5",
				Ports:   map[string]uint32{"http-port": 1081},
				Labels:  map[string]string{"foo1": "bar1"},
			},
		},
	}

	testControllerOptions = &coredatamodel.Options{
		DomainSuffix: "cluster.local",
		ConfigLedger: &model.DisabledLedger{},
	}
)

func TestOptions(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g,
		schemas.ServiceEntry.MessageName,
		serviceEntry)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"service-bar"},
		setVersion("1"),
		setCollection(schemas.ServiceEntry.Collection),
		setTypeURL(schemas.ServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(schemas.ServiceEntry.Type, "")
	g.Expect(c).ToNot(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(c[0].Domain).To(gomega.Equal(testControllerOptions.DomainSuffix))
}

func TestHasSynced(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	g.Expect(controller.HasSynced()).To(gomega.BeFalse())
}

func TestConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)
	descriptors := controller.ConfigDescriptor()
	for _, descriptor := range descriptors {
		g.Expect(descriptor.Collection).ToNot(gomega.Equal(schemas.SyntheticServiceEntry.Collection))
	}
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

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := coredatamodel.NewController(testControllerOptions)

	messages := convertToResources(g,
		schemas.Gateway.MessageName,
		[]proto.Message{gateway, gateway2, gateway3})

	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"namespace1/some-gateway1", "default/some-other-gateway", "some-other-gateway3"},
		setVersion("1"),
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(3))

	for _, conf := range c {
		g.Expect(conf.Type).To(gomega.Equal(schemas.Gateway.Type))
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

	messages := convertToResources(g,
		schemas.Gateway.MessageName,
		[]proto.Message{gateway, gateway2, gateway3})

	message, message2, message3 := messages[0], messages[1], messages[2]

	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"namespace1/some-gateway1", "default/some-other-gateway", "namespace1/some-other-gateway3"},
		setVersion("1"),
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "namespace1")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(2))

	for _, conf := range c {
		g.Expect(conf.Type).To(gomega.Equal(schemas.Gateway.Type))
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

	message := convertToResource(g,
		schemas.Gateway.MessageName,
		gateway)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"some-gateway"},
		setVersion("1"),
		setCollection("bad-collection"),
		setTypeURL("bad-type"))

	err := controller.Apply(change)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestApplyValidTypeWithNoNamespace(t *testing.T) {
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

		message, err := makeMessage(marshaledGateway, schemas.Gateway.MessageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())

		change := convertToChange([]proto.Message{message},
			[]string{"some-gateway"},
			setVersion("1"),
			setCollection(schemas.Gateway.Collection),
			setTypeURL(schemas.Gateway.MessageName))

		err = controller.Apply(change)
		g.Expect(err).ToNot(gomega.HaveOccurred())

		c, err := controller.List("gateway", "")
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(len(c)).To(gomega.Equal(1))
		g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
		g.Expect(c[0].Type).To(gomega.Equal(schemas.Gateway.Type))
		g.Expect(c[0].Spec).To(gomega.Equal(message))
		g.Expect(c[0].Spec).To(gomega.ContainSubstring(fmt.Sprintf("number:%d", port)))
	}
	createAndCheckGateway(g, controller, 80)
	createAndCheckGateway(g, controller, 9999)
}

func TestApplyMetadataNameIncludesNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g,
		schemas.Gateway.MessageName,
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"istio-namespace/some-gateway"},
		setVersion("1"),
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "istio-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message))
}

func TestApplyMetadataNameWithoutNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g,
		schemas.Gateway.MessageName,
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"some-gateway"},
		setVersion("1"),
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message))
}

func TestApplyChangeNoObjects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g,
		schemas.Gateway.MessageName,
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"some-gateway"},
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message))

	change = convertToChange([]proto.Message{},
		[]string{"some-gateway"},
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err = controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(0))
}

func TestApplyConfigUpdate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g,
		schemas.Gateway.MessageName,
		gateway)

	change := convertToChange([]proto.Message{message},
		[]string{"some-gateway"},
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	event := <-fx.Events
	g.Expect(event).To(gomega.Equal("ConfigUpdate"))
}

func TestApplyClusterScopedAuthPolicy(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message0 := convertToResource(g,
		schemas.AuthenticationPolicy.MessageName,
		authnPolicy0)

	message1 := convertToResource(g,
		schemas.AuthenticationMeshPolicy.MessageName,
		authnPolicy1)

	change := convertToChange(
		[]proto.Message{message0},
		[]string{"bar-namespace/foo"},
		setVersion("1"),
		setCollection(schemas.AuthenticationPolicy.Collection),
		setTypeURL(schemas.AuthenticationPolicy.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	change = convertToChange(
		[]proto.Message{message1},
		[]string{"default"},
		setVersion("1"),
		setCollection(schemas.AuthenticationMeshPolicy.Collection),
		setTypeURL(schemas.AuthenticationMeshPolicy.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(schemas.AuthenticationPolicy.Type, "bar-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("foo"))
	g.Expect(c[0].Namespace).To(gomega.Equal("bar-namespace"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.AuthenticationPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message0))

	c, err = controller.List(schemas.AuthenticationMeshPolicy.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("default"))
	g.Expect(c[0].Namespace).To(gomega.Equal(""))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.AuthenticationMeshPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message1))

	// verify the namespace scoped resource can be deleted
	change = convertToChange(
		[]proto.Message{message1},
		[]string{"default"},
		setVersion("1"),
		setCollection(schemas.AuthenticationPolicy.Collection),
		setTypeURL(schemas.AuthenticationPolicy.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err = controller.List(schemas.AuthenticationMeshPolicy.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("default"))
	g.Expect(c[0].Namespace).To(gomega.Equal(""))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.AuthenticationMeshPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message1))

	// verify the namespace scoped resource can be added and mesh-scoped resource removed
	change = convertToChange(
		[]proto.Message{message0},
		[]string{"bar-namespace/foo"},
		setVersion("1"),
		setCollection(schemas.AuthenticationPolicy.Collection),
		setTypeURL(schemas.AuthenticationPolicy.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	change = convertToChange(
		[]proto.Message{},
		[]string{"default"},
		setVersion("1"),
		setCollection(schemas.AuthenticationMeshPolicy.Collection),
		setTypeURL(schemas.AuthenticationMeshPolicy.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err = controller.List(schemas.AuthenticationPolicy.Type, "bar-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("foo"))
	g.Expect(c[0].Namespace).To(gomega.Equal("bar-namespace"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.AuthenticationPolicy.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message0))
}

func TestInvalidResource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	gw := proto.Clone(gateway).(*networking.Gateway)
	gw.Servers[0].Hosts = nil

	message := convertToResource(g, schemas.Gateway.MessageName, gw)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"bar-namespace/foo"},
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.Gateway.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}

func TestInvalidResource_BadTimestamp(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController(testControllerOptions)

	message := convertToResource(g, schemas.Gateway.MessageName, gateway)
	change := convertToChange(
		[]proto.Message{message},
		[]string{"bar-namespace/foo"},
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.MessageName))

	change.Objects[0].Metadata.CreateTime = &types.Timestamp{
		Seconds: -1,
		Nanos:   -1,
	}

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.Gateway.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
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
	controller.RegisterEventHandler(schemas.ServiceEntry.Type, func(m model.Config, e model.Event) {
		gotEvents[e][makeName(m.Namespace, m.Name)] = m
	})

	typeURL := "type.googleapis.com/istio.networking.v1alpha3.ServiceEntry"
	collection := schemas.ServiceEntry.Collection

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
				Type:              schemas.ServiceEntry.Type,
				Group:             schemas.ServiceEntry.Group,
				Version:           schemas.ServiceEntry.Version,
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

func setCollection(collection string) func(*sink.Change) {
	return func(c *sink.Change) {
		c.Collection = collection
	}
}

func setAnnotations(a map[string]string) func(*sink.Change) {
	return func(c *sink.Change) {
		for _, obj := range c.Objects {
			obj.Metadata.Annotations = a
		}
	}
}

func setVersion(v string) func(*sink.Change) {
	return func(c *sink.Change) {
		for _, obj := range c.Objects {
			obj.Metadata.Version = v
		}
	}
}

func setTypeURL(url string) func(*sink.Change) {
	return func(c *sink.Change) {
		for _, obj := range c.Objects {
			obj.TypeURL = url
		}
	}
}

func convertToResource(g *gomega.GomegaWithT, typeURL string, resource proto.Message) (messages proto.Message) {
	marshaled, err := proto.Marshal(resource)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	message, err := makeMessage(marshaled, typeURL)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	return message
}

func convertToResources(g *gomega.GomegaWithT, typeURL string, resources []proto.Message) (messages []proto.Message) {
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
