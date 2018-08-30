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
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	mcpapi "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/model"
	mcpclient "istio.io/istio/pkg/mcp/client"
)

var (
	baseURL = "type.googleapis.com/"

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
)

func TestHasSynced(t *testing.T) {
	t.Skip("Pending: https://github.com/istio/istio/issues/7947")
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	g.Expect(controller.HasSynced()).To(gomega.BeFalse())
}

func TestConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	descriptors := controller.ConfigDescriptor()
	g.Expect(descriptors).To(gomega.Equal(model.IstioConfigTypes))
}

func TestListInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	c, err := controller.List("bad-type", "some-phony-name-space.com")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("list unknown type"))
}

func TestListCorrectTypeNoData(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	c, err := controller.List("virtual-service", "some-phony-name-space.com")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
}

func TestListAllNameSpace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	messages := gatewayEnvelope(g, []*networking.Gateway{gateway, gateway2, gateway3})
	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convert(
		[]proto.Message{message, message2, message3},
		[]string{"some-gateway1/namespace1", "some-other-gateway/default", "some-other-gateway3"},
		model.Gateway.MessageName)

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
	controller := coredatamodel.NewController()

	messages := gatewayEnvelope(g, []*networking.Gateway{gateway, gateway2, gateway3})
	message, message2, message3 := messages[0], messages[1], messages[2]

	change := convert(
		[]proto.Message{message, message2, message3},
		[]string{"some-gateway1/namespace1", "some-other-gateway/default", "some-other-gateway3/namespace1"},
		model.Gateway.MessageName)

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
	controller := coredatamodel.NewController()

	message := gatewayEnvelope(g, []*networking.Gateway{gateway})
	change := convert([]proto.Message{message[0]}, []string{"some-gateway"}, "bad-type")

	err := controller.Apply(change)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestApplyValidTypeWithNoBaseURL(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

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

		change := convert([]proto.Message{message}, []string{"some-gateway"}, model.Gateway.MessageName)
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
	controller := coredatamodel.NewController()

	message := gatewayEnvelope(g, []*networking.Gateway{gateway})

	change := convert([]proto.Message{message[0]}, []string{"some-gateway/istio-namespace"}, model.Gateway.MessageName)
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
	controller := coredatamodel.NewController()

	message := gatewayEnvelope(g, []*networking.Gateway{gateway})

	change := convert([]proto.Message{message[0]}, []string{"some-gateway"}, model.Gateway.MessageName)
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
	controller := coredatamodel.NewController()

	message := gatewayEnvelope(g, []*networking.Gateway{gateway})
	change := convert([]proto.Message{message[0]}, []string{"some-gateway"}, model.Gateway.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("some-gateway"))
	g.Expect(c[0].Type).To(gomega.Equal(model.Gateway.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message[0]))

	change = convert([]proto.Message{}, []string{"some-gateway"}, model.Gateway.MessageName)

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err = controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(0))
}

var (
	serviceEntry = &networking.ServiceEntry{
		Hosts:     []string{"something.example.com"},
		Addresses: []string{"172.217.0.0"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	}

	serviceEntry2 = &networking.ServiceEntry{
		Hosts: []string{"other.example.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "172.217.0.1",
				Ports:   map[string]uint32{"http-port": 8080},
				Labels:  map[string]string{"foo": "bar"},
			},
		},
	}
)

