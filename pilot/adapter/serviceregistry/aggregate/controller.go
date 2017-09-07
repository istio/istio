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

package aggregate

import (
	"github.com/golang/glog"

	"istio.io/pilot/model"
	"istio.io/pilot/platform"
)

// Registry specifies the collection of service registry related interfaces
type Registry struct {
	Name platform.ServiceRegistry
	model.Controller
	model.ServiceDiscovery
	model.ServiceAccounts
}

// Controller aggregates data across different registries and monitors for changes
type Controller struct {
	registries []Registry
}

// NewController creates a new Aggregate controller
func NewController() *Controller {
	return &Controller{
		registries: make([]Registry, 0),
	}
}

// AddRegistry adds registries into the aggregated controller
func (c *Controller) AddRegistry(registry Registry) {
	c.registries = append(c.registries, registry)
}

// Services lists services from all platforms
func (c *Controller) Services() []*model.Service {
	services := make([]*model.Service, 0)
	for _, r := range c.registries {
		services = append(services, r.Services()...)
	}
	return services
}

// GetService retrieves a service by hostname if exists
func (c *Controller) GetService(hostname string) (*model.Service, bool) {
	for _, r := range c.registries {
		if service, exists := r.GetService(hostname); exists {
			return service, true
		}
	}
	return nil, false
}

// ManagementPorts retrieves set of health check ports by instance IP
// Return on the first hit.
func (c *Controller) ManagementPorts(addr string) model.PortList {
	for _, r := range c.registries {
		if portList := r.ManagementPorts(addr); portList != nil {
			return portList
		}
	}
	return nil
}

// Instances retrieves instances for a service and its ports that match
// any of the supplied labels. All instances match an empty label list.
func (c *Controller) Instances(hostname string, ports []string,
	labels model.LabelsCollection) []*model.ServiceInstance {
	var instances []*model.ServiceInstance
	for _, r := range c.registries {
		if instances = r.Instances(hostname, ports, labels); len(instances) > 0 {
			break
		}
	}
	return instances
}

// HostInstances lists service instances for a given set of IPv4 addresses.
func (c *Controller) HostInstances(addrs map[string]bool) []*model.ServiceInstance {
	instances := make([]*model.ServiceInstance, 0)
	for _, r := range c.registries {
		instances = append(instances, r.HostInstances(addrs)...)
	}
	return instances
}

// Run starts all the controllers
func (c *Controller) Run(stop <-chan struct{}) {

	for _, r := range c.registries {
		go r.Run(stop)
	}

	<-stop
	glog.V(2).Info("Registry Aggregator terminated")
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	for _, r := range c.registries {
		if err := r.AppendServiceHandler(f); err != nil {
			glog.V(2).Infof("Fail to append service handler to adapter %s", r.Name)
			return err
		}
	}
	return nil
}

// AppendInstanceHandler implements a service instance catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	for _, r := range c.registries {
		if err := r.AppendInstanceHandler(f); err != nil {
			glog.V(2).Infof("Fail to append instance handler to adapter %s", r.Name)
			return err
		}
	}
	return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation
func (c *Controller) GetIstioServiceAccounts(hostname string, ports []string) []string {
	for _, r := range c.registries {
		if svcAccounts := r.GetIstioServiceAccounts(hostname, ports); svcAccounts != nil {
			return svcAccounts
		}
	}
	return nil
}
