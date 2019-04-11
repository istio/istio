// Copyright 2018 Istio Authors
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

package v2

import (
	"errors"
	"fmt"
	"sync"

	"istio.io/istio/pkg/spiffe"

	"istio.io/istio/pilot/pkg/model"
)

// memregistry is based on mock/discovery - it is used for testing and debugging v2.
// In future (post 1.0) it may be used for representing remote pilots.

// MemServiceController is a mock service controller
type MemServiceController struct {
	svcHandlers  []func(*model.Service, model.Event)
	instHandlers []func(*model.ServiceInstance, model.Event)

	sync.RWMutex
}

// AppendServiceHandler appends a service handler to the controller
func (c *MemServiceController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.Lock()
	c.svcHandlers = append(c.svcHandlers, f)
	c.Unlock()
	return nil
}

// AppendInstanceHandler appends a service instance handler to the controller
func (c *MemServiceController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.Lock()
	c.instHandlers = append(c.instHandlers, f)
	c.Unlock()
	return nil
}

// Run will run the controller
func (c *MemServiceController) Run(<-chan struct{}) {}

// MemServiceDiscovery is a mock discovery interface
type MemServiceDiscovery struct {
	services map[model.Hostname]*model.Service
	// EndpointShards table. Key is the fqdn of the service, ':', port
	instancesByPortNum  map[string][]*model.ServiceInstance
	instancesByPortName map[string][]*model.ServiceInstance

	// Used by GetProxyServiceInstance, used to configure inbound (list of services per IP)
	// We generally expect a single instance - conflicting services need to be reported.
	ip2instance                   map[string][]*model.ServiceInstance
	versions                      int
	WantGetProxyServiceInstances  []*model.ServiceInstance
	ServicesError                 error
	GetServiceError               error
	InstancesError                error
	GetProxyServiceInstancesError error
	controller                    model.Controller
	ClusterID                     string

	// Used by GetProxyWorkloadLabels
	ip2workloadLabels map[string]*model.Labels

	// XDSUpdater will push EDS changes to the ADS model.
	EDSUpdater model.XDSUpdater

	// Single mutex for now - it's for debug only.
	mutex sync.Mutex
}

// NewMemServiceDiscovery builds an in-memory MemServiceDiscovery
func NewMemServiceDiscovery(services map[model.Hostname]*model.Service, versions int) *MemServiceDiscovery {
	return &MemServiceDiscovery{
		services:            services,
		versions:            versions,
		controller:          &MemServiceController{},
		instancesByPortNum:  map[string][]*model.ServiceInstance{},
		instancesByPortName: map[string][]*model.ServiceInstance{},
		ip2instance:         map[string][]*model.ServiceInstance{},
		ip2workloadLabels:   map[string]*model.Labels{},
	}
}

// ClearErrors clear errors used for mocking failures during model.MemServiceDiscovery interface methods
func (sd *MemServiceDiscovery) ClearErrors() {
	sd.ServicesError = nil
	sd.GetServiceError = nil
	sd.InstancesError = nil
	sd.GetProxyServiceInstancesError = nil
}

func (sd *MemServiceDiscovery) AddWorkload(ip string, labels model.Labels) {
	sd.ip2workloadLabels[ip] = &labels
}

// AddHTTPService is a helper to add a service of type http, named 'http-main', with the
// specified vip and port.
func (sd *MemServiceDiscovery) AddHTTPService(name, vip string, port int) {
	sd.AddService(model.Hostname(name), &model.Service{
		Hostname: model.Hostname(name),
		Ports: model.PortList{
			{
				Name:     "http-main",
				Port:     port,
				Protocol: model.ProtocolHTTP,
			},
		},
	})
}

// AddService adds an in-memory service.
func (sd *MemServiceDiscovery) AddService(name model.Hostname, svc *model.Service) {
	sd.mutex.Lock()
	sd.services[name] = svc
	sd.mutex.Unlock()
	// TODO: notify listeners
}

// AddInstance adds an in-memory instance.
func (sd *MemServiceDiscovery) AddInstance(service model.Hostname, instance *model.ServiceInstance) {
	// WIP: add enough code to allow tests and load tests to work
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	svc := sd.services[service]
	if svc == nil {
		return
	}
	instance.Service = svc
	sd.ip2instance[instance.Endpoint.Address] = []*model.ServiceInstance{instance}

	key := fmt.Sprintf("%s:%d", service, instance.Endpoint.ServicePort.Port)
	instanceList := sd.instancesByPortNum[key]
	sd.instancesByPortNum[key] = append(instanceList, instance)

	key = fmt.Sprintf("%s:%s", service, instance.Endpoint.ServicePort.Name)
	instanceList = sd.instancesByPortName[key]
	sd.instancesByPortName[key] = append(instanceList, instance)
}

