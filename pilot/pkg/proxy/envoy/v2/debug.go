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

package v2

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

// memregistry is based on mock/discovery - it is used for testing and debugging v2.
// In future (post 1.0) it may be used for representing remote pilots.

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = NewMemServiceDiscovery(
		map[string]*model.Service{
			//			mock.HelloService.Hostname: mock.HelloService,
		}, 2)

	sctl.AddRegistry(aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("memAdapter"),
		ServiceDiscovery: s.MemRegistry,
		ServiceAccounts:  s.MemRegistry,
		Controller:       s.MemRegistry.controller,
	})

	mux.HandleFunc("/debug/edsz", EDSz)

	mux.HandleFunc("/debug/cdsz", Cdsz)

	mux.HandleFunc("/debug/ldsz", LDSz)

	mux.HandleFunc("/debug/registryz", s.registryz)
}

// NewMemServiceDiscovery builds an in-memory MemServiceDiscovery
func NewMemServiceDiscovery(services map[string]*model.Service, versions int) *MemServiceDiscovery {
	return &MemServiceDiscovery{
		services:    services,
		versions:    versions,
		controller:  &memServiceController{},
		instances:   map[string][]*model.ServiceInstance{},
		ip2instance: map[string][]*model.ServiceInstance{},
	}
}

// TODO: the mock was used for test setup, has no mutex. This will also be used for
// integration and load tests, will need to add mutex as we cleanup the code.

type memServiceController struct {
	svcHandlers  []func(*model.Service, model.Event)
	instHandlers []func(*model.ServiceInstance, model.Event)
}

func (c *memServiceController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.svcHandlers = append(c.svcHandlers, f)
	return nil
}

func (c *memServiceController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instHandlers = append(c.instHandlers, f)
	return nil
}

func (c *memServiceController) Run(<-chan struct{}) {}

// MemServiceDiscovery is a mock discovery interface
type MemServiceDiscovery struct {
	services                      map[string]*model.Service
	instances                     map[string][]*model.ServiceInstance
	ip2instance                   map[string][]*model.ServiceInstance
	versions                      int
	WantGetProxyServiceInstances  []*model.ServiceInstance
	ServicesError                 error
	GetServiceError               error
	InstancesError                error
	GetProxyServiceInstancesError error
	controller                    model.Controller
}

// ClearErrors clear errors used for mocking failures during model.MemServiceDiscovery interface methods
func (sd *MemServiceDiscovery) ClearErrors() {
	sd.ServicesError = nil
	sd.GetServiceError = nil
	sd.InstancesError = nil
	sd.GetProxyServiceInstancesError = nil
}

// AddService adds an in-memory service.
func (sd *MemServiceDiscovery) AddService(name string, svc *model.Service) {
	sd.services[name] = svc
	// TODO: notify listeners
}

// AddInstance adds an in-memory instance.
func (sd *MemServiceDiscovery) AddInstance(service string, instanceName string, instance *model.ServiceInstance) {
	// WIP: add enough code to allow tests and load tests to work
	svc := sd.services[service]
	if svc == nil {
		return
	}
	instance.Service = svc
	sd.ip2instance[instance.Endpoint.Address] = []*model.ServiceInstance{instance}

	instanceList := sd.instances[service]
	if instanceList == nil {
		instanceList = []*model.ServiceInstance{instance}
		sd.instances[service] = instanceList
		return
	}
	sd.instances[service] = append(instanceList, instance)
}

// Services implements discovery interface
func (sd *MemServiceDiscovery) Services() ([]*model.Service, error) {
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
func (sd *MemServiceDiscovery) GetService(hostname string) (*model.Service, error) {
	if sd.GetServiceError != nil {
		return nil, sd.GetServiceError
	}
	val := sd.services[hostname]
	return val, sd.GetServiceError
}

// Instances filters the service instances by labels
func (sd *MemServiceDiscovery) Instances(hostname string, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	instances, ok := sd.instances[hostname]
	if !ok {
		return nil, sd.InstancesError
	}
	return instances, sd.InstancesError
}

// GetProxyServiceInstances returns service instances associated with a node, resulting in
// 'in' services.
func (sd *MemServiceDiscovery) GetProxyServiceInstances(node model.Proxy) ([]*model.ServiceInstance, error) {
	if sd.GetProxyServiceInstancesError != nil {
		return nil, sd.GetProxyServiceInstancesError
	}
	if sd.WantGetProxyServiceInstances != nil {
		return sd.WantGetProxyServiceInstances, nil
	}
	out := make([]*model.ServiceInstance, 0)
	si, found := sd.ip2instance[node.IPAddress]
	if found {
		out = append(out, si...)
	}
	return out, sd.GetProxyServiceInstancesError
}

// ManagementPorts implements discovery interface
func (sd *MemServiceDiscovery) ManagementPorts(addr string) model.PortList {
	return model.PortList{{
		Name:     "http",
		Port:     3333,
		Protocol: model.ProtocolHTTP,
	}, {
		Name:     "custom",
		Port:     9999,
		Protocol: model.ProtocolTCP,
	}}
}

// GetIstioServiceAccounts gets the Istio service accounts for a service hostname.
func (sd *MemServiceDiscovery) GetIstioServiceAccounts(hostname string, ports []string) []string {
	if hostname == "world.default.svc.cluster.local" {
		return []string{
			"spiffe://cluster.local/ns/default/sa/serviceaccount1",
			"spiffe://cluster.local/ns/default/sa/serviceaccount2",
		}
	}
	return make([]string, 0)
}

// registryz providees debug support for registry - adding and listing model items.
// Can be combined with the push debug interface to reproduce changes.
func (s *DiscoveryServer) registryz(w http.ResponseWriter, req *http.Request) {
	req.ParseForm()
	svcName := req.Form.Get("svc")
	epName := req.Form.Get("ep")
	if svcName != "" {
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return
		}
		if epName == "" {
			svc := &model.Service{}
			err = json.Unmarshal(data, svc)
			if err != nil {
				return
			}
			s.MemRegistry.AddService(svcName, svc)
		} else {
			svc := &model.ServiceInstance{}
			err = json.Unmarshal(data, svc)
			if err != nil {
				return
			}
			s.MemRegistry.AddInstance(svcName, epName, svc)
		}
	}

	all, err := s.env.ServiceDiscovery.Services()
	if err != nil {
		return
	}
	for _, svc := range all {
		b, err := json.MarshalIndent(svc, "", "  ")
		if err != nil {
			return
		}
		_, _ = w.Write(b)
	}
}
