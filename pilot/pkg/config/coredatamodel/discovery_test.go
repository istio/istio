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

package coredatamodel_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schemas"
)

var (
	d          *coredatamodel.MCPDiscovery
	controller coredatamodel.CoreDataModel
	fx         *FakeXdsUpdater
	namespace  = "random-namespace"
	name       = "test-synthetic-se"
	svcPort    = []*model.Port{
		{
			Name:     "http-port",
			Port:     80,
			Protocol: protocol.Instance("http"),
		},
		{
			Name:     "http-alt-port",
			Port:     8080,
			Protocol: protocol.Instance("http"),
		},
	}
	fakeCreateTime, _ = time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
)

// Since service instance is representation of a service with a corresponding backend port
// the following ServiceEntry plus a notReadyEndpoint 4.4.4.4:5555 should  yield into 10
// service instances once flattened. That is one endpoints IP + endpoint Port, per service Port, per service host.
// However GetProxyServiceInstances should only return all the ones with 4.4.4.4 IP address since they match the
// given proxy address
// e.g:
//	&networking.ServiceEntry{
//		Hosts: []string{"svc.example2.com"},
//		Ports: []*networking.Port{
//			{Number: 80, Name: "http-port", Protocol: "http"},
//			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
//		},
//		Endpoints: []*networking.ServiceEntry_Endpoint{
//			{
//				Address: "2.2.2.2",
//				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
//			},
//			{
//				Address: "3.3.3.3",
//				Ports:   map[string]uint32{"http-port": 1080},
//			},
//			{
//				Address: "4.4.4.4",
//				Ports:   map[string]uint32{"http-port": 1080},
//				Labels:  map[string]string{"foo": "bar"},
//			},
//		},
//	}
// AND
// notReadyEndpoint 4.4.4.4:5555
// should result in the following service instances:
//
// NetworkEndpoint(endpoint{Address:2.2.2.2, Port:18080, servicePort: &{http-port 80 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:2.2.2.2, Port:7080, servicePort: &{http-port 80 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:2.2.2.2, Port:18080, servicePort: &{http-alt-port 8080 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:2.2.2.2, Port:7080, servicePort: &{http-alt-port 8080 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:3.3.3.3, Port:1080, servicePort: &{http-alt-port 8080 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:3.3.3.3, Port:1080, servicePort: &{http-port 80 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:4.4.4.4, Port:5555, servicePort: &{http-port 80 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:4.4.4.4, Port:1080, servicePort: &{http-port 80 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:4.4.4.4, Port:5555, servicePort: &{http-alt-port 8080 http}} service:{Hostname: svc.example2.com})
// NetworkEndpoint(endpoint{Address:4.4.4.4, Port:1080, servicePort: &{http-alt-port 8080 http}} service:{Hostname: svc.example2.com})

func TestGetProxyServiceInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testSetup(g)

	svcInstances, err := d.GetProxyServiceInstances(buildProxy("4.4.4.4"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).To(gomega.Equal(4))

	steps := map[string]struct {
		address     string
		ports       []int
		servicePort []*model.Port
		hostname    host.Name
	}{
		"4.4.4.4": {
			address:     "4.4.4.4",
			ports:       []int{1080, 5555, 8080},
			servicePort: svcPort,
			hostname:    host.Name("svc.example2.com"),
		},
	}

	t.Run("verify service instances", func(_ *testing.T) {
		for _, svcInstance := range svcInstances {
			step := steps[svcInstance.Endpoint.Address]
			g.Expect(step.address).To(gomega.Equal(svcInstance.Endpoint.Address))
			g.Expect(step.hostname).To(gomega.Equal(svcInstance.Service.Hostname))
			g.Expect(step.ports).To(gomega.ContainElement(svcInstance.Endpoint.Port))
		}
	})
}