func TestGetService(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	messages := serviceEntryEnvelope(g, []*networking.ServiceEntry{serviceEntry, serviceEntry2})
	message, message2 := messages[0], messages[1]
	change := convert([]proto.Message{message, message2}, []string{"some-service-entry/yolo", "other-service-entry"}, model.ServiceEntry.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	service, err := controller.GetService(model.Hostname("something.example.com"))
	g.Expect(err).ToNot(gomega.HaveOccurred())

	g.Expect(service.Hostname).To(gomega.Equal(model.Hostname(serviceEntry.Hosts[0])))
	g.Expect(service.Address).To(gomega.Equal("172.217.0.0"))
	g.Expect(service.Ports[0]).To(gomega.Equal(convertPort(serviceEntry.Ports[0])))
	g.Expect(service.Resolution).To(gomega.Equal(model.DNSLB))
	g.Expect(service.MeshExternal).To(gomega.BeFalse())
	g.Expect(service.Attributes.Name).To(gomega.Equal(serviceEntry.Hosts[0]))
	g.Expect(service.Attributes.Namespace).To(gomega.Equal("yolo"))

	service, err = controller.GetService(model.Hostname("other.example.com"))
	g.Expect(err).ToNot(gomega.HaveOccurred())

	g.Expect(service.Hostname).To(gomega.Equal(model.Hostname(serviceEntry2.Hosts[0])))
	g.Expect(service.Address).To(gomega.Equal("0.0.0.0"))
	g.Expect(service.Ports[0]).To(gomega.Equal(convertPort(serviceEntry2.Ports[0])))
	g.Expect(service.Resolution).To(gomega.Equal(model.ClientSideLB))
	g.Expect(service.MeshExternal).To(gomega.BeTrue())
	g.Expect(service.Attributes.Name).To(gomega.Equal(serviceEntry2.Hosts[0]))
	g.Expect(service.Attributes.Namespace).To(gomega.Equal(""))
}

func TestServices(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	messages := serviceEntryEnvelope(g, []*networking.ServiceEntry{serviceEntry, serviceEntry2})
	message, message2 := messages[0], messages[1]
	change := convert([]proto.Message{message, message2}, []string{"some-service-entry/yolo", "other-service-entry"}, model.ServiceEntry.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	services, err := controller.Services()
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(services)).To(gomega.Equal(2))

	for _, service := range services {
		if service.Hostname == model.Hostname("something.example.com") {
			g.Expect(service.Address).To(gomega.Equal("172.217.0.0"))
			g.Expect(service.Ports[0]).To(gomega.Equal(convertPort(serviceEntry.Ports[0])))
			g.Expect(service.Resolution).To(gomega.Equal(model.DNSLB))
			g.Expect(service.MeshExternal).To(gomega.BeFalse())
			g.Expect(service.Attributes.Name).To(gomega.Equal(serviceEntry.Hosts[0]))
			g.Expect(service.Attributes.Namespace).To(gomega.Equal("yolo"))
		} else {
			g.Expect(service.Hostname).To(gomega.Equal(model.Hostname("other.example.com")))
			g.Expect(service.Address).To(gomega.Equal("0.0.0.0"))
			g.Expect(service.Ports[0]).To(gomega.Equal(convertPort(serviceEntry2.Ports[0])))
			g.Expect(service.Resolution).To(gomega.Equal(model.ClientSideLB))
			g.Expect(service.MeshExternal).To(gomega.BeTrue())
			g.Expect(service.Attributes.Name).To(gomega.Equal(serviceEntry2.Hosts[0]))
			g.Expect(service.Attributes.Namespace).To(gomega.Equal(""))
		}
	}
}

func TestInstancesByPort(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()
	serviceEntry := tcpDNS
	serviceEntry2 := httpDNS

	messages := serviceEntryEnvelope(g, []*networking.ServiceEntry{serviceEntry, serviceEntry2})
	change := convert(messages, []string{"some-service-entry", "other-service-entry"}, model.ServiceEntry.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	actualServiceInstances, err := controller.InstancesByPort("tcpdns.com", 444, nil)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(actualServiceInstances)).To(gomega.Equal(2))

	port := &networking.Port{
		Number:   444,
		Name:     "tcp-444",
		Protocol: "tcp",
	}
	instanceOne := makeInstance(serviceEntry, "lon.google.com", 444, port, nil, time.Now())
	instanceTwo := makeInstance(serviceEntry, "in.google.com", 444, port, nil, time.Now())
	expectedServiceInstances := []*model.ServiceInstance{instanceOne, instanceTwo}

	sortServiceInstances(actualServiceInstances)
	sortServiceInstances(expectedServiceInstances)
	for i, actualInstance := range actualServiceInstances {
		g.Expect(actualInstance.Endpoint).To(gomega.Equal(expectedServiceInstances[i].Endpoint))
		g.Expect(actualInstance.Labels).To(gomega.BeNil())
		g.Expect(actualInstance.Service.Hostname).To(gomega.Equal(model.Hostname(serviceEntry.Hosts[0])))
		g.Expect(actualInstance.Service.Ports[0]).To(gomega.Equal(convertPort(serviceEntry.Ports[0])))
		g.Expect(actualInstance.Service.Resolution).To(gomega.Equal(model.DNSLB))
		g.Expect(actualInstance.Service.MeshExternal).To(gomega.BeTrue())
		g.Expect(actualInstance.Service.Attributes.Name).To(gomega.Equal(serviceEntry.Hosts[0]))
		g.Expect(actualInstance.Service.Attributes.Namespace).To(gomega.Equal(""))
		g.Expect(actualInstance.Service.Address).To(gomega.Equal(expectedServiceInstances[i].Service.Address))
	}
}

func TestGetProxyServiceInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := coredatamodel.NewController()

	messages := serviceEntryEnvelope(g, []*networking.ServiceEntry{httpStatic, tcpStatic})
	change := convert(messages, []string{"some-service-entry", "other-service-entry"}, model.ServiceEntry.MessageName)

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	instances, err := controller.GetProxyServiceInstances(&model.Proxy{IPAddress: "2.2.2.2"})
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(instances)).To(gomega.Equal(3))

	for _, instance := range instances {
		if instance.Endpoint.Port == 444 {
			g.Expect(instance.Service.Hostname).To(gomega.Equal(model.Hostname(tcpStatic.Hosts[0])))
		} else {
			g.Expect(instance.Service.Hostname).To(gomega.Equal(model.Hostname(httpStatic.Hosts[0])))
		}
	}
}

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: model.ParseProtocol(port.Protocol),
	}
}

