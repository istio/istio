// Copyright 2019 Istio Authors
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

package serviceentry

import (
	"net"
	"strconv"
	"strings"
	"sync"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/istio/pkg/config/visibility"
)

var (
	_ serviceregistry.Instance = &Discovery{}
)

// DiscoveryOptions stores the configurable attributes of a Control
type DiscoveryOptions struct {
	ClusterID    string
	DomainSuffix string
}

// Discovery provides discovery interface for SyntheticServiceEntries
type Discovery struct {
	*SyntheticServiceEntryController
	*DiscoveryOptions
	cacheMutex sync.RWMutex
	// key [endpointIP]
	cacheByEndpointIP map[string][]*model.ServiceInstance
	// key [service.Hostname]
	cacheByHostName map[host.Name][]*model.ServiceInstance
	// key [hostname]
	cacheServices map[string]*model.Service
}

// NewDiscovery provides a new instance of Discovery
func NewDiscovery(controller Controller, options *DiscoveryOptions) *Discovery {
	discovery := &Discovery{
		SyntheticServiceEntryController: controller.(*SyntheticServiceEntryController),
		DiscoveryOptions:                options,
		cacheByEndpointIP:               make(map[string][]*model.ServiceInstance),
		cacheByHostName:                 make(map[host.Name][]*model.ServiceInstance),
		cacheServices:                   make(map[string]*model.Service),
	}
	discovery.RegisterEventHandler(schemas.SyntheticServiceEntry.Type, func(_, config model.Config, event model.Event) {
		discovery.HandleCacheEvents(config, event)
	})
	return discovery

}

// HandleCacheEvents populates local cache based on events received from controller
func (d *Discovery) HandleCacheEvents(config model.Config, event model.Event) {
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()
	services := convertServices(config)
	d.mergeCachedServices(services)
	switch event {
	case model.EventAdd, model.EventUpdate:
		newSvcInstancesByIP, newSvcInstancesByHost := d.convertInstances(config, services)
		for ip, svcInstances := range newSvcInstancesByIP {
			d.cacheByEndpointIP[ip] = svcInstances
		}
		for hostname, svcInstances := range newSvcInstancesByHost {
			d.cacheByHostName[hostname] = svcInstances
		}
	case model.EventDelete:
		svcInstancesByIP, svcInstancesByHost := d.convertInstances(config, services)
		for ip := range svcInstancesByIP {
			delete(d.cacheByEndpointIP, ip)
		}
		for hostname := range svcInstancesByHost {
			delete(d.cacheByHostName, hostname)
		}
	}
}

// Run until a signal is received
// NOTE: eventually there may be a need for some sort of
// cache drain/purge mechanism that runs on a time interval
// basis to purge all caches, the purge maybe necessary since
// controller's configStore only keeps track of one sink.Change
// object at a time everytime apply is called.
func (d *Discovery) Run(<-chan struct{}) {
	if err := d.initializeCache(); err != nil {
		log.Warnf("Run: %s", err)
	}
}

func (d *Discovery) Provider() serviceregistry.ProviderID {
	return serviceregistry.MCP
}

func (d *Discovery) Cluster() string {
	return d.ClusterID
}

// Services list declarations of all SyntheticServiceEntries in the system
func (d *Discovery) Services() ([]*model.Service, error) {
	if err := d.initializeCache(); err != nil {
		return nil, err
	}
	out := make([]*model.Service, 0)
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()
	for _, s := range d.cacheServices {
		out = append(out, s)
	}
	return out, nil
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
func (d *Discovery) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)

	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]

	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()

	if svcInstances, exist := d.cacheByEndpointIP[proxyIP]; exist {
		out = append(out, svcInstances...)
	}
	return out, nil
}

// InstancesByPort implements a service catalog operation
func (d *Discovery) InstancesByPort(svc *model.Service, servicePort int, labels labels.Collection) ([]*model.ServiceInstance, error) {
	if err := d.initializeCache(); err != nil {
		return nil, err
	}
	out := make([]*model.ServiceInstance, 0)
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()

	instances, found := d.cacheByHostName[svc.Hostname]
	if found {
		for _, instance := range instances {
			if instance.Service.Hostname == svc.Hostname &&
				labels.HasSubsetOf(instance.Endpoint.Labels) &&
				portMatchSingle(instance, servicePort) {
				out = append(out, instance)
			}
		}
	}
	return out, nil
}

