//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package local

import (
	"sync"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

// serviceManager provides local implementations of model.Controller, model.ServiceDiscovery, and model.ServiceAccounts
type serviceManager struct {
	controller                    serviceController
	services                      map[string]*model.Service
	instances                     map[string][]*model.ServiceInstance
	versions                      int
	servicesError                 error
	getServiceError               error
	instancesError                error
	getProxyServiceInstancesError error
}

func (m *serviceManager) addService(name string, svc *model.Service) {
	m.services[name] = svc
	m.controller.notifyServiceEvent(serviceEvent{
		service: svc,
		event:   model.EventAdd,
	})
}

func (m *serviceManager) addInstance(name string, i *model.ServiceInstance) {
	m.instances[name] = append(m.instances[name], i)
	m.controller.notifyInstanceEvent(instanceEvent{
		instance: i,
		event:    model.EventAdd,
	})
}

// TODO(nmittler): Should we implement update/delete as well?

// toRegistry is an adapter method that converts the contents of this service manager
func (m *serviceManager) toRegistry() aggregate.Registry {
	return aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockRegistry"),
		ServiceDiscovery: m,
		ServiceAccounts:  m,
		Controller:       &m.controller,
	}
}

func (m *serviceManager) clearErrors() {
	m.servicesError = nil
	m.getServiceError = nil
	m.instancesError = nil
	m.getProxyServiceInstancesError = nil
}

// Services implements model.ServiceDiscovery interface
func (m *serviceManager) Services() ([]*model.Service, error) {
	if m.servicesError != nil {
		return nil, m.servicesError
	}

	out := make([]*model.Service, 0, len(m.services))
	for _, service := range m.services {
		out = append(out, service)
	}
	return out, nil
}

// GetService implements model.ServiceDiscovery interface
func (m *serviceManager) GetService(hostname string) (*model.Service, error) {
	if m.getServiceError != nil {
		return nil, m.getServiceError
	}
	val := m.services[hostname]
	return val, m.getServiceError
}

// Instances implements model.ServiceDiscovery interface
func (m *serviceManager) Instances(hostname string, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	if m.instancesError != nil {
		return nil, m.instancesError
	}

	instances, ok := m.instances[hostname]
	if !ok {
		return nil, nil
	}
	return append([]*model.ServiceInstance{}, instances...), nil
}

// GetProxyServiceInstances implements model.ServiceDiscovery interface
func (m *serviceManager) GetProxyServiceInstances(node model.Proxy) ([]*model.ServiceInstance, error) {
	if m.getProxyServiceInstancesError != nil {
		return nil, m.getProxyServiceInstancesError
	}
	// TODO(nmittler): What's the right thing to do here?
	return []*model.ServiceInstance{}, nil
}

// ManagementPorts implements model.ServiceDiscovery interface
func (m *serviceManager) ManagementPorts(addr string) model.PortList {
	// TODO(nmittler): Do we need this?
	return []*model.Port{}
}

// GetIstioServiceAccounts implements model.ServiceAccounts interface
func (m *serviceManager) GetIstioServiceAccounts(hostname string, ports []string) []string {
	for _, s := range m.services {
		if s.Hostname == hostname {
			return s.ServiceAccounts
		}
	}
	return make([]string, 0)
}

type serviceController struct {
	serviceEventChan  chan serviceEvent
	instanceEventChan chan instanceEvent
	m                 sync.Mutex
	serviceHandlers   []func(*model.Service, model.Event)
	instanceHandlers  []func(*model.ServiceInstance, model.Event)
}

type serviceEvent struct {
	service *model.Service
	event   model.Event
}

type instanceEvent struct {
	instance *model.ServiceInstance
	event    model.Event
}

func (c *serviceController) notifyServiceEvent(e serviceEvent) {
	c.serviceEventChan <- e
}

func (c *serviceController) notifyInstanceEvent(e instanceEvent) {
	c.instanceEventChan <- e
}

// AppendServiceHandler implements model.Controller interface
func (c *serviceController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.m.Lock()
	defer c.m.Unlock()
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

// AppendInstanceHandler implements model.Controller interface
func (c *serviceController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.m.Lock()
	defer c.m.Unlock()
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

// Run implements model.Controller interface
func (c *serviceController) Run(stop <-chan struct{}) {
	select {
	case se := <-c.serviceEventChan:
		handlers := c.copyServiceHandlers()
		for _, h := range handlers {
			h(se.service, se.event)
		}
	case ie := <-c.instanceEventChan:
		handlers := c.copyInstanceHandlers()
		for _, h := range handlers {
			h(ie.instance, ie.event)
		}
	case <-stop:
		return
	}
}

func (c *serviceController) copyServiceHandlers() []func(*model.Service, model.Event) {
	c.m.Lock()
	defer c.m.Unlock()
	return append([]func(*model.Service, model.Event){}, c.serviceHandlers...)
}

func (c *serviceController) copyInstanceHandlers() []func(*model.ServiceInstance, model.Event) {
	c.m.Lock()
	defer c.m.Unlock()
	return append([]func(*model.ServiceInstance, model.Event){}, c.instanceHandlers...)
}
