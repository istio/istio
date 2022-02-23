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
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
)

// PortHTTPName is the HTTP port name
var PortHTTPName = "http"

type ServiceArgs struct {
	Hostname        host.Name
	Address         string
	ServiceAccounts []string
	ClusterID       cluster.ID
}

func MakeServiceInstance(service *model.Service, port *model.Port, version int, locality model.Locality) *model.ServiceInstance {
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

// MakeService creates a memory service
func MakeService(args ServiceArgs) *model.Service {
	return &model.Service{
		CreationTime: time.Now(),
		Hostname:     args.Hostname,
		ClusterVIPs: model.AddressMap{
			Addresses: map[cluster.ID][]string{args.ClusterID: {args.Address}},
		},
		DefaultAddress:  args.Address,
		ServiceAccounts: args.ServiceAccounts,
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
		CreationTime:   time.Now(),
		Hostname:       hostname,
		DefaultAddress: address,
		MeshExternal:   isMeshExternal,
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
		CreationTime:   time.Now(),
		Hostname:       hostname,
		DefaultAddress: address,
		MeshExternal:   isMeshExternal,
		Ports: []*model.Port{{
			Name:     "https",
			Port:     443,
			Protocol: protocol.HTTPS,
		}},
	}
}

// MakeIP creates a fake IP address for a service and instance version
func MakeIP(service *model.Service, version int) string {
	// external services have no instances
	if service.External() {
		return ""
	}
	ip := net.ParseIP(service.DefaultAddress).To4()
	ip[2] = byte(1)
	ip[3] = byte(version)
	return ip.String()
}

type Controller struct {
	serviceHandler model.ControllerHandlers
}

func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) {
	c.serviceHandler.AppendServiceHandler(f)
}

func (c *Controller) AppendWorkloadHandler(func(*model.WorkloadInstance, model.Event)) {}

func (c *Controller) Run(<-chan struct{}) {}

func (c *Controller) HasSynced() bool { return true }

func (c *Controller) OnServiceEvent(s *model.Service, e model.Event) {
	for _, h := range c.serviceHandler.GetServiceHandlers() {
		h(s, e)
	}
}
