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

package cloudfoundry

import (
	"fmt"

	copilotapi "code.cloudfoundry.org/copilot/api"
	"golang.org/x/net/context"

	"istio.io/istio/pilot/pkg/model"
)

// CopilotClient defines a local interface for interacting with Cloud Foundry Copilot
type CopilotClient interface {
	copilotapi.IstioCopilotClient
}

// ServiceDiscovery implements the model.ServiceDiscovery interface for Cloud Foundry
type ServiceDiscovery struct {
	Client CopilotClient

	// Cloud Foundry currently only supports applications exposing a single HTTP or TCP port
	// It is typically 8080
	ServicePort int
}

// Services implements a service catalog operation
func (sd *ServiceDiscovery) Services() ([]*model.Service, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting services: %s", err)
	}
	services := make([]*model.Service, 0, len(resp.GetBackends()))

	port := sd.servicePort()
	for hostname := range resp.Backends {
		services = append(services, &model.Service{
			Hostname: hostname,
			Ports:    []*model.Port{port},
		})
	}

	return services, nil
}

// GetService implements a service catalog operation
func (sd *ServiceDiscovery) GetService(hostname string) (*model.Service, error) {
	services, err := sd.Services()
	if err != nil {
		return nil, err
	}
	for _, svc := range services {
		if svc.Hostname == hostname {
			return svc, nil
		}
	}
	return nil, nil
}

// Instances implements a service catalog operation
func (sd *ServiceDiscovery) Instances(hostname string, ports []string, tagsList model.LabelsCollection) ([]*model.ServiceInstance, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting instances: %s", err)
	}
	instances := make([]*model.ServiceInstance, 0, len(resp.GetBackends()))
	backendSet, ok := resp.Backends[hostname]
	if !ok {
		return nil, nil
	}
	for _, backend := range backendSet.GetBackends() {
		port := sd.servicePort()

		instances = append(instances, &model.ServiceInstance{
			Endpoint: model.NetworkEndpoint{
				Address:     backend.Address,
				Port:        int(backend.Port),
				ServicePort: port,
			},
			Service: &model.Service{
				Hostname: hostname,
				Ports:    []*model.Port{port},
			},
		})
	}

	return instances, nil
}

// GetSidecarServiceInstances implements a service catalog operation
func (sd *ServiceDiscovery) GetSidecarServiceInstances(node model.Node) ([]*model.ServiceInstance, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting host instances: %s", err)
	}

	var instances []*model.ServiceInstance

	for hostname, backendSet := range resp.GetBackends() {
		for _, backend := range backendSet.GetBackends() {
			port := sd.servicePort()

			instances = append(instances, &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					Address:     backend.Address,
					Port:        int(backend.Port),
					ServicePort: port,
				},
				Service: &model.Service{
					Hostname: hostname,
					Ports:    []*model.Port{port},
				},
			})
		}
	}

	return instances, nil
}

// ManagementPorts is not currently implemented for Cloud Foundry
func (sd *ServiceDiscovery) ManagementPorts(addr string) model.PortList {
	return nil
}

// all CF apps listen on the same port (for now)
func (sd *ServiceDiscovery) servicePort() *model.Port {
	return &model.Port{Port: sd.ServicePort, Protocol: model.ProtocolHTTP}
}
