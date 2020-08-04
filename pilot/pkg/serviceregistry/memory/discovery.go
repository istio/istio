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

package memory

import (
	"errors"
	"fmt"
	"sync"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/spiffe"
)

// ServiceController is a mock service controller
type ServiceController struct {
	svcHandlers  []func(*model.Service, model.Event)
	instHandlers []func(*model.ServiceInstance, model.Event)

	sync.RWMutex
}

func (c *ServiceController) AppendWorkloadHandler(f func(*model.WorkloadInstance, model.Event)) error {
	// Memory does not support workload handlers; everything is done in terms of instances
	return nil
}

var _ model.Controller = &ServiceController{}

// AppendServiceHandler appends a service handler to the controller
func (c *ServiceController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.Lock()
	c.svcHandlers = append(c.svcHandlers, f)
	c.Unlock()
	return nil
}

// AppendInstanceHandler appends a service instance handler to the controller
func (c *ServiceController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.Lock()
	c.instHandlers = append(c.instHandlers, f)
	c.Unlock()
	return nil
}

// Run will run the controller
func (c *ServiceController) Run(<-chan struct{}) {}

// HasSynced always returns true
func (c *ServiceController) HasSynced() bool { return true }

// ServiceDiscovery is a mock discovery interface
type ServiceDiscovery struct {
	services map[host.Name]*model.Service
	// EndpointShards table. Key is the fqdn of the service, ':', port
	instancesByPortNum  map[string][]*model.ServiceInstance
	instancesByPortName map[string][]*model.ServiceInstance

	// Used by GetProxyServiceInstance, used to configure inbound (list of services per IP)
	// We generally expect a single instance - conflicting services need to be reported.
	ip2instance                   map[string][]*model.ServiceInstance
	WantGetProxyServiceInstances  []*model.ServiceInstance
	ServicesError                 error
	GetServiceError               error
	InstancesError                error
	GetProxyServiceInstancesError error
	Controller                    model.Controller
	ClusterID                     string

	// Used by GetProxyWorkloadLabels
	ip2workloadLabels map[string]*labels.Instance

	// XDSUpdater will push EDS changes to the ADS model.
	EDSUpdater model.XDSUpdater

	// Single mutex for now - it's for debug only.
	mutex sync.Mutex
}

var _ model.ServiceDiscovery = &ServiceDiscovery{}

// NewServiceDiscovery builds an in-memory ServiceDiscovery
func NewServiceDiscovery(services []*model.Service) *ServiceDiscovery {
	svcs := map[host.Name]*model.Service{}
	for _, svc := range services {
		svcs[svc.Hostname] = svc
	}
	return &ServiceDiscovery{
		services:            svcs,
		Controller:          &ServiceController{},
		instancesByPortNum:  map[string][]*model.ServiceInstance{},
		instancesByPortName: map[string][]*model.ServiceInstance{},
		ip2instance:         map[string][]*model.ServiceInstance{},
		ip2workloadLabels:   map[string]*labels.Instance{},
	}
}

func (sd *ServiceDiscovery) AddWorkload(ip string, labels labels.Instance) {
	sd.ip2workloadLabels[ip] = &labels
}

// AddHTTPService is a helper to add a service of type http, named 'http-main', with the
// specified vip and port.
func (sd *ServiceDiscovery) AddHTTPService(name, vip string, port int) {
	sd.AddService(host.Name(name), &model.Service{
		Hostname: host.Name(name),
		Address:  vip,
		Ports: model.PortList{
			{
				Name:     "http-main",
				Port:     port,
				Protocol: protocol.HTTP,
			},
		},
	})
}

// AddService adds an in-memory service.
func (sd *ServiceDiscovery) AddService(name host.Name, svc *model.Service) {
	sd.mutex.Lock()
	svc.Attributes.ServiceRegistry = string(serviceregistry.Mock)
	sd.services[name] = svc
	sd.mutex.Unlock()
	// TODO: notify listeners
}

// RemoveService removes an in-memory service.
func (sd *ServiceDiscovery) RemoveService(name host.Name) {
	sd.mutex.Lock()
	delete(sd.services, name)
	sd.mutex.Unlock()
	sd.EDSUpdater.SvcUpdate(sd.ClusterID, string(name), "", model.EventDelete)
}

// AddInstance adds an in-memory instance.
func (sd *ServiceDiscovery) AddInstance(service host.Name, instance *model.ServiceInstance) {
	// WIP: add enough code to allow tests and load tests to work
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	svc := sd.services[service]
	if svc == nil {
		return
	}
	instance.Service = svc
	sd.ip2instance[instance.Endpoint.Address] = []*model.ServiceInstance{instance}

	key := fmt.Sprintf("%s:%d", service, instance.ServicePort.Port)
	instanceList := sd.instancesByPortNum[key]
	sd.instancesByPortNum[key] = append(instanceList, instance)

	key = fmt.Sprintf("%s:%s", service, instance.ServicePort.Name)
	instanceList = sd.instancesByPortName[key]
	sd.instancesByPortName[key] = append(instanceList, instance)
}

