// Copyright 2017 Istio Authors
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

package external

import (
	"encoding/json"
	"strings"
	"testing"

	mesh "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
)

var httpNone = &networking.ExternalService{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-number", Protocol: "http"},
		{Number: 8080, Name: "http2-number", Protocol: "http2"},
	},
	Discovery: networking.ExternalService_NONE,
}

var tcpNone = &networking.ExternalService{
	Hosts: []string{"172.217.0.0/16"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Discovery: networking.ExternalService_NONE,
}

var httpStatic = &networking.ExternalService{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-port", Protocol: "http"},
		{Number: 8080, Name: "http-alt-port", Protocol: "http"},
	},
	Endpoints: []*networking.ExternalService_Endpoint{
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
	Discovery: networking.ExternalService_STATIC,
}

var httpDNSnoEndpoints = &networking.ExternalService{
	Hosts: []string{"google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-port", Protocol: "http"},
		{Number: 8080, Name: "http-alt-port", Protocol: "http"},
	},

	Discovery: networking.ExternalService_DNS,
}

var httpDNS = &networking.ExternalService{
	Hosts: []string{"*.google.com"},
	Ports: []*networking.Port{
		{Number: 80, Name: "http-port", Protocol: "http"},
		{Number: 8080, Name: "http-alt-port", Protocol: "http"},
	},
	Endpoints: []*networking.ExternalService_Endpoint{
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
	Discovery: networking.ExternalService_DNS,
}

var tcpDNS = &networking.ExternalService{
	Hosts: []string{"172.217.0.0/16"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Endpoints: []*networking.ExternalService_Endpoint{
		{
			Address: "lon.google.com",
		},
		{
			Address: "in.google.com",
		},
	},
	Discovery: networking.ExternalService_DNS,
}

var tcpStatic = &networking.ExternalService{
	Hosts: []string{"172.217.0.0/16"},
	Ports: []*networking.Port{
		{Number: 444, Name: "tcp-444", Protocol: "tcp"},
	},
	Endpoints: []*networking.ExternalService_Endpoint{
		{
			Address: "1.1.1.1",
		},
		{
			Address: "2.2.2.2",
		},
	},
	Discovery: networking.ExternalService_STATIC,
}

func convertPortNameToProtocol(name string) model.Protocol {
	prefix := name
	i := strings.Index(name, "-")
	if i >= 0 {
		prefix = name[:i]
	}
	return model.ConvertCaseInsensitiveStringToProtocol(prefix)
}

func makeService(hostname string, ports map[string]int, resolution model.Resolution) *model.Service {
	svc := &model.Service{
		Hostname:     hostname,
		MeshExternal: true,
		Resolution:   resolution,
	}

	svcPorts := make(model.PortList, 0, len(ports))
	for name, port := range ports {
		svcPort := &model.Port{
			Name:                 name,
			Port:                 port,
			Protocol:             convertPortNameToProtocol(name),
			AuthenticationPolicy: mesh.AuthenticationPolicy_NONE,
		}
		svcPorts = append(svcPorts, svcPort)
	}

	sortPorts(svcPorts)
	svc.Ports = svcPorts

	return svc
}

func makeInstance(externalSvc *networking.ExternalService, address string, port int,
	svcPort *networking.Port, labels map[string]string) *model.ServiceInstance {
	return &model.ServiceInstance{
		Service: convertService(externalSvc)[0],
		Endpoint: model.NetworkEndpoint{
			Address: address,
			Port:    port,
			ServicePort: &model.Port{
				Name:                 svcPort.Name,
				Port:                 int(svcPort.Number),
				Protocol:             convertProtocol(svcPort.Protocol),
				AuthenticationPolicy: mesh.AuthenticationPolicy_NONE,
			},
		},
		Labels: model.Labels(labels),
	}
}

func TestConvertService(t *testing.T) {
	serviceTests := []struct {
		externalSvc *networking.ExternalService
		services    []*model.Service
	}{
		{
			// external service http
			externalSvc: httpNone,
			services: []*model.Service{makeService("*.google.com",
				map[string]int{"http-number": 80, "http2-number": 8080}, model.Passthrough),
			},
		},
		{
			// external service tcp
			externalSvc: tcpNone,
			services: []*model.Service{makeService("172.217.0.0/16",
				map[string]int{"tcp-444": 444}, model.Passthrough),
			},
		},
		{
			// external service http  static
			externalSvc: httpStatic,
			services: []*model.Service{makeService("*.google.com",
				map[string]int{"http-port": 80, "http-alt-port": 8080}, model.ClientSideLB),
			},
		},
		{
			// external service DNS with no endpoints
			externalSvc: httpDNSnoEndpoints,
			services: []*model.Service{makeService("google.com",
				map[string]int{"http-port": 80, "http-alt-port": 8080}, model.DNSLB),
			},
		},
		{
			// external service dns
			externalSvc: httpDNS,
			services: []*model.Service{makeService("*.google.com",
				map[string]int{"http-port": 80, "http-alt-port": 8080}, model.DNSLB),
			},
		},
		{
			// external service tcp DNS
			externalSvc: tcpDNS,
			services: []*model.Service{makeService("172.217.0.0/16",
				map[string]int{"tcp-444": 444}, model.DNSLB),
			},
		},
		{
			// external service tcp static
			externalSvc: tcpStatic,
			services: []*model.Service{makeService("172.217.0.0/16",
				map[string]int{"tcp-444": 444}, model.ClientSideLB),
			},
		},
	}

	for _, tt := range serviceTests {
		services := convertService(tt.externalSvc)
		if err := compare(t, services, tt.services); err != nil {
			t.Error(err)
		}
	}
}

func TestConvertInstances(t *testing.T) {
	serviceInstanceTests := []struct {
		externalSvc *networking.ExternalService
		out         []*model.ServiceInstance
	}{
		{
			// single instance with multiple ports
			externalSvc: httpNone,
			// DNS type none means service should not have a registered instance
			out: []*model.ServiceInstance{},
		},
		{
			// external service tcp
			externalSvc: tcpNone,
			// DNS type none means service should not have a registered instance
			out: []*model.ServiceInstance{},
		},
		{
			// external service static
			externalSvc: httpStatic,
			out: []*model.ServiceInstance{
				makeInstance(httpStatic, "2.2.2.2", 7080, httpStatic.Ports[0], nil),
				makeInstance(httpStatic, "2.2.2.2", 18080, httpStatic.Ports[1], nil),
				makeInstance(httpStatic, "3.3.3.3", 1080, httpStatic.Ports[0], nil),
				makeInstance(httpStatic, "3.3.3.3", 8080, httpStatic.Ports[1], nil),
				makeInstance(httpStatic, "4.4.4.4", 1080, httpStatic.Ports[0], map[string]string{"foo": "bar"}),
				makeInstance(httpStatic, "4.4.4.4", 8080, httpStatic.Ports[1], map[string]string{"foo": "bar"}),
			},
		},
		{
			// external service DNS with no endpoints
			externalSvc: httpDNSnoEndpoints,
			out:         []*model.ServiceInstance{},
		},
		{
			// external service dns
			externalSvc: httpDNS,
			out: []*model.ServiceInstance{
				makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Ports[0], nil),
				makeInstance(httpDNS, "us.google.com", 18080, httpDNS.Ports[1], nil),
				makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Ports[0], nil),
				makeInstance(httpDNS, "uk.google.com", 8080, httpDNS.Ports[1], nil),
				makeInstance(httpDNS, "de.google.com", 80, httpDNS.Ports[0], map[string]string{"foo": "bar"}),
				makeInstance(httpDNS, "de.google.com", 8080, httpDNS.Ports[1], map[string]string{"foo": "bar"}),
			},
		},
		{
			// external service tcp DNS
			externalSvc: tcpDNS,
			out: []*model.ServiceInstance{
				makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Ports[0], nil),
				makeInstance(tcpDNS, "in.google.com", 444, tcpDNS.Ports[0], nil),
			},
		},
		{
			// external service tcp static
			externalSvc: tcpStatic,
			out: []*model.ServiceInstance{
				makeInstance(tcpStatic, "1.1.1.1", 444, tcpStatic.Ports[0], nil),
				makeInstance(tcpStatic, "2.2.2.2", 444, tcpStatic.Ports[0], nil),
			},
		},
	}

	for _, tt := range serviceInstanceTests {
		instances := convertInstances(tt.externalSvc)
		sortServiceInstances(instances)
		sortServiceInstances(tt.out)
		if err := compare(t, instances, tt.out); err != nil {
			t.Error(err)
		}
	}
}

func compare(t *testing.T, actual, expected interface{}) error {
	return util.Compare(jsonBytes(t, actual), jsonBytes(t, expected))
}

func jsonBytes(t *testing.T, v interface{}) []byte {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(t)
	}
	return data
}