func TestInstancesByPort(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testSetup(g)
	svc := &model.Service{
		Hostname: host.Name("svc.example2.com"),
	}
	svcInstances, err := d.InstancesByPort(svc, 80, labels.Collection{})
	g.Expect(len(svcInstances)).To(gomega.Equal(7))
	g.Expect(err).ToNot(gomega.HaveOccurred())

	steps := map[string]struct {
		address     string
		ports       []int
		servicePort []*model.Port
		hostname    host.Name
	}{
		"2.2.2.2": {
			address:     "2.2.2.2",
			ports:       []int{7080, 18080},
			servicePort: svcPort,
			hostname:    host.Name("svc.example2.com"),
		},
		"3.3.3.3": {
			address:     "3.3.3.3",
			ports:       []int{1080},
			servicePort: svcPort,
			hostname:    host.Name("svc.example2.com"),
		},
		"4.4.4.4": {
			address:     "4.4.4.4",
			ports:       []int{1080, 5555},
			servicePort: svcPort,
			hostname:    host.Name("svc.example2.com"),
		},
		"6.6.6.6": {
			address:     "6.6.6.6",
			ports:       []int{7777},
			servicePort: svcPort,
			hostname:    host.Name("svc.example2.com"),
		},
		"1.1.1.1": {
			address:     "1.1.1.1",
			ports:       []int{2222},
			servicePort: svcPort,
			hostname:    host.Name("svc.example2.com"),
		},
	}

	t.Run("verify service instances", func(_ *testing.T) {
		for _, svcInstance := range svcInstances {
			switch svcInstance.Endpoint.Address {
			case "2.2.2.2":
				step := steps[svcInstance.Endpoint.Address]
				g.Expect(step.address).To(gomega.Equal(svcInstance.Endpoint.Address))
				g.Expect(step.hostname).To(gomega.Equal(svcInstance.Service.Hostname))
				g.Expect(step.ports).To(gomega.ContainElement(svcInstance.Endpoint.Port))
				g.Expect(step.servicePort).To(gomega.ContainElement(svcInstance.Endpoint.ServicePort))
			case "3.3.3.3":
				step := steps[svcInstance.Endpoint.Address]
				g.Expect(step.address).To(gomega.Equal(svcInstance.Endpoint.Address))
				g.Expect(step.hostname).To(gomega.Equal(svcInstance.Service.Hostname))
				g.Expect(step.ports).To(gomega.ContainElement(svcInstance.Endpoint.Port))
			case "4.4.4.4":
				step := steps[svcInstance.Endpoint.Address]
				g.Expect(step.address).To(gomega.Equal(svcInstance.Endpoint.Address))
				g.Expect(step.hostname).To(gomega.Equal(svcInstance.Service.Hostname))
				g.Expect(step.ports).To(gomega.ContainElement(svcInstance.Endpoint.Port))
			case "6.6.6.6":
				step := steps[svcInstance.Endpoint.Address]
				g.Expect(step.address).To(gomega.Equal(svcInstance.Endpoint.Address))
				g.Expect(step.hostname).To(gomega.Equal(svcInstance.Service.Hostname))
				g.Expect(step.ports).To(gomega.ContainElement(svcInstance.Endpoint.Port))
			case "1.1.1.1":
				step := steps[svcInstance.Endpoint.Address]
				g.Expect(step.address).To(gomega.Equal(svcInstance.Endpoint.Address))
				g.Expect(step.hostname).To(gomega.Equal(svcInstance.Service.Hostname))
				g.Expect(step.ports).To(gomega.ContainElement(svcInstance.Endpoint.Port))
			default:
				t.Fatal("no test step found")
			}
		}
	})
}

func TestItShouldReadFromCache(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	testSetup(g)
	proxy := buildProxy("4.4.4.4")

	svcInstances, err := d.GetProxyServiceInstances(proxy)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).To(gomega.Equal(4))

	// drain the cache
	conf := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schemas.ServiceEntry.Type,
			Group:             schemas.ServiceEntry.Group,
			Version:           schemas.ServiceEntry.Version,
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: fakeCreateTime,
		},
	}
	d.HandleCacheEvents(conf, model.EventDelete)

	svcInstances, err = d.GetProxyServiceInstances(proxy)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).To(gomega.Equal(0))

}

