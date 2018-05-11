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
	"sync"
	"time"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

// ServiceEntryStore communicates with ServiceEntry CRDs and monitors for changes
type ServiceEntryStore struct {
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
	store model.IstioConfigStore

	// storeCache has callbacks. Some tests use mock store.
	// Pilot 0.8 implementation only invalidates the v1 cache.
	// Post 0.8 we want to remove the v1 cache and directly interface with ads, to
	// simplify and optimize the code, this abstraction is not helping.
	callbacks model.ConfigStoreCache

	storeMutex sync.RWMutex

	ip2instance map[string][]*model.ServiceInstance
	// Endpoints table. Key is the fqdn of the service, ':', port
	instances                     map[string][]*model.ServiceInstance

	lastChange time.Time
	updateNeeded bool
}

// NewServiceDiscovery creates a new ServiceEntry discovery service
func NewServiceDiscovery(callbacks model.ConfigStoreCache, store model.IstioConfigStore) *ServiceEntryStore {
	c := &ServiceEntryStore{
		serviceHandlers:  make([]serviceHandler, 0),
		instanceHandlers: make([]instanceHandler, 0),
		store:            store,
		callbacks:        callbacks,
		ip2instance:      map[string][]*model.ServiceInstance{},
		updateNeeded: true,
	}
	if callbacks != nil {
		callbacks.RegisterEventHandler(model.ServiceEntry.Type, func(config model.Config, event model.Event) {
			serviceEntry := config.Spec.(*networking.ServiceEntry)

			// Recomputing the index here is too expensive.
			c.storeMutex.Lock()
			c.lastChange = time.Now()
			c.updateNeeded = true
			c.storeMutex.Unlock()

			services := convertServices(serviceEntry)
			for _, handler := range c.serviceHandlers {
				for _, service := range services {
					go handler(service, event)
				}
			}

			instances := convertInstances(serviceEntry)
			for _, handler := range c.instanceHandlers {
				for _, instance := range instances {
					go handler(instance, event)
				}
			}
		})
	}

	return c
}


// Deprecated: post 0.8 we're planning to use direct interface
func (c *ServiceEntryStore) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

func (c *ServiceEntryStore) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

func (c *ServiceEntryStore) Run(stop <-chan struct{}) {}

// Services list declarations of all services in the system
func (d *ServiceEntryStore) Services() ([]*model.Service, error) {
	services := make([]*model.Service, 0)
	for _, config := range d.store.ServiceEntries() {
		serviceEntry := config.Spec.(*networking.ServiceEntry)
		services = append(services, convertServices(serviceEntry)...)
	}

	return services, nil
}

// GetService retrieves a service by host name if it exists
func (d *ServiceEntryStore) GetService(hostname model.Hostname) (*model.Service, error) {
	for _, service := range d.getServices() {
		if service.Hostname == hostname {
			return service, nil
		}
	}

	return nil, nil
}

func (d *ServiceEntryStore) getServices() []*model.Service {
	services := make([]*model.Service, 0)
	for _, config := range d.store.ServiceEntries() {
		serviceEntry := config.Spec.(*networking.ServiceEntry)
		services = append(services, convertServices(serviceEntry)...)
	}
	return services
}

// ManagementPorts retries set of health check ports by instance IP.
// This does not apply to Service Entry registry, as Service entries do not
// manage the service instances.
func (d *ServiceEntryStore) ManagementPorts(addr string) model.PortList {
	return nil
}

// Instances retrieves instances for a service and its ports that match
// any of the supplied labels. All instances match an empty tag list.
func (d *ServiceEntryStore) Instances(hostname model.Hostname, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	portMap := make(map[string]bool)
	for _, port := range ports {
		portMap[port] = true
	}

	out := []*model.ServiceInstance{}
	for _, config := range d.store.ServiceEntries() {
		serviceEntry := config.Spec.(*networking.ServiceEntry)
		for _, instance := range convertInstances(serviceEntry) {
			if instance.Service.Hostname == hostname &&
				labels.HasSubsetOf(instance.Labels) &&
				portMatchEnvoyV1(instance, portMap) {
				out = append(out, instance)
			}
		}
	}

	return out, nil
}

// Instances retrieves instances for a service on the given ports with labels that
// match any of the supplied labels. All instances match an empty tag list.
func (d *externalDiscovery) InstancesByPort(hostname model.Hostname, ports []int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	portMap := make(map[int]bool)
	for _, port := range ports {
		portMap[port] = true
	}

	out := []*model.ServiceInstance{}
	for _, config := range d.store.ServiceEntries() {
		serviceEntry := config.Spec.(*networking.ServiceEntry)
		for _, instance := range convertInstances(serviceEntry) {
			if instance.Service.Hostname == hostname &&
				labels.HasSubsetOf(instance.Labels) &&
				portMatch(instance, portMap) {
				out = append(out, instance)
			}
		}
	}

	return out, nil
}

func (c *ServiceEntryStore) update() {
	c.storeMutex.RLock()
	if ! c.updateNeeded {
		return
	}
	c.storeMutex.RUnlock()

}


// returns true if an instance's port matches with any in the provided list
func portMatchEnvoyV1(instance *model.ServiceInstance, portMap map[string]bool) bool {
	return len(portMap) == 0 || portMap[instance.Endpoint.ServicePort.Name]
}

// returns true if an instance's port matches with any in the provided list
func portMatch(instance *model.ServiceInstance, portMap map[int]bool) bool {
	return len(portMap) == 0 || portMap[instance.Endpoint.ServicePort.Port]
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (d *ServiceEntryStore) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	// There is no proxy sitting next to google.com.  If supplied, istio will end up generating a full envoy
	// configuration with routes to internal services, (listeners, etc.) for the service entry
	// (which does not exist in the cluster).
	return []*model.ServiceInstance{}, nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation TODOg
func (d *ServiceEntryStore) GetIstioServiceAccounts(hostname model.Hostname, ports []string) []string {
	//for service entries, there is no istio auth, no service accounts, etc. It is just a
	// service, with service instances, and dns.
	return []string{}
}
