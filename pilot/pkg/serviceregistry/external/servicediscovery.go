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
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// Controller communicates with Consul and monitors for changes
type externalDiscovery struct {
	config model.IstioConfigStore
}

// NewController creates a new Consul controller
func NewServiceDiscovery(config model.IstioConfigStore) model.ServiceDiscovery {
	return &externalDiscovery{
		config:  config,
	}
}

// Services list declarations of all services in the system
func (c *externalDiscovery) Services() ([]*model.Service, error) {
	configs := c.config.ExternalServices()

	services := make([]*model.Service, 0)
	for _, externalServiceConfig := range configs {
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)

		services = append(services, convertService(externalService)...)
	}

	return services, nil
}

// GetService retrieves a service by host name if it exists
func (c *externalDiscovery) GetService(hostname string) (*model.Service, error) {

	// Get actual service by name
	name, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	for _, service := range c.getServices() {
		if service.Hostname == name {
			return service, nil
		}
	}


	return nil, nil
}

func (c *externalDiscovery) getServices() ([]*model.Service) {
	configs := c.config.ExternalServices()

	services := make([]*model.Service, 0)
	for _, externalServiceConfig := range configs {
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)

		services = append(services, convertService(externalService)...)
	}
	return services
}


// ManagementPorts retries set of health check ports by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *externalDiscovery) ManagementPorts(addr string) model.PortList {
	return nil
}

// Instances retrieves instances for a service and its ports that match
// any of the supplied labels. All instances match an empty tag list.
func (c *externalDiscovery) Instances(hostname string, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	// Get actual service by name
	name, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	portMap := make(map[string]bool)
	for _, port := range ports {
		portMap[port] = true
	}

	instances := []*model.ServiceInstance{}
	externalInstances := []*model.ServiceInstance{}

	configs := c.config.ExternalServices()
	for _, externalServiceConfig := range configs {
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)
		externalInstances = append(externalInstances, convertInstances(externalService)...)
	}

	for _, externalInstance := range externalInstances {
		if externalInstance.Service.Hostname == name &&
			labels.HasSubsetOf(externalInstance.Labels) &&
			portMatch(externalInstance, portMap) {

			instances = append(instances, externalInstance)
		}
	}

	return instances, nil
}

// returns true if an instance's port matches with any in the provided list
func portMatch(instance *model.ServiceInstance, portMap map[string]bool) bool {
	if len(portMap) == 0 {
		return true
	}

	if portMap[instance.Endpoint.ServicePort.Name] {
		return true
	}

	return false
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *externalDiscovery) GetProxyServiceInstances(node model.Proxy) ([]*model.ServiceInstance, error) {
	configs := c.config.ExternalServices()

	externalInstances := make([]*model.ServiceInstance, 0)
	for _, externalServiceConfig := range configs {
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)
		externalInstances = append(externalInstances, convertInstances(externalService)...)
	}

	out := make([]*model.ServiceInstance, 0)
	for _, externalInstance := range externalInstances {
		if node.IPAddress == externalInstance.Endpoint.Address {
			out = append(out, externalInstance)
		}
	}

	return out, nil
}