func convert(resources []proto.Message, names []string, responseMessageName string) *mcpclient.Change {
	out := new(mcpclient.Change)
	out.TypeURL = responseMessageName
	for i, res := range resources {
		out.Objects = append(out.Objects,
			&mcpclient.Object{
				TypeURL: responseMessageName,
				Metadata: &mcpapi.Metadata{
					Name: names[i],
				},
				Resource: res,
			},
		)
	}
	return out
}

func gatewayEnvelope(g *gomega.GomegaWithT, gateways []*networking.Gateway) (messages []proto.Message) {
	for _, gateway := range gateways {
		marshaledGateway, err := proto.Marshal(gateway)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		message, err := makeMessage(marshaledGateway, model.Gateway.MessageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		messages = append(messages, message)
	}
	return messages
}

func serviceEntryEnvelope(g *gomega.GomegaWithT, serviceEntries []*networking.ServiceEntry) (messages []proto.Message) {
	for _, serviceEntry := range serviceEntries {
		marshaledServiceEntry, err := proto.Marshal(serviceEntry)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		message, err := makeMessage(marshaledServiceEntry, model.ServiceEntry.MessageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		messages = append(messages, message)
	}
	return messages
}

func makeMessage(value []byte, responseMessageName string) (proto.Message, error) {
	resource := &google_protobuf.Any{
		TypeUrl: fmt.Sprintf("type.googleapis.com/%s", responseMessageName),
		Value:   value,
	}

	var dynamicAny google_protobuf.DynamicAny
	err := google_protobuf.UnmarshalAny(resource, &dynamicAny)
	if err == nil {
		return dynamicAny.Message, nil
	}

	return nil, err
}
