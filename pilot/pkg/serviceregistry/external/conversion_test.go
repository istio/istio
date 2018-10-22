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
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
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

func convertPortNameToProtocol(name string) model.Protocol {
	prefix := name
	i := strings.Index(name, "-")
	if i >= 0 {
		prefix = name[:i]
	}
	return model.ParseProtocol(prefix)
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

func makeInstance(serviceEntry *networking.ServiceEntry, address string, port int,
	svcPort *networking.Port, labels map[string]string, creationTime time.Time) *model.ServiceInstance {
	family := model.AddressFamilyTCP
	if port == 0 {
		family = model.AddressFamilyUnix
	}

	services := convertServices(serviceEntry, creationTime)
	svc := services[0] // default
	for _, s := range services {
		if s.Hostname.String() == address {
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

func TestConvertService(t *testing.T) {
	tnow := time.Now()
	serviceTests := []struct {
		externalSvc *networking.ServiceEntry
		services    []*model.Service
	}{
		{
			// service entry http
			externalSvc: httpNone,
			services: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-number": 80, "http2-number": 8080}, true, model.Passthrough, tnow),
			},
		},
		{
			// service entry tcp
			externalSvc: tcpNone,
			services: []*model.Service{makeService("tcpnone.com", "172.217.0.0/16",
				map[string]int{"tcp-444": 444}, true, model.Passthrough, tnow),
			},
		},
		{
			// service entry http  static
			externalSvc: httpStatic,
			services: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.ClientSideLB, tnow),
			},
		},
		{
			// service entry DNS with no endpoints
			externalSvc: httpDNSnoEndpoints,
			services: []*model.Service{
				makeService("google.com", model.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
				makeService("www.wikipedia.org", model.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
			},
		},
		{
			// service entry dns
			externalSvc: httpDNS,
			services: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, tnow),
			},
		},
		{
			// service entry tcp DNS
			externalSvc: tcpDNS,
			services: []*model.Service{makeService("tcpdns.com", model.UnspecifiedIP,
				map[string]int{"tcp-444": 444}, true, model.DNSLB, tnow),
			},
		},
		{
			// service entry tcp static
			externalSvc: tcpStatic,
			services: []*model.Service{makeService("tcpstatic.com", "172.217.0.1",
				map[string]int{"tcp-444": 444}, true, model.ClientSideLB, tnow),
			},
		},
		{
			// service entry http internal
			externalSvc: httpNoneInternal,
			services: []*model.Service{makeService("*.google.com", model.UnspecifiedIP,
				map[string]int{"http-number": 80, "http2-number": 8080}, false, model.Passthrough, tnow),
			},
		},
		{
			// service entry tcp internal
			externalSvc: tcpNoneInternal,
			services: []*model.Service{makeService("tcpinternal.com", "172.217.0.0/16",
				map[string]int{"tcp-444": 444}, false, model.Passthrough, tnow),
			},
		},
		{
			// service entry multiAddrInternal
			externalSvc: multiAddrInternal,
			services: []*model.Service{
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

	for _, tt := range serviceTests {
		services := convertServices(tt.externalSvc, tnow)
		if err := compare(t, services, tt.services); err != nil {
			t.Error(err)
		}
	}
}

func TestConvertInstances(t *testing.T) {
	tnow := time.Now()
	serviceInstanceTests := []struct {
		externalSvc *networking.ServiceEntry
		out         []*model.ServiceInstance
	}{
		{
			// single instance with multiple ports
			externalSvc: httpNone,
			// DNS type none means service should not have a registered instance
			out: []*model.ServiceInstance{},
		},
		{
			// service entry tcp
			externalSvc: tcpNone,
			// DNS type none means service should not have a registered instance
			out: []*model.ServiceInstance{},
		},
		{
			// service entry static
			externalSvc: httpStatic,
			out: []*model.ServiceInstance{
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
			externalSvc: httpDNSnoEndpoints,
			out: []*model.ServiceInstance{
				makeInstance(httpDNSnoEndpoints, "google.com", 80, httpDNSnoEndpoints.Ports[0], nil, tnow),
				makeInstance(httpDNSnoEndpoints, "google.com", 8080, httpDNSnoEndpoints.Ports[1], nil, tnow),
				makeInstance(httpDNSnoEndpoints, "www.wikipedia.org", 80, httpDNSnoEndpoints.Ports[0], nil, tnow),
				makeInstance(httpDNSnoEndpoints, "www.wikipedia.org", 8080, httpDNSnoEndpoints.Ports[1], nil, tnow),
			},
		},
		{
			// service entry dns
			externalSvc: httpDNS,
			out: []*model.ServiceInstance{
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
			externalSvc: tcpDNS,
			out: []*model.ServiceInstance{
				makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Ports[0], nil, tnow),
				makeInstance(tcpDNS, "in.google.com", 444, tcpDNS.Ports[0], nil, tnow),
			},
		},
		{
			// service entry tcp static
			externalSvc: tcpStatic,
			out: []*model.ServiceInstance{
				makeInstance(tcpStatic, "1.1.1.1", 444, tcpStatic.Ports[0], nil, tnow),
				makeInstance(tcpStatic, "2.2.2.2", 444, tcpStatic.Ports[0], nil, tnow),
			},
		},
		{
			// service entry unix domain socket static
			externalSvc: udsLocal,
			out: []*model.ServiceInstance{
				makeInstance(udsLocal, "/test/sock", 0, udsLocal.Ports[0], nil, tnow),
			},
		},
	}

	for _, tt := range serviceInstanceTests {
		t.Run(strings.Join(tt.externalSvc.Hosts, "_"), func(t *testing.T) {
			instances := convertInstances(tt.externalSvc, tnow)
			sortServiceInstances(instances)
			sortServiceInstances(tt.out)
			if err := compare(t, instances, tt.out); err != nil {
				t.Fatal(err)
			}
		})
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
