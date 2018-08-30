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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/model"
)

var (
	defaultNamespace = ""
)

var httpNone = &networking.ServiceEntry{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-number", Protocol: "http"},
		{Number: 8080, Name: "http2-number", Protocol: "http2"},
	},
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_NONE,
}

var tcpNone = &networking.ServiceEntry{
	Hosts:     []string{"tcpnone.com"},
	Addresses: []string{"172.217.0.0/16"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_NONE,
}

var httpStatic = &networking.ServiceEntry{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-port", Protocol: "http"},
		{Number: 8080, Name: "http-alt-port", Protocol: "http"},
	},
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
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_STATIC,
}

var httpDNSnoEndpoints = &networking.ServiceEntry{
	Hosts: []string{"google.com", "www.wikipedia.org"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-port", Protocol: "http"},
		{Number: 8080, Name: "http-alt-port", Protocol: "http"},
	},
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_DNS,
}

var httpDNS = &networking.ServiceEntry{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-port", Protocol: "http"},
		{Number: 8080, Name: "http-alt-port", Protocol: "http"},
	},
	Endpoints: []*networking.ServiceEntry_Endpoint{
		{
			Address: "us.google.com",
			Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
		},
		{
			Address: "uk.google.com",
			Ports:   map[string]uint32{"http-port": 1080},
		},
		{
			Address: "de.google.com",
			Labels:  map[string]string{"foo": "bar"},
		},
	},
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_DNS,
}

