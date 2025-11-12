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
	"fmt"
	"net/netip"
	"sync"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

// ServiceDiscovery is a mock discovery interface
type ServiceDiscovery struct {
	services map[host.Name]*model.Service

	handlers model.ControllerHandlers

	networkGateways []model.NetworkGateway
	model.NetworkGatewaysHandler

	// EndpointShards table. Key is the fqdn of the service, ':', port
	instancesByPortNum  map[string][]*model.ServiceInstance
	instancesByPortName map[string][]*model.ServiceInstance

	// Used by GetProxyServiceInstance, used to configure inbound (list of services per IP)
	// We generally expect a single instance - conflicting services need to be reported.
	ip2instance                map[string][]*model.ServiceInstance
	WantGetProxyServiceTargets []model.ServiceTarget
	InstancesError             error
	Controller                 model.Controller
	ClusterID                  cluster.ID

	// Used by GetProxyWorkloadLabels
	ip2workloadLabels map[string]labels.Instance

	addresses map[string]model.AddressInfo

	// XDSUpdater will push EDS changes to the ADS model.
	XdsUpdater model.XDSUpdater

	// Single mutex for now - it's for debug only.
	mutex sync.Mutex
}

var (
	_ model.Controller       = &ServiceDiscovery{}
	_ model.ServiceDiscovery = &ServiceDiscovery{}
)

// NewServiceDiscovery builds an in-memory ServiceDiscovery
func NewServiceDiscovery(services ...*model.Service) *ServiceDiscovery {
	svcs := map[host.Name]*model.Service{}
	for _, svc := range services {
		svcs[svc.Hostname] = svc
	}
	return &ServiceDiscovery{
		services:            svcs,
		instancesByPortNum:  map[string][]*model.ServiceInstance{},
		instancesByPortName: map[string][]*model.ServiceInstance{},
		ip2instance:         map[string][]*model.ServiceInstance{},
		ip2workloadLabels:   map[string]labels.Instance{},
		addresses:           map[string]model.AddressInfo{},
	}
}

func (sd *ServiceDiscovery) shardKey() model.ShardKey {
	return model.ShardKey{Cluster: sd.ClusterID, Provider: provider.Mock}
}

func (sd *ServiceDiscovery) AddWorkload(ip string, labels labels.Instance) {
	sd.ip2workloadLabels[ip] = labels
}

// AddHTTPService is a helper to add a service of type http, named 'http-main', with the
// specified vip and port.
func (sd *ServiceDiscovery) AddHTTPService(name, vip string, port int) {
	sd.AddService(&model.Service{
		Hostname:       host.Name(name),
		DefaultAddress: vip,
		Ports: model.PortList{
			{
				Name:     "http-main",
				Port:     port,
				Protocol: protocol.HTTP,
			},
		},
	})
}

// AddService adds an in-memory service and notifies
func (sd *ServiceDiscovery) AddService(svc *model.Service) {
	sd.mutex.Lock()
	svc.Attributes.ServiceRegistry = provider.Mock
	var old *model.Service
	event := model.EventAdd
	if o, f := sd.services[svc.Hostname]; f {
		old = o
		event = model.EventUpdate
	}
	sd.services[svc.Hostname] = svc

	if sd.XdsUpdater != nil {
		sd.XdsUpdater.SvcUpdate(sd.shardKey(), string(svc.Hostname), svc.Attributes.Namespace, model.EventAdd)
	}
	sd.handlers.NotifyServiceHandlers(old, svc, event)
	sd.mutex.Unlock()
}

// RemoveService removes an in-memory service.
func (sd *ServiceDiscovery) RemoveService(name host.Name) {
	sd.mutex.Lock()
	svc := sd.services[name]
	delete(sd.services, name)

	// remove old entries
	for k, v := range sd.ip2instance {
		sd.ip2instance[k] = slices.FilterInPlace(v, func(instance *model.ServiceInstance) bool {
			return instance.Service == nil || instance.Service.Hostname != name
		})
	}

	if sd.XdsUpdater != nil {
		sd.XdsUpdater.SvcUpdate(sd.shardKey(), string(svc.Hostname), svc.Attributes.Namespace, model.EventDelete)
	}
	sd.handlers.NotifyServiceHandlers(nil, svc, model.EventDelete)
	sd.mutex.Unlock()
}