func (d *Discovery) uid(cfg model.Config) string {
	return "kubernetes://" + cfg.Name + "." + cfg.Namespace
}

// Considered running this in the Run func, however
// it is a little too early to populate the cache then
// since the controller does not receive any data then
func (d *Discovery) initializeCache() error {
	sseConfigs, err := d.List(schemas.SyntheticServiceEntry.Type, model.NamespaceAll)
	if err != nil {
		return err
	}
	d.cacheMutex.Lock()
	for _, conf := range sseConfigs {
		// this only happens once so no need to check if namespace exist
		services := convertServices(conf)
		byIP, byHost := d.convertInstances(conf, services)
		d.mergeCacheByEndpoint(byIP)
		d.mergeCacheByHostName(byHost)
		d.mergeCachedServices(services)
	}
	d.cacheMutex.Unlock()
	return nil
}

func (d *Discovery) mergeCachedServices(newServices map[string]*model.Service) {
	for hostname, newSvc := range newServices {
		d.cacheServices[hostname] = newSvc
	}
}

func (d *Discovery) mergeCacheByEndpoint(newServicesInstances map[string][]*model.ServiceInstance) {
	for ip, svcInst := range newServicesInstances {
		d.cacheByEndpointIP[ip] = svcInst
	}
}

func (d *Discovery) mergeCacheByHostName(newServicesInstances map[host.Name][]*model.ServiceInstance) {
	for hostname, svcInst := range newServicesInstances {
		d.cacheByHostName[hostname] = svcInst
	}
}

func (d *Discovery) convertInstances(
	cfg model.Config,
	services map[string]*model.Service,
) (map[string][]*model.ServiceInstance, map[host.Name][]*model.ServiceInstance) {
	byIP := make(map[string][]*model.ServiceInstance)
	byHost := make(map[host.Name][]*model.ServiceInstance)
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	for _, service := range services {
		for _, serviceEntryPort := range serviceEntry.Ports {
			if len(serviceEntry.Endpoints) == 0 &&
				serviceEntry.Resolution == networking.ServiceEntry_DNS {
				// when service entry has discovery type DNS and no endpoints
				// we create endpoints from service's host
				// Do not use serviceentry.hosts as a service entry is converted into
				// multiple services (one for each host)
				// NOTE: these are excluded from byIP since GetProxyServiceInstances
				// can not work on hostnames
				svcInstance := &model.ServiceInstance{
					Endpoint: &model.IstioEndpoint{
						UID:             d.uid(cfg),
						Address:         string(service.Hostname),
						EndpointPort:    serviceEntryPort.Number,
						ServicePortName: serviceEntryPort.Name,
						Labels:          nil,
						TLSMode:         model.DisabledTLSModeLabel,
						Attributes: model.ServiceAttributes{
							Name:      service.Attributes.Name,
							Namespace: service.Attributes.Namespace,
						},
					},
					Service:     service,
					ServicePort: convertPort(serviceEntryPort),
				}
				if svcInstances, exist := byHost[service.Hostname]; exist {
					svcInstances = append(svcInstances, svcInstance)
					byHost[service.Hostname] = svcInstances
				} else {
					byHost[service.Hostname] = []*model.ServiceInstance{svcInstance}
				}
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					svcInstance := d.convertEndpoint(cfg, service, serviceEntryPort, endpoint)
					// populate byIP with all endpoints attached to service
					if svcInstances, exist := byIP[endpoint.Address]; exist {
						svcInstances = append(svcInstances, svcInstance)
						byIP[endpoint.Address] = svcInstances
					} else {
						byIP[endpoint.Address] = []*model.ServiceInstance{svcInstance}
					}
					// populate byHost with all service instances
					if svcInstances, exist := byHost[service.Hostname]; exist {
						svcInstances = append(svcInstances, svcInstance)
						byHost[service.Hostname] = svcInstances
					} else {
						byHost[service.Hostname] = []*model.ServiceInstance{svcInstance}
					}

				}
				notReadyEps := notReadyEndpoints(cfg)
				for ip, port := range notReadyEps {
					svcInstancesFromNotReadyEps := convertNotReadyEndpoints(service, serviceEntryPort, ip, port)
					// populate byIP with notReadyEndpoints associated to service
					if svcInstances, exist := byIP[ip]; exist {
						svcInstances = append(svcInstances, svcInstancesFromNotReadyEps...)
						byIP[ip] = svcInstances
					} else {
						byIP[ip] = svcInstancesFromNotReadyEps
					}
					// populate byHost with notReadyEndpoints associated to service
					if svcInstances, exist := byHost[service.Hostname]; exist {
						svcInstances = append(svcInstances, svcInstancesFromNotReadyEps...)
						byHost[service.Hostname] = svcInstances
					} else {
						byHost[service.Hostname] = svcInstancesFromNotReadyEps
					}
				}
			}
		}
	}
	return byIP, byHost
}

