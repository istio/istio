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

	"golang.org/x/net/context"

	copilotapi "code.cloudfoundry.org/copilot/api"

	"istio.io/istio/pilot/model"
)

type CopilotClient interface {
	copilotapi.IstioCopilotClient
}

const AppPort = 8080

type ServiceDiscovery struct {
	Client CopilotClient
}

func (sd *ServiceDiscovery) Services() ([]*model.Service, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting services: %s", err)
	}
	services := make([]*model.Service, 0, len(resp.GetBackends()))

	for hostname, _ := range resp.Backends {
		services = append(services, newService(hostname))
	}

	return services, nil
}

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
	return nil, fmt.Errorf("No service exists with name '%s'.", hostname)
}

func newService(hostname string) *model.Service {
	return &model.Service{
		Hostname: hostname,
		Ports: []*model.Port{
			&model.Port{
				Port:     AppPort,
				Protocol: model.ProtocolTCP,
			},
		},
	}
}

func (sd *ServiceDiscovery) Instances(hostname string, ports []string,
	tagsList model.LabelsCollection) ([]*model.ServiceInstance, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting instances: %s", err)
	}
	service := newService(hostname)
	instances := make([]*model.ServiceInstance, 0, len(resp.GetBackends()))
	backendSet, ok := resp.Backends[hostname]
	if !ok {
		return nil, nil
	}
	for _, backend := range backendSet.GetBackends() {
		instances = append(instances, &model.ServiceInstance{
			Endpoint: model.NetworkEndpoint{
				Address:     backend.Address,
				Port:        int(backend.Port),
				ServicePort: service.Ports[0],
			},
			Service: service,
		})
	}

	return instances, nil
}

func (sd *ServiceDiscovery) HostInstances(addrs map[string]bool) ([]*model.ServiceInstance, error) {
	return nil, nil
}

func (sd *ServiceDiscovery) ManagementPorts(addr string) model.PortList {
	return nil
}