// AddInstance adds an in-memory instance and notifies the XDS updater
func (sd *ServiceDiscovery) AddInstance(instance *model.ServiceInstance) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	service := instance.Service.Hostname
	svc := sd.services[service]
	if svc == nil {
		return
	}
	if instance.Endpoint.ServicePortName == "" {
		instance.Endpoint.ServicePortName = instance.ServicePort.Name
	}
	instance.Service = svc
	sd.ip2instance[instance.Endpoint.FirstAddressOrNil()] = append(sd.ip2instance[instance.Endpoint.FirstAddressOrNil()], instance)

	key := fmt.Sprintf("%s:%d", service, instance.ServicePort.Port)
	instanceList := sd.instancesByPortNum[key]
	sd.instancesByPortNum[key] = append(instanceList, instance)

	key = fmt.Sprintf("%s:%s", service, instance.ServicePort.Name)
	instanceList = sd.instancesByPortName[key]
	sd.instancesByPortName[key] = append(instanceList, instance)
	eps := make([]*model.IstioEndpoint, 0, len(sd.instancesByPortName[key]))
	for _, port := range svc.Ports {
		key := fmt.Sprintf("%s:%s", service, port.Name)
		for _, i := range sd.instancesByPortName[key] {
			eps = append(eps, i.Endpoint)
		}
	}
	if sd.XdsUpdater != nil {
		sd.XdsUpdater.EDSUpdate(sd.shardKey(), string(service), svc.Attributes.Namespace, eps)
	}
}

// AddEndpoint adds an endpoint to a service.
func (sd *ServiceDiscovery) AddEndpoint(service host.Name, servicePortName string, servicePort int, address string, port int) *model.ServiceInstance {
	instance := &model.ServiceInstance{
		Service: &model.Service{Hostname: service},
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{address},
			ServicePortName: servicePortName,
			EndpointPort:    uint32(port),
		},
		ServicePort: &model.Port{
			Name:     servicePortName,
			Port:     servicePort,
			Protocol: protocol.HTTP,
		},
	}
	sd.AddInstance(instance)
	return instance
}

// SetEndpoints update the list of endpoints for a service, similar with K8S controller.
func (sd *ServiceDiscovery) SetEndpoints(service string, namespace string, endpoints []*model.IstioEndpoint) {
	sh := host.Name(service)

	sd.mutex.Lock()
	svc := sd.services[sh]
	if svc == nil {
		sd.mutex.Unlock()
		return
	}
	if svc.Attributes.Namespace != namespace {
		log.Errorf("Service namespace %q != namespace %q", svc.Attributes.Namespace, namespace)
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
				Protocol: p.Protocol,
			},
			Endpoint: e,
		}
		sd.ip2instance[instance.Endpoint.FirstAddressOrNil()] = []*model.ServiceInstance{instance}

		key := fmt.Sprintf("%s:%d", service, instance.ServicePort.Port)

		instanceList := sd.instancesByPortNum[key]
		sd.instancesByPortNum[key] = append(instanceList, instance)

		key = fmt.Sprintf("%s:%s", service, instance.ServicePort.Name)
		instanceList = sd.instancesByPortName[key]
		sd.instancesByPortName[key] = append(instanceList, instance)

	}
	if sd.XdsUpdater != nil {
		sd.XdsUpdater.EDSUpdate(sd.shardKey(), service, namespace, endpoints)
	}
	sd.mutex.Unlock()
}

// Services implements discovery interface
// Each call to Services() should return a list of new *model.Service
func (sd *ServiceDiscovery) Services() []*model.Service {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	out := make([]*model.Service, 0, len(sd.services))
	for _, service := range sd.services {
		out = append(out, service)
	}
	return out
}

// GetService implements discovery interface
// Each call to GetService() should return a new *model.Service
func (sd *ServiceDiscovery) GetService(hostname host.Name) *model.Service {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	return sd.services[hostname]
}

