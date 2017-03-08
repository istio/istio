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

package mock

import (
	"fmt"
	"net"

	"istio.io/manager/model"
)

// Mock values
var (
	HelloService                        = MakeService("hello.default.svc.cluster.local", "10.1.0.0")
	WorldService                        = MakeService("world.default.svc.cluster.local", "10.2.0.0")
	Discovery    model.ServiceDiscovery = &ServiceDiscovery{
		services: map[string]*model.Service{
			HelloService.Hostname: HelloService,
			WorldService.Hostname: WorldService,
		},
		versions: 2,
	}
	HostInstanceV0 = MakeIP(HelloService, 0)
	HostInstanceV1 = MakeIP(HelloService, 1)
)

// MakeService creates a mock service
func MakeService(hostname, address string) *model.Service {
	return &model.Service{
		Hostname: hostname,
		Address:  address,
		Ports: []*model.Port{{
			Name:     "http",
			Port:     80,
			Protocol: model.ProtocolHTTP,
		}, {
			Name:     "http-status",
			Port:     81,
			Protocol: model.ProtocolHTTP,
		}, {
			Name:     "custom",
			Port:     90,
			Protocol: model.ProtocolTCP,
		}},
	}
}

// MakeInstance creates a mock instance, version enumerates endpoints
func MakeInstance(service *model.Service, port *model.Port, version int) *model.ServiceInstance {
	// we make port 80 same as endpoint port, otherwise, it's distinct
	target := port.Port
	if target != 80 {
		target = target + 1000
	}

	return &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address:     MakeIP(service, version),
			Port:        target,
			ServicePort: port,
		},
		Service: service,
		Tags:    map[string]string{"version": fmt.Sprintf("v%d", version)},
	}
}

// MakeIP creates a fake IP address for a service and instance version
func MakeIP(service *model.Service, version int) string {
	ip := net.ParseIP(service.Address).To4()
	ip[2] = byte(1)
	ip[3] = byte(version)
	return ip.String()
}

// ServiceDiscovery is a mock discovery interface
type ServiceDiscovery struct {
	services map[string]*model.Service
	versions int
}

// Services implements discovery interface
func (sd *ServiceDiscovery) Services() []*model.Service {
	out := make([]*model.Service, 0)
	for _, service := range sd.services {
		out = append(out, service)
	}
	return out
}

// GetService implements discovery interface
func (sd *ServiceDiscovery) GetService(hostname string) (*model.Service, bool) {
	val, ok := sd.services[hostname]
	return val, ok
}

// Instances implements discovery interface
func (sd *ServiceDiscovery) Instances(hostname string, ports []string, tags model.TagsList) []*model.ServiceInstance {
	service, ok := sd.services[hostname]
	if !ok {
		return nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, name := range ports {
		if port, ok := service.Ports.Get(name); ok {
			for v := 0; v < sd.versions; v++ {
				if tags.HasSubsetOf(map[string]string{"version": fmt.Sprintf("v%d", v)}) {
					out = append(out, MakeInstance(service, port, v))
				}
			}
		}
	}
	return out
}

// HostInstances implements discovery interface
func (sd *ServiceDiscovery) HostInstances(addrs map[string]bool) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range sd.services {
		for v := 0; v < sd.versions; v++ {
			if addrs[MakeIP(service, v)] {
				for _, port := range service.Ports {
					out = append(out, MakeInstance(service, port, v))
				}
			}
		}
	}
	return out
}