var tcpDNS = &networking.ServiceEntry{
	Hosts: []string{"tcpdns.com"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Endpoints: []*networking.ServiceEntry_Endpoint{
		{
			Address: "lon.google.com",
		},
		{
			Address: "in.google.com",
		},
	},
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_DNS,
}

var tcpStatic = &networking.ServiceEntry{
	Hosts:     []string{"tcpstatic.com"},
	Addresses: []string{"172.217.0.1"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Endpoints: []*networking.ServiceEntry_Endpoint{
		{
			Address: "1.1.1.1",
		},
		{
			Address: "2.2.2.2",
		},
	},
	Location:   networking.ServiceEntry_MESH_EXTERNAL,
	Resolution: networking.ServiceEntry_STATIC,
}

var httpNoneInternal = &networking.ServiceEntry{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-number", Protocol: "http"},
		{Number: 8080, Name: "http2-number", Protocol: "http2"},
	},
	Location:   networking.ServiceEntry_MESH_INTERNAL,
	Resolution: networking.ServiceEntry_NONE,
}

var tcpNoneInternal = &networking.ServiceEntry{
	Hosts:     []string{"tcpinternal.com"},
	Addresses: []string{"172.217.0.0/16"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Location:   networking.ServiceEntry_MESH_INTERNAL,
	Resolution: networking.ServiceEntry_NONE,
}

var multiAddrInternal = &networking.ServiceEntry{
	Hosts:     []string{"tcp1.com", "tcp2.com"},
	Addresses: []string{"1.1.1.0/16", "2.2.2.0/16"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Location:   networking.ServiceEntry_MESH_INTERNAL,
	Resolution: networking.ServiceEntry_NONE,
}

var udsLocal = &networking.ServiceEntry{
	Hosts: []string{"uds.cluster.local"},
	Ports: []*networking.Port{
		{Number: 6553, Name: "grpc-1", Protocol: "grpc"},
	},
	Endpoints: []*networking.ServiceEntry_Endpoint{
		{Address: "unix:///test/sock"},
	},
	Resolution: networking.ServiceEntry_STATIC,
}

func TestConvertService(t *testing.T) {
	tnow := time.Now()
	serviceTests := []struct {
		serviceEntryInput *networking.ServiceEntry
		expectedServices  []*model.Service
	}{
		{
			// service entry http
			serviceEntryInput: httpNone,
			expectedServices: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-number": 80, "http2-number": 8080}, true, model.Passthrough, tnow),
			},
		},
		{
			// service entry tcp
			serviceEntryInput: tcpNone,
			expectedServices: []*model.Service{makeService("tcpnone.com", "172.217.0.0/16",
				map[string]int{"tcp-444": 444}, true, model.Passthrough, tnow),
			},
		},
		{
			// service entry http  static
			serviceEntryInput: httpStatic,
			expectedServices: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.ClientSideLB, tnow),
			},
		},
		{
			// service entry DNS with no endpoints
			serviceEntryInput: httpDNSnoEndpoints,
			expectedServices: []*model.Service{
				makeService("google.com", model.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
				makeService("www.wikipedia.org", model.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
			},
		},
		{
			// service entry dns
			serviceEntryInput: httpDNS,
			expectedServices: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
			},
		},
		{
			// service entry tcp DNS
			serviceEntryInput: tcpDNS,
			expectedServices: []*model.Service{makeService("tcpdns.com", model.UnspecifiedIP,
				map[string]int{"tcp-444": 444}, true, model.DNSLB, tnow),
			},
		},
		{
			// service entry tcp static
			serviceEntryInput: tcpStatic,
			expectedServices: []*model.Service{makeService("tcpstatic.com", "172.217.0.1",
				map[string]int{"tcp-444": 444}, true, model.ClientSideLB, tnow),
			},
		},
		{
			// service entry http internal
			serviceEntryInput: httpNoneInternal,
			expectedServices: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-number": 80, "http2-number": 8080}, false, model.Passthrough, tnow),
			},
		},
		{
			// service entry tcp internal
			serviceEntryInput: tcpNoneInternal,
			expectedServices: []*model.Service{makeService("tcpinternal.com", "172.217.0.0/16",
				map[string]int{"tcp-444": 444}, false, model.Passthrough, tnow),
			},
		},
		{
			// service entry multiAddrInternal
			serviceEntryInput: multiAddrInternal,
			expectedServices: []*model.Service{
				makeService("tcp1.com", "1.1.1.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough, tnow),
				makeService("tcp1.com", "2.2.2.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough, tnow),
				makeService("tcp2.com", "1.1.1.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough, tnow),
				makeService("tcp2.com", "2.2.2.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough, tnow),
			},
		},
	}

	g := gomega.NewGomegaWithT(t)
	for _, tt := range serviceTests {
		t.Log(strings.Join(tt.serviceEntryInput.Hosts, "_"))
		convertedService := coredatamodel.ConvertServices(tt.serviceEntryInput, defaultNamespace, tnow)
		compareServices(g, tt.serviceEntryInput, convertedService, tt.expectedServices)
	}
}

func TestConvertInstances(t *testing.T) {
	tnow := time.Now()
	serviceInstanceTests := []struct {
		serviceEntryInput       *networking.ServiceEntry
		expectedServiceInstance []*model.ServiceInstance
	}{
		{
			// single instance with multiple ports
			serviceEntryInput: httpNone,
			// DNS type none means service should not have a registered instance
			expectedServiceInstance: []*model.ServiceInstance{},
		},
		{
			// service entry tcp
			serviceEntryInput: tcpNone,
			// DNS type none means service should not have a registered instance
			expectedServiceInstance: []*model.ServiceInstance{},
		},
		{
			// service entry static
			serviceEntryInput: httpStatic,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(httpStatic, "2.2.2.2", 7080, httpStatic.Ports[0], nil, tnow),
				makeInstance(httpStatic, "2.2.2.2", 18080, httpStatic.Ports[1], nil, tnow),
				makeInstance(httpStatic, "3.3.3.3", 1080, httpStatic.Ports[0], nil, tnow),
				makeInstance(httpStatic, "3.3.3.3", 8080, httpStatic.Ports[1], nil, tnow),
				makeInstance(httpStatic, "4.4.4.4", 1080, httpStatic.Ports[0], map[string]string{"foo": "bar"}, tnow),
				makeInstance(httpStatic, "4.4.4.4", 8080, httpStatic.Ports[1], map[string]string{"foo": "bar"}, tnow),
			},
		},
		{
			// service entry DNS with no endpoints
			serviceEntryInput: httpDNSnoEndpoints,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(httpDNSnoEndpoints, "google.com", 80, httpDNSnoEndpoints.Ports[0], nil, tnow),
				makeInstance(httpDNSnoEndpoints, "google.com", 8080, httpDNSnoEndpoints.Ports[1], nil, tnow),
				makeInstance(httpDNSnoEndpoints, "www.wikipedia.org", 80, httpDNSnoEndpoints.Ports[0], nil, tnow),
				makeInstance(httpDNSnoEndpoints, "www.wikipedia.org", 8080, httpDNSnoEndpoints.Ports[1], nil, tnow),
			},
		},
		{
			// service entry dns
			serviceEntryInput: httpDNS,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Ports[0], nil, tnow),
				makeInstance(httpDNS, "us.google.com", 18080, httpDNS.Ports[1], nil, tnow),
				makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Ports[0], nil, tnow),
				makeInstance(httpDNS, "uk.google.com", 8080, httpDNS.Ports[1], nil, tnow),
				makeInstance(httpDNS, "de.google.com", 80, httpDNS.Ports[0], map[string]string{"foo": "bar"}, tnow),
				makeInstance(httpDNS, "de.google.com", 8080, httpDNS.Ports[1], map[string]string{"foo": "bar"}, tnow),
			},
		},
		{
			// service entry tcp DNS
			serviceEntryInput: tcpDNS,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Ports[0], nil, tnow),
				makeInstance(tcpDNS, "in.google.com", 444, tcpDNS.Ports[0], nil, tnow),
			},
		},
		{
			// service entry tcp static
			serviceEntryInput: tcpStatic,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(tcpStatic, "1.1.1.1", 444, tcpStatic.Ports[0], nil, tnow),
				makeInstance(tcpStatic, "2.2.2.2", 444, tcpStatic.Ports[0], nil, tnow),
			},
		},
		{
			// service entry unix domain socket static
			serviceEntryInput: udsLocal,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(udsLocal, "/test/sock", 0, udsLocal.Ports[0], nil, tnow),
			},
		},
	}

	g := gomega.NewGomegaWithT(t)
	for _, tt := range serviceInstanceTests {
		t.Log(strings.Join(tt.serviceEntryInput.Hosts, "_"))
		instances := coredatamodel.ConvertInstances(tt.serviceEntryInput, defaultNamespace, tnow)
		sortServiceInstances(instances)
		sortServiceInstances(tt.expectedServiceInstance)
		compareServiceInstances(g, instances, tt.expectedServiceInstance)
	}
}