// GetProxyServiceTargets returns service instances associated with a node, resulting in
// 'in' services.
func (sd *ServiceDiscovery) GetProxyServiceTargets(node *model.Proxy) []model.ServiceTarget {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.WantGetProxyServiceTargets != nil {
		return sd.WantGetProxyServiceTargets
	}
	out := make([]model.ServiceTarget, 0)
	for _, ip := range node.IPAddresses {
		si, found := sd.ip2instance[ip]
		if found {
			out = append(out, slices.Map(si, model.ServiceInstanceToTarget)...)
		}
	}
	return out
}

func (sd *ServiceDiscovery) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()

	for _, ip := range proxy.IPAddresses {
		if l, found := sd.ip2workloadLabels[ip]; found {
			return l
		}
	}
	return nil
}

func (sd *ServiceDiscovery) AddGateways(gws ...model.NetworkGateway) {
	sd.networkGateways = append(sd.networkGateways, gws...)
	sd.NotifyGatewayHandlers()
}

func (sd *ServiceDiscovery) NetworkGateways() []model.NetworkGateway {
	return sd.networkGateways
}

func (sd *ServiceDiscovery) MCSServices() []model.MCSServiceInfo {
	return nil
}

// Memory does not support workload handlers; everything is done in terms of instances
func (sd *ServiceDiscovery) AppendWorkloadHandler(func(*model.WorkloadInstance, model.Event)) {}

// AppendServiceHandler appends a service handler to the controller
func (sd *ServiceDiscovery) AppendServiceHandler(f model.ServiceHandler) {
	sd.handlers.AppendServiceHandler(f)
}

// Run will run the controller
func (sd *ServiceDiscovery) Run(<-chan struct{}) {}

// HasSynced always returns true
func (sd *ServiceDiscovery) HasSynced() bool { return true }

func (sd *ServiceDiscovery) AddressInformation(requests sets.String) ([]model.AddressInfo, sets.String) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if len(requests) == 0 {
		return maps.Values(sd.addresses), nil
	}

	var infos []model.AddressInfo
	removed := sets.String{}
	for req := range requests {
		if _, found := sd.addresses[req]; !found {
			removed.Insert(req)
		} else {
			infos = append(infos, sd.addresses[req])
		}
	}
	return infos, removed
}

func (sd *ServiceDiscovery) AdditionalPodSubscriptions(
	*model.Proxy,
	sets.String,
	sets.String,
) sets.String {
	return nil
}

func (sd *ServiceDiscovery) Policies(sets.Set[model.ConfigKey]) []model.WorkloadAuthorization {
	return nil
}

func (sd *ServiceDiscovery) ServicesForWaypoint(model.WaypointKey) []model.ServiceInfo {
	return nil
}

func (sd *ServiceDiscovery) ServicesWithWaypoint(string) []model.ServiceWaypointInfo {
	return nil
}

func (sd *ServiceDiscovery) Waypoint(string, string) []netip.Addr {
	return nil
}

func (sd *ServiceDiscovery) WorkloadsForWaypoint(model.WaypointKey) []model.WorkloadInfo {
	return nil
}

func (sd *ServiceDiscovery) ServiceInfo(string) *model.ServiceInfo {
	return nil
}

func (sd *ServiceDiscovery) AddWorkloadInfo(infos ...*model.WorkloadInfo) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	for _, info := range infos {
		sd.addresses[info.ResourceName()] = workloadToAddressInfo(info.Workload)
	}
}

func (sd *ServiceDiscovery) RemoveWorkloadInfo(info *model.WorkloadInfo) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	delete(sd.addresses, info.ResourceName())
}

func (sd *ServiceDiscovery) AddServiceInfo(infos ...*model.ServiceInfo) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	for _, info := range infos {
		sd.addresses[info.ResourceName()] = serviceToAddressInfo(info.Service)
	}
}

func (sd *ServiceDiscovery) RemoveServiceInfo(info *model.ServiceInfo) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	delete(sd.addresses, info.ResourceName())
}

func workloadToAddressInfo(w *workloadapi.Workload) model.AddressInfo {
	return model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: w,
			},
		},
	}
}

func serviceToAddressInfo(s *workloadapi.Service) model.AddressInfo {
	return model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Service{
				Service: s,
			},
		},
	}
}
