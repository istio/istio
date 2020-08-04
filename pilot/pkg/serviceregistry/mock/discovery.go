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

package mock

import (
	"fmt"
	"net"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/spiffe"
)

var (
	// PortHTTPName is the HTTP port name
	PortHTTPName = "http"

	// Locality for mock endpoints
	Locality = model.Locality{
		Label:     "region/zone",
		ClusterID: "",
	}
)

// NewDiscovery builds a memory ServiceDiscovery
func NewDiscovery(services map[host.Name]*model.Service, versions int) *ServiceDiscovery {
	return &ServiceDiscovery{
		services: services,
		versions: versions,
	}
}

// MakeService creates a memory service
func MakeService(hostname host.Name, address string) *model.Service {
	return &model.Service{
		CreationTime: time.Now(),
		Hostname:     hostname,
		Address:      address,
		Ports: []*model.Port{
			{
				Name:     PortHTTPName,
				Port:     80, // target port 80
				Protocol: protocol.HTTP,
			}, {
				Name:     "http-status",
				Port:     81, // target port 1081
				Protocol: protocol.HTTP,
			}, {
				Name:     "custom",
				Port:     90, // target port 1090
				Protocol: protocol.TCP,
			}, {
				Name:     "mongo",
				Port:     100, // target port 1100
				Protocol: protocol.Mongo,
			}, {
				Name:     "redis",
				Port:     110, // target port 1110
				Protocol: protocol.Redis,
			}, {
				Name:     "mysql",
				Port:     120, // target port 1120
				Protocol: protocol.MySQL,
			},
		},
	}
}

// MakeExternalHTTPService creates memory external service
func MakeExternalHTTPService(hostname host.Name, isMeshExternal bool, address string) *model.Service {
	return &model.Service{
		CreationTime: time.Now(),
		Hostname:     hostname,
		Address:      address,
		MeshExternal: isMeshExternal,
		Ports: []*model.Port{{
			Name:     "http",
			Port:     80,
			Protocol: protocol.HTTP,
		}},
	}
}

// MakeExternalHTTPSService creates memory external service
func MakeExternalHTTPSService(hostname host.Name, isMeshExternal bool, address string) *model.Service {
	return &model.Service{
		CreationTime: time.Now(),
		Hostname:     hostname,
		Address:      address,
		MeshExternal: isMeshExternal,
		Ports: []*model.Port{{
			Name:     "https",
			Port:     443,
			Protocol: protocol.HTTPS,
		}},
	}
}

// newServiceInstance creates a memory instance, version enumerates endpoints
func newServiceInstance(service *model.Service, port *model.Port, version int, locality model.Locality) *model.ServiceInstance {
	if service.External() {
		return nil
	}

	// we make port 80 same as endpoint port, otherwise, it's distinct
	target := port.Port
	if target != 80 {
		target += 1000
	}

	return &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         MakeIP(service, version),
			EndpointPort:    uint32(target),
			ServicePortName: port.Name,
			Labels:          map[string]string{"version": fmt.Sprintf("v%d", version)},
			Locality:        locality,
		},
		Service:     service,
		ServicePort: port,
	}
}

// MakeIP creates a fake IP address for a service and instance version
func MakeIP(service *model.Service, version int) string {
	// external services have no instances
	if service.External() {
		return ""
	}
	ip := net.ParseIP(service.Address).To4()
	ip[2] = byte(1)
	ip[3] = byte(version)
	return ip.String()
}

// ServiceDiscovery is a memory discovery interface
type ServiceDiscovery struct {
	services                      map[host.Name]*model.Service
	versions                      int
	WantGetProxyServiceInstances  []*model.ServiceInstance
	ServicesError                 error
	GetServiceError               error
	InstancesError                error
	GetProxyServiceInstancesError error
}

// Services implements discovery interface
func (sd *ServiceDiscovery) Services() ([]*model.Service, error) {
	if sd.ServicesError != nil {
		return nil, sd.ServicesError
	}
	out := make([]*model.Service, 0, len(sd.services))
	for _, service := range sd.services {
		out = append(out, service)
	}
	return out, sd.ServicesError
}

// GetService implements discovery interface
func (sd *ServiceDiscovery) GetService(hostname host.Name) (*model.Service, error) {
	if sd.GetServiceError != nil {
		return nil, sd.GetServiceError
	}
	val := sd.services[hostname]
	return val, sd.GetServiceError
}

// InstancesByPort implements discovery interface
func (sd *ServiceDiscovery) InstancesByPort(svc *model.Service, num int,
	labels labels.Collection) ([]*model.ServiceInstance, error) {
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	if _, ok := sd.services[svc.Hostname]; !ok {
		return nil, sd.InstancesError
	}
	out := make([]*model.ServiceInstance, 0)
	if svc.External() {
		return out, sd.InstancesError
	}
	if port, ok := svc.Ports.GetByPort(num); ok {
		for v := 0; v < sd.versions; v++ {
			if labels.HasSubsetOf(map[string]string{"version": fmt.Sprintf("v%d", v)}) {
				out = append(out, newServiceInstance(svc, port, v, Locality))
			}
		}
	}
	return out, sd.InstancesError
}

// GetProxyServiceInstances implements discovery interface
func (sd *ServiceDiscovery) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	if sd.GetProxyServiceInstancesError != nil {
		return nil, sd.GetProxyServiceInstancesError
	}
	if sd.WantGetProxyServiceInstances != nil {
		return sd.WantGetProxyServiceInstances, nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, service := range sd.services {
		if !service.External() {
			for v := 0; v < sd.versions; v++ {
				// Only one IP for memory discovery?
				if node.IPAddresses[0] == MakeIP(service, v) {
					for _, port := range service.Ports {
						out = append(out, newServiceInstance(service, port, v, Locality))
					}
				}
			}

		}
	}
	return out, sd.GetProxyServiceInstancesError
}

func (sd *ServiceDiscovery) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	if sd.GetProxyServiceInstancesError != nil {
		return nil, sd.GetProxyServiceInstancesError
	}
	// no useful labels from the ServiceInstances created by newServiceInstance()
	return nil, nil
}

// GetIstioServiceAccounts gets the Istio service accounts for a service hostname.
func (sd *ServiceDiscovery) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	if svc.Hostname == "world.default.svc.cluster.local" {
		return []string{
			spiffe.MustGenSpiffeURI("default", "serviceaccount1"),
			spiffe.MustGenSpiffeURI("default", "serviceaccount2"),
		}
	}
	return make([]string, 0)
}

type Controller struct{}

func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	return nil
}

func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	return nil
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) error {
	return nil
}

func (c *Controller) Run(<-chan struct{}) {}

func (c *Controller) HasSynced() bool { return true }