func TestConvertInstancesFilter(t *testing.T) {
	tnow := time.Now()
	serviceInstanceTests := []struct {
		serviceEntryInput       *networking.ServiceEntry
		expectedServiceInstance []*model.ServiceInstance
	}{
		{
			// service entry static
			serviceEntryInput: httpStatic,
			expectedServiceInstance: []*model.ServiceInstance{
				makeInstance(httpStatic, "3.3.3.3", 1080, httpStatic.Ports[0], nil, tnow),
				makeInstance(httpStatic, "4.4.4.4", 1080, httpStatic.Ports[0], map[string]string{"foo": "bar"}, tnow),
			},
		},
	}

	filter := func(instance *model.ServiceInstance) bool {
		return instance.Endpoint.Port == 1080
	}

	g := gomega.NewGomegaWithT(t)
	for _, tt := range serviceInstanceTests {
		t.Log(strings.Join(tt.serviceEntryInput.Hosts, "_"))
		instances := coredatamodel.ConvertInstances(tt.serviceEntryInput, defaultNamespace, tnow, filter)
		sortServiceInstances(instances)
		sortServiceInstances(tt.expectedServiceInstance)
		compareServiceInstances(g, instances, tt.expectedServiceInstance)
	}
}

func compareServices(g *gomega.GomegaWithT, serviceEntry *networking.ServiceEntry, convertedServices, expectedServices []*model.Service) {
	sortServices(convertedServices)
	sortServices(expectedServices)

	for i, service := range convertedServices {
		g.Expect(service.Hostname).To(gomega.Equal(expectedServices[i].Hostname))
		g.Expect(expectedServices[i].Address).Should(gomega.ContainSubstring(service.Address))
		g.Expect(service.Ports).To(gomega.Equal(expectedServices[i].Ports))
		g.Expect(service.Resolution).To(gomega.Equal(coredatamodel.ServiceResolutionMapping[serviceEntry.Resolution]))
		g.Expect(service.MeshExternal).To(gomega.Equal(expectedServices[i].MeshExternal))
		g.Expect(service.Attributes.Name).Should(gomega.Equal(expectedServices[i].Attributes.Name))
		g.Expect(service.Attributes.Namespace).To(gomega.Equal(defaultNamespace))
	}
}