// AddEndpoint adds an endpoint to a service.
func (sd *ServiceDiscovery) AddEndpoint(service host.Name, servicePortName string, servicePort int, address string, port int) *model.ServiceInstance {
	instance := &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         address,
			ServicePortName: servicePortName,
			EndpointPort:    uint32(port),
		},
		ServicePort: &model.Port{
			Name:     servicePortName,
			Port:     servicePort,
			Protocol: protocol.HTTP,
		},
	}
	sd.AddInstance(service, instance)
	return instance
}

// SetEndpoints update the list of endpoints for a service, similar with K8S controller.
func (sd *ServiceDiscovery) SetEndpoints(service string, namespace string, endpoints []*model.IstioEndpoint) {

	sh := host.Name(service)
	sd.mutex.Lock()
	defer sd.mutex.Unlock()

	svc := sd.services[sh]
	if svc == nil {
		return
	}

	// remove old entries
	for k, v := range sd.ip2instance {
		if len(v) > 0 && v[0].Service.Hostname == sh {
			delete(sd.ip2instance, k)
		}
	}
	for k, v := range sd.instancesByPortNum {
		if len(v) > 0 && v[0].Service.Hostname == sh {
			delete(sd.instancesByPortNum, k)
		}
	}
	for k, v := range sd.instancesByPortName {
		if len(v) > 0 && v[0].Service.Hostname == sh {
			delete(sd.instancesByPortName, k)
		}
	}

	for _, e := range endpoints {
		// servicePortName string, servicePort int, address string, port int
		p, _ := svc.Ports.Get(e.ServicePortName)

		instance := &model.ServiceInstance{
			Service: svc,
			ServicePort: &model.Port{
				Name:     e.ServicePortName,
				Port:     p.Port,
				Protocol: protocol.HTTP,
			},
			Endpoint: e,
		}
		sd.ip2instance[instance.Endpoint.Address] = []*model.ServiceInstance{instance}

		key := fmt.Sprintf("%s:%d", service, instance.ServicePort.Port)

		instanceList := sd.instancesByPortNum[key]
		sd.instancesByPortNum[key] = append(instanceList, instance)

		key = fmt.Sprintf("%s:%s", service, instance.ServicePort.Name)
		instanceList = sd.instancesByPortName[key]
		sd.instancesByPortName[key] = append(instanceList, instance)

	}
	_ = sd.EDSUpdater.EDSUpdate(sd.ClusterID, service, namespace, endpoints)
}

// Services implements discovery interface
// Each call to Services() should return a list of new *model.Service
func (sd *ServiceDiscovery) Services() ([]*model.Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
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
// Each call to GetService() should return a new *model.Service
func (sd *ServiceDiscovery) GetService(hostname host.Name) (*model.Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetServiceError != nil {
		return nil, sd.GetServiceError
	}
	val := sd.services[hostname]
	if val == nil {
		return nil, errors.New("missing service")
	}
	return val, sd.GetServiceError
}

// InstancesByPort filters the service instances by labels. This assumes single port, as is
// used by EDS/ADS.
func (sd *ServiceDiscovery) InstancesByPort(svc *model.Service, port int,
	labels labels.Collection) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	key := fmt.Sprintf("%s:%d", string(svc.Hostname), port)
	instances, ok := sd.instancesByPortNum[key]
	if !ok {
		return nil, nil
	}
	return instances, nil
}

// GetProxyServiceInstances returns service instances associated with a node, resulting in
// 'in' services.
func (sd *ServiceDiscovery) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetProxyServiceInstancesError != nil {
		return nil, sd.GetProxyServiceInstancesError
	}
	if sd.WantGetProxyServiceInstances != nil {
		return sd.WantGetProxyServiceInstances, nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, ip := range node.IPAddresses {
		si, found := sd.ip2instance[ip]
		if found {
			out = append(out, si...)
		}
	}
	return out, sd.GetProxyServiceInstancesError
}

func (sd *ServiceDiscovery) GetProxyWorkloadLabels(proxy *model.Proxy) (labels.Collection, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	out := make(labels.Collection, 0)

	for _, ip := range proxy.IPAddresses {
		if l, found := sd.ip2workloadLabels[ip]; found {
			out = append(out, *l)
		}
	}
	return out, nil
}

// GetIstioServiceAccounts gets the Istio service accounts for a service hostname.
func (sd *ServiceDiscovery) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if svc.Hostname == "world.default.svc.cluster.local" {
		return []string{
			spiffe.MustGenSpiffeURI("default", "serviceaccount1"),
			spiffe.MustGenSpiffeURI("default", "serviceaccount2"),
		}
	}
	return make([]string, 0)
}