// returns true if an instance's port matches with any in the provided list
func portMatchSingle(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.ServicePort.Port
}

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: protocol.Parse(port.Protocol),
	}
}

func convertServices(cfg model.Config) map[string]*model.Service {
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	creationTime := cfg.CreationTimestamp

	out := make(map[string]*model.Service)

	var resolution model.Resolution
	switch serviceEntry.Resolution {
	case networking.ServiceEntry_NONE:
		resolution = model.Passthrough
	case networking.ServiceEntry_DNS:
		resolution = model.DNSLB
	case networking.ServiceEntry_STATIC:
		resolution = model.ClientSideLB
	}

	svcPorts := make(model.PortList, 0, len(serviceEntry.Ports))
	for _, port := range serviceEntry.Ports {
		svcPorts = append(svcPorts, convertPort(port))
	}

	var exportTo map[visibility.Instance]bool
	if len(serviceEntry.ExportTo) > 0 {
		exportTo = make(map[visibility.Instance]bool)
		for _, e := range serviceEntry.ExportTo {
			exportTo[visibility.Instance(e)] = true
		}
	}

	for _, hostname := range serviceEntry.Hosts {
		if len(serviceEntry.Addresses) > 0 {
			for _, address := range serviceEntry.Addresses {
				if ip, network, cidrErr := net.ParseCIDR(address); cidrErr == nil {
					newAddress := address
					ones, zeroes := network.Mask.Size()
					if ones == zeroes {
						// /32 mask. Remove the /32 and make it a normal IP address
						newAddress = ip.String()
					}
					out[hostname] = &model.Service{
						CreationTime: creationTime,
						MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
						Hostname:     host.Name(hostname),
						Address:      newAddress,
						Ports:        svcPorts,
						Resolution:   resolution,
						Attributes: model.ServiceAttributes{
							ServiceRegistry: string(serviceregistry.MCP),
							Name:            hostname,
							Namespace:       cfg.Namespace,
							ExportTo:        exportTo,
						},
					}
				} else if net.ParseIP(address) != nil {
					out[hostname] = &model.Service{
						CreationTime: creationTime,
						MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
						Hostname:     host.Name(hostname),
						Address:      address,
						Ports:        svcPorts,
						Resolution:   resolution,
						Attributes: model.ServiceAttributes{
							ServiceRegistry: string(serviceregistry.MCP),
							Name:            hostname,
							Namespace:       cfg.Namespace,
							ExportTo:        exportTo,
						},
					}
				}
			}
		} else {
			out[hostname] = &model.Service{
				CreationTime: creationTime,
				MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
				Hostname:     host.Name(hostname),
				Address:      constants.UnspecifiedIP,
				Ports:        svcPorts,
				Resolution:   resolution,
				Attributes: model.ServiceAttributes{
					ServiceRegistry: string(serviceregistry.MCP),
					Name:            hostname,
					Namespace:       cfg.Namespace,
					ExportTo:        exportTo,
				},
			}
		}
	}

	return out
}