// AddEndpoint adds an endpoint to a service.
func (sd *MemServiceDiscovery) AddEndpoint(service model.Hostname, servicePortName string, servicePort int, address string, port int) *model.ServiceInstance {
	instance := &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: address,
			Port:    port,
			ServicePort: &model.Port{
				Name:     servicePortName,
				Port:     servicePort,
				Protocol: model.ProtocolHTTP,
			},
		},
	}
	sd.AddInstance(service, instance)
	return instance
}

// SetEndpoints update the list of endpoints for a service, similar with K8S controller.
func (sd *MemServiceDiscovery) SetEndpoints(service string, endpoints []*model.IstioEndpoint) {

	sh := model.Hostname(service)
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
		//servicePortName string, servicePort int, address string, port int
		p, _ := svc.Ports.Get(e.ServicePortName)

		instance := &model.ServiceInstance{
			Service: svc,
			Labels:  e.Labels,
			Endpoint: model.NetworkEndpoint{
				Address: e.Address,
				ServicePort: &model.Port{
					Name:     e.ServicePortName,
					Port:     p.Port,
					Protocol: model.ProtocolHTTP,
				},
				Locality: e.Locality,
				LbWeight: e.LbWeight,
			},
			ServiceAccount: e.ServiceAccount,
		}
		sd.ip2instance[instance.Endpoint.Address] = []*model.ServiceInstance{instance}

		key := fmt.Sprintf("%s:%d", service, instance.Endpoint.ServicePort.Port)

		instanceList := sd.instancesByPortNum[key]
		sd.instancesByPortNum[key] = append(instanceList, instance)

		key = fmt.Sprintf("%s:%s", service, instance.Endpoint.ServicePort.Name)
		instanceList = sd.instancesByPortName[key]
		sd.instancesByPortName[key] = append(instanceList, instance)

	}

	sd.EDSUpdater.EDSUpdate(sd.ClusterID, service, endpoints)
}

// Services implements discovery interface
// Each call to Services() should return a list of new *model.Service
func (sd *MemServiceDiscovery) Services() ([]*model.Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.ServicesError != nil {
		return nil, sd.ServicesError
	}
	out := make([]*model.Service, 0, len(sd.services))
	for _, service := range sd.services {
		// Make a new service out of the existing one
		// nolint: govet
		newSvc := *service
		out = append(out, &newSvc)
	}
	return out, sd.ServicesError
}

// GetService implements discovery interface
// Each call to GetService() should return a new *model.Service
func (sd *MemServiceDiscovery) GetService(hostname model.Hostname) (*model.Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetServiceError != nil {
		return nil, sd.GetServiceError
	}
	val := sd.services[hostname]
	if val == nil {
		return nil, errors.New("missing service")
	}
	// Make a new service out of the existing one
	// nolint: govet
	newSvc := *val
	return &newSvc, sd.GetServiceError
}

// Instances filters the service instances by labels. This assumes single port, as is
// used by EDS/ADS.
func (sd *MemServiceDiscovery) Instances(hostname model.Hostname, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	if len(ports) != 1 {
		adsLog.Warna("Unexpected ports ", ports)
		return nil, nil
	}
	key := string(hostname) + ":" + ports[0]
	instances, ok := sd.instancesByPortName[key]
	if !ok {
		return nil, nil
	}
	return instances, nil
}

// InstancesByPort filters the service instances by labels. This assumes single port, as is
// used by EDS/ADS.
func (sd *MemServiceDiscovery) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	key := fmt.Sprintf("%s:%d", string(hostname), port)
	instances, ok := sd.instancesByPortNum[key]
	if !ok {
		return nil, nil
	}
	return instances, nil
}

// GetProxyServiceInstances returns service instances associated with a node, resulting in
// 'in' services.
func (sd *MemServiceDiscovery) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
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

func (sd *MemServiceDiscovery) GetProxyWorkloadLabels(proxy *model.Proxy) (model.LabelsCollection, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	out := make(model.LabelsCollection, 0)

	for _, ip := range proxy.IPAddresses {
		if labels, found := sd.ip2workloadLabels[ip]; found {
			out = append(out, *labels)
		}
	}
	return out, nil
}

// ManagementPorts implements discovery interface
func (sd *MemServiceDiscovery) ManagementPorts(addr string) model.PortList {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	return nil
}

// WorkloadHealthCheckInfo implements discovery interface
func (sd *MemServiceDiscovery) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}

// GetIstioServiceAccounts gets the Istio service accounts for a service hostname.
func (sd *MemServiceDiscovery) GetIstioServiceAccounts(hostname model.Hostname, ports []int) []string {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if hostname == "world.default.svc.cluster.local" {
		return []string{
			spiffe.MustGenSpiffeURI("default", "serviceaccount1"),
			spiffe.MustGenSpiffeURI("default", "serviceaccount2"),
		}
	}
	return make([]string, 0)
}