func compareServiceInstances(g *gomega.GomegaWithT, actualServiceInstances, expectedServiceInstances []*model.ServiceInstance) {
	g.Expect(len(actualServiceInstances)).To(gomega.Equal(len(expectedServiceInstances)))
	for i, actualInstance := range actualServiceInstances {
		g.Expect(actualInstance.Endpoint).To(gomega.Equal(expectedServiceInstances[i].Endpoint))
		g.Expect(actualInstance.Labels).To(gomega.Equal(expectedServiceInstances[i].Labels))
		g.Expect(actualInstance.Service.Hostname).To(gomega.Equal(expectedServiceInstances[i].Service.Hostname))
		g.Expect(actualInstance.Service.Ports).To(gomega.Equal(expectedServiceInstances[i].Service.Ports))
		g.Expect(actualInstance.Service.Resolution).To(gomega.Equal(expectedServiceInstances[i].Service.Resolution))
		g.Expect(actualInstance.Service.MeshExternal).To(gomega.Equal(expectedServiceInstances[i].Service.MeshExternal))
		g.Expect(actualInstance.Service.Attributes.Name).To(gomega.Equal(expectedServiceInstances[i].Service.Attributes.Name))
		g.Expect(actualInstance.Service.Attributes.Namespace).To(gomega.Equal(defaultNamespace))
		g.Expect(actualInstance.Service.Address).To(gomega.Equal(expectedServiceInstances[i].Service.Address))
	}
}

func sortServiceInstances(instances []*model.ServiceInstance) {
	labelsToSlice := func(labels model.Labels) []string {
		out := make([]string, 0, len(labels))
		for k, v := range labels {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(out)
		return out
	}

	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Service.Hostname == instances[j].Service.Hostname {
			if instances[i].Endpoint.Port == instances[j].Endpoint.Port {
				if instances[i].Endpoint.Address == instances[j].Endpoint.Address {
					if len(instances[i].Labels) == len(instances[j].Labels) {
						iLabels := labelsToSlice(instances[i].Labels)
						jLabels := labelsToSlice(instances[j].Labels)
						for k := range iLabels {
							if iLabels[k] < jLabels[k] {
								return true
							}
						}
					}
					return len(instances[i].Labels) < len(instances[j].Labels)
				}
				return instances[i].Endpoint.Address < instances[j].Endpoint.Address
			}
			return instances[i].Endpoint.Port < instances[j].Endpoint.Port
		}
		return instances[i].Service.Hostname < instances[j].Service.Hostname
	})
}

func sortServices(services []*model.Service) {
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })
	for _, service := range services {
		sortPorts(service.Ports)
	}
}

func sortPorts(ports []*model.Port) {
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port == ports[j].Port {
			if ports[i].Name == ports[j].Name {
				return ports[i].Protocol < ports[j].Protocol
			}
			return ports[i].Name < ports[j].Name
		}
		return ports[i].Port < ports[j].Port
	})
}

func makeService(hostname model.Hostname, address string, ports map[string]int, external bool, resolution model.Resolution,
	creationTime time.Time) *model.Service {

	svc := &model.Service{
		CreationTime: creationTime,
		Hostname:     hostname,
		Address:      address,
		MeshExternal: external,
		Resolution:   resolution,
		Attributes: model.ServiceAttributes{
			Name:      string(hostname),
			Namespace: "default",
		},
	}

	svcPorts := make(model.PortList, 0, len(ports))
	for name, port := range ports {
		svcPort := &model.Port{
			Name:     name,
			Port:     port,
			Protocol: convertPortNameToProtocol(name),
		}
		svcPorts = append(svcPorts, svcPort)
	}

	sortPorts(svcPorts)
	svc.Ports = svcPorts

	return svc
}

func convertPortNameToProtocol(name string) model.Protocol {
	prefix := name
	i := strings.Index(name, "-")
	if i >= 0 {
		prefix = name[:i]
	}
	return model.ParseProtocol(prefix)
}

func makeInstance(serviceEntry *networking.ServiceEntry, address string, port int,
	svcPort *networking.Port, labels map[string]string, creationTime time.Time) *model.ServiceInstance {
	family := model.AddressFamilyTCP
	if port == 0 {
		family = model.AddressFamilyUnix
	}

	services := coredatamodel.ConvertServices(serviceEntry, "", creationTime)
	svc := services[0] // default
	for _, s := range services {
		if string(s.Hostname) == address {
			svc = s
			break
		}
	}
	return &model.ServiceInstance{
		Service: svc,
		Endpoint: model.NetworkEndpoint{
			Family:  family,
			Address: address,
			Port:    port,
			ServicePort: &model.Port{
				Name:     svcPort.Name,
				Port:     int(svcPort.Number),
				Protocol: model.ParseProtocol(svcPort.Protocol),
			},
		},
		Labels: model.Labels(labels),
	}
}