func (d *Discovery) convertEndpoint(cfg model.Config, service *model.Service, servicePort *networking.Port,
	endpoint *networking.ServiceEntry_Endpoint) *model.ServiceInstance {
	var instancePort uint32
	var family model.AddressFamily
	addr := endpoint.GetAddress()
	if strings.HasPrefix(addr, model.UnixAddressPrefix) {
		instancePort = 0
		family = model.AddressFamilyUnix
		addr = strings.TrimPrefix(addr, model.UnixAddressPrefix)
	} else {
		instancePort = endpoint.Ports[servicePort.Name]
		if instancePort == 0 {
			instancePort = servicePort.Number
		}
		family = model.AddressFamilyTCP
	}

	tlsMode := model.GetTLSModeFromEndpointLabels(endpoint.Labels)

	return &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			UID:             d.uid(cfg),
			Address:         addr,
			Family:          family,
			EndpointPort:    instancePort,
			ServicePortName: servicePort.Name,
			Network:         endpoint.Network,
			Locality:        endpoint.Locality,
			LbWeight:        endpoint.Weight,
			Labels:          endpoint.Labels,
			TLSMode:         tlsMode,
			Attributes: model.ServiceAttributes{
				Name:      service.Attributes.Name,
				Namespace: service.Attributes.Namespace,
			},
		},
		Service:     service,
		ServicePort: convertPort(servicePort),
	}
}

// TODO: this serviceInstance is poorly constructed,
// figure out how to populate other critical fields
func convertNotReadyEndpoints(service *model.Service, servicePort *networking.Port, ip string, port int) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	family := model.AddressFamilyTCP
	out = append(out, &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         ip,
			Family:          family,
			EndpointPort:    uint32(port),
			ServicePortName: servicePort.Name,
			Attributes: model.ServiceAttributes{
				Name:      service.Attributes.Name,
				Namespace: service.Attributes.Namespace,
			},
		},
		Service:     service,
		ServicePort: convertPort(servicePort),
	})
	return out
}

func notReadyEndpoints(conf model.Config) map[string]int {
	notReadyEndpoints := make(map[string]int)
	if nrEps, ok := conf.Annotations[notReadyEndpointkey]; ok {
		addrs := strings.Split(nrEps, ",")
		for _, addr := range addrs {
			notReadyIP, port, err := net.SplitHostPort(addr)
			if err != nil {
				log.Errorf("notReadyEndpoints: %v", err)
			}
			notReadyPort, err := strconv.Atoi(port)
			if err != nil {
				log.Errorf("notReadyEndpoints: %v", err)
			}
			notReadyEndpoints[notReadyIP] = notReadyPort
		}
	}
	return notReadyEndpoints
}

// GetService Not Supported
func (d *Discovery) GetService(host.Name) (*model.Service, error) {
	log.Warnf("GetService %s", errUnsupported)
	return nil, nil
}

// ManagementPorts Not Supported
func (d *Discovery) ManagementPorts(string) model.PortList {
	log.Warnf("ManagementPorts %s", errUnsupported)
	return nil
}

// WorkloadHealthCheckInfo Not Supported
func (d *Discovery) WorkloadHealthCheckInfo(string) model.ProbeList {
	log.Warnf("WorkloadHealthCheckInfo %s", errUnsupported)
	return nil
}

// GetIstioServiceAccounts Not Supported
func (d *Discovery) GetIstioServiceAccounts(*model.Service, []int) []string {
	log.Warnf("GetIstioServiceAccounts %s", errUnsupported)
	return nil
}

// GetProxyWorkloadLabels Not Supported
func (d *Discovery) GetProxyWorkloadLabels(*model.Proxy) (labels.Collection, error) {
	log.Warnf("GetProxyWorkloadLabels %s", errUnsupported)
	return nil, nil
}

// model controller

// AppendServiceHandler Not Supported
func (d *Discovery) AppendServiceHandler(func(*model.Service, model.Event)) error {
	log.Warnf("AppendServiceHandler %s", errUnsupported)
	return nil
}

// AppendInstanceHandler Not Supported
func (d *Discovery) AppendInstanceHandler(func(*model.ServiceInstance, model.Event)) error {
	log.Warnf("AppendInstanceHandler %s", errUnsupported)
	return nil
}