func TestHandleCacheEvents(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	initDiscovery()
	fakeCreateTime, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	conf := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schemas.ServiceEntry.Type,
			Group:             schemas.ServiceEntry.Group,
			Version:           schemas.ServiceEntry.Version,
			Name:              name,
			Namespace:         "default",
			Domain:            "example2.com",
			ResourceVersion:   "1",
			CreationTimestamp: fakeCreateTime,
			Labels:            map[string]string{"lk1": "lv1"},
			Annotations:       map[string]string{"ak1": "av1"},
		},
		Spec: syntheticServiceEntry0,
	}

	// add the first config
	d.HandleCacheEvents(conf, model.EventAdd)

	svcInstances, err := d.GetProxyServiceInstances(buildProxy("4.4.4.4"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).ToNot(gomega.Equal(0))
	for _, s := range svcInstances {
		g.Expect(s.Labels).To(gomega.Equal(labels.Instance{"foo": "bar"}))
	}

	// update the first config
	syntheticServiceEntry0.Endpoints = []*networking.ServiceEntry_Endpoint{
		{
			Address: "3.3.3.3",
			Ports:   map[string]uint32{"http-port": 1080},
		},
		{
			Address: "4.4.4.4",
			Ports:   map[string]uint32{"http-port": 1080},
			Labels:  map[string]string{"foo": "bar2"},
		},
	}
	d.HandleCacheEvents(conf, model.EventUpdate)
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("4.4.4.4"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).ToNot(gomega.Equal(0))
	for _, s := range svcInstances {
		g.Expect(s.Labels).To(gomega.Equal(labels.Instance{"foo": "bar2"}))
	}

	// add another config
	syntheticServiceEntry1.Endpoints = []*networking.ServiceEntry_Endpoint{
		{
			Address: "3.3.3.3",
			Ports:   map[string]uint32{"http-port": 1080},
		},
		{
			Address: "5.5.5.5",
			Ports:   map[string]uint32{"http-port": 1081},
			Labels:  map[string]string{"foo1": "bar1"},
		},
	}
	conf2 := conf
	conf2.Spec = syntheticServiceEntry1
	conf2.ConfigMeta.Name = "test-name"
	conf2.ConfigMeta.Namespace = "test-namespace"
	d.HandleCacheEvents(conf2, model.EventAdd)
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("5.5.5.5"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).ToNot(gomega.Equal(0))
	for _, s := range svcInstances {
		g.Expect(s.Labels).To(gomega.Equal(labels.Instance{"foo1": "bar1"}))
	}

	// add another config in the same namespace as the second one
	syntheticServiceEntry2 := syntheticServiceEntry1
	syntheticServiceEntry2.Endpoints = []*networking.ServiceEntry_Endpoint{
		{
			Address: "2.2.2.2",
			Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
			Labels:  map[string]string{"foo3": "bar3"},
		},
	}

	conf3 := conf
	conf3.Spec = syntheticServiceEntry2
	conf3.ConfigMeta.Name = "test-name2"
	conf3.ConfigMeta.Namespace = "test-namespace"
	d.HandleCacheEvents(conf3, model.EventAdd)
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("2.2.2.2"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).ToNot(gomega.Equal(0))
	for _, s := range svcInstances {
		g.Expect(s.Labels).To(gomega.Equal(labels.Instance{"foo3": "bar3"}))
	}

	// delete the first config
	d.HandleCacheEvents(conf, model.EventDelete)
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("4.4.4.4"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).To(gomega.Equal(0))

	// delete the second config
	d.HandleCacheEvents(conf2, model.EventDelete)
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("5.5.5.5"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).To(gomega.Equal(0))

	// check to see if other config in the same namespace
	// as second config is not deleted
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("2.2.2.2"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).ToNot(gomega.Equal(0))
	for _, s := range svcInstances {
		g.Expect(s.Labels).To(gomega.Equal(labels.Instance{"foo3": "bar3"}))
	}

	// delete the last config
	d.HandleCacheEvents(conf3, model.EventDelete)
	svcInstances, err = d.GetProxyServiceInstances(buildProxy("2.2.2.2"))
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(svcInstances)).To(gomega.Equal(0))
}

func buildProxy(proxyIP string) *model.Proxy {
	return &model.Proxy{
		IPAddresses: []string{proxyIP},
	}
}

func initDiscovery() {
	fx = NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	controller = coredatamodel.NewSyntheticServiceEntryController(testControllerOptions)

	options := &coredatamodel.DiscoveryOptions{
		ClusterID:    "test",
		DomainSuffix: "cluster.local",
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				MixerCheckServer: "mixer",
			},
		},
	}
	d = coredatamodel.NewMCPDiscovery(controller, options)

}

func testSetup(g *gomega.GomegaWithT) {
	initDiscovery()

	message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message},
		[]string{fmt.Sprintf("%s/%s", namespace, name)},
		setAnnotations(map[string]string{
			"networking.alpha.istio.io/notReadyEndpoints": "1.1.1.1:2222,4.4.4.4:5555,6.6.6.6:7777",
		}),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

}
