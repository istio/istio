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

package consul

import (
	"fmt"
	"sync"

	"github.com/hashicorp/consul/api"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/spiffe"
)

var _ serviceregistry.Instance = &Controller{}

// Controller communicates with Consul and monitors for changes
type Controller struct {
	client           *api.Client
	monitor          Monitor
	services         map[string]*model.Service //key hostname value service
	servicesList     []*model.Service
	serviceInstances map[string][]*model.ServiceInstance //key hostname value serviceInstance array
	cacheMutex       sync.Mutex
	initDone         bool
	clusterID        string
}

// NewController creates a new Consul controller
func NewController(addr string, clusterID string) (*Controller, error) {
	conf := api.DefaultConfig()
	conf.Address = addr

	client, err := api.NewClient(conf)
	monitor := NewConsulMonitor(client)
	controller := Controller{
		monitor:   monitor,
		client:    client,
		clusterID: clusterID,
	}

	//Watch the change events to refresh local caches
	monitor.AppendServiceHandler(controller.ServiceChanged)
	monitor.AppendInstanceHandler(controller.InstanceChanged)
	return &controller, err
}

func (c *Controller) Provider() serviceregistry.ProviderID {
	return serviceregistry.Consul
}

func (c *Controller) Cluster() string {
	return c.clusterID
}

// Services list declarations of all services in the system
func (c *Controller) Services() ([]*model.Service, error) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	err := c.initCache()
	if err != nil {
		return nil, err
	}

	return c.servicesList, nil
}

// GetService retrieves a service by host name if it exists
func (c *Controller) GetService(hostname host.Name) (*model.Service, error) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	err := c.initCache()
	if err != nil {
		return nil, err
	}

	// Get actual service by name
	name, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	if service, ok := c.services[name]; ok {
		return service, nil
	}
	return nil, nil
}

// InstancesByPort retrieves instances for a service that match
// any of the supplied labels. All instances match an empty tag list.
func (c *Controller) InstancesByPort(svc *model.Service, port int,
	labels labels.Collection) ([]*model.ServiceInstance, error) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	err := c.initCache()
	if err != nil {
		return nil, err
	}

	// Get actual service by name
	name, err := parseHostname(svc.Hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", svc.Hostname, err)
		return nil, err
	}

	if serviceInstances, ok := c.serviceInstances[name]; ok {
		var instances []*model.ServiceInstance
		for _, instance := range serviceInstances {
			if labels.HasSubsetOf(instance.Endpoint.Labels) && portMatch(instance, port) {
				instances = append(instances, instance)
			}
		}

		return instances, nil
	}
	return nil, fmt.Errorf("could not find instance of service: %s", name)
}

// returns true if an instance's port matches with any in the provided list
func portMatch(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.ServicePort.Port
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	err := c.initCache()
	if err != nil {
		return nil, err
	}

	out := make([]*model.ServiceInstance, 0)
	for _, instances := range c.serviceInstances {
		for _, instance := range instances {
			addr := instance.Endpoint.Address
			if len(node.IPAddresses) > 0 {
				for _, ipAddress := range node.IPAddresses {
					if ipAddress == addr {
						out = append(out, instance)
						break
					}
				}
			}
		}
	}

	return out, nil
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	err := c.initCache()
	if err != nil {
		return nil, err
	}

	out := make(labels.Collection, 0)
	for _, instances := range c.serviceInstances {
		for _, instance := range instances {
			addr := instance.Endpoint.Address
			if len(proxy.IPAddresses) > 0 {
				for _, ipAddress := range proxy.IPAddresses {
					if ipAddress == addr {
						out = append(out, instance.Endpoint.Labels)
						break
					}
				}
			}
		}
	}

	return out, nil
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	c.monitor.Start(stop)
}

// HasSynced always returns true for consul
func (c *Controller) HasSynced() bool {
	return true
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.monitor.AppendServiceHandler(func(instances []*api.CatalogService, event model.Event) error {
		f(convertService(instances), event)
		return nil
	})
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.monitor.AppendInstanceHandler(func(instance *api.CatalogService, event model.Event) error {
		f(convertInstance(instance), event)
		return nil
	})
	return nil
}

func (c *Controller) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) error {
	// Consul does not support workload handlers
	return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation TODO
func (c *Controller) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	// Need to get service account of service registered with consul
	// Currently Consul does not have service account or equivalent concept
	// As a step-1, to enabling istio security in Consul, We assume all the services run in default service account
	// This will allow all the consul services to do mTLS
	// Follow - https://goo.gl/Dt11Ct

	return []string{
		spiffe.MustGenSpiffeURI("default", "default"),
	}
}

func (c *Controller) initCache() error {
	if c.initDone {
		return nil
	}

	c.services = make(map[string]*model.Service)
	c.serviceInstances = make(map[string][]*model.ServiceInstance)

	// get all services from consul
	consulServices, err := c.getServices()
	if err != nil {
		return err
	}

	for serviceName := range consulServices {
		// get endpoints of a service from consul
		endpoints, err := c.getCatalogService(serviceName, nil)
		if err != nil {
			return err
		}
		c.services[serviceName] = convertService(endpoints)

		instances := make([]*model.ServiceInstance, len(endpoints))
		for i, endpoint := range endpoints {
			instances[i] = convertInstance(endpoint)
		}
		c.serviceInstances[serviceName] = instances
	}

	c.servicesList = make([]*model.Service, 0, len(c.services))
	for _, value := range c.services {
		c.servicesList = append(c.servicesList, value)
	}

	c.initDone = true
	return nil
}

func (c *Controller) getServices() (map[string][]string, error) {
	data, _, err := c.client.Catalog().Services(nil)
	if err != nil {
		log.Warnf("Could not retrieve services from consul: %v", err)
		return nil, err
	}

	return data, nil
}

// nolint: unparam
func (c *Controller) getCatalogService(name string, q *api.QueryOptions) ([]*api.CatalogService, error) {
	endpoints, _, err := c.client.Catalog().Service(name, "", q)
	if err != nil {
		log.Warnf("Could not retrieve service catalog from consul: %v", err)
		return nil, err
	}

	return endpoints, nil
}

func (c *Controller) refreshCache() {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()
	c.initDone = false
}

func (c *Controller) InstanceChanged(instance *api.CatalogService, event model.Event) error {
	c.refreshCache()
	return nil
}

func (c *Controller) ServiceChanged(instances []*api.CatalogService, event model.Event) error {
	c.refreshCache()
	return nil
}
