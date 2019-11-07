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

package coredatamodel

import (
	"net"
	"strconv"
	"strings"
	"sync"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/pkg/log"
)

var (
	_ model.Controller       = &MCPDiscovery{}
	_ model.ServiceDiscovery = &MCPDiscovery{}
)

// MCPDiscovery provides discovery interface for SyntheticServiceEntries
type MCPDiscovery struct {
	*SyntheticServiceEntryController
	cacheMutex sync.RWMutex
	// keys [namespace][endpointIP]
	cacheByEndpointIP map[string]map[string][]*model.ServiceInstance
	// keys [namespace][service.Hostname]
	cacheByHostName map[string]map[host.Name][]*model.ServiceInstance
	cacheInit       bool
}

// NewMCPDiscovery provides a new instance of Discovery
func NewMCPDiscovery(controller CoreDataModel) *MCPDiscovery {
	discovery := &MCPDiscovery{
		SyntheticServiceEntryController: controller.(*SyntheticServiceEntryController),
		cacheByEndpointIP:               make(map[string]map[string][]*model.ServiceInstance),
		cacheByHostName:                 make(map[string]map[host.Name][]*model.ServiceInstance),
	}
	discovery.RegisterEventHandler(schemas.SyntheticServiceEntry.Type, func(config model.Config, event model.Event) {
		discovery.HandleCacheEvents(config, event)
	})
	return discovery

}

func (d *MCPDiscovery) initializeCache() error {
	if !d.cacheInit {
		sseConfigs, err := d.List(schemas.SyntheticServiceEntry.Type, model.NamespaceAll)
		if err != nil {
			return err
		}
		d.cacheMutex.Lock()
		for _, conf := range sseConfigs {
			// this only happens once so no need to check if namespace exist
			byIP, byHost := convertInstances(conf)
			d.cacheByEndpointIP[conf.Namespace] = byIP
			d.cacheByHostName[conf.Namespace] = byHost
		}
		d.cacheMutex.Unlock()
		d.cacheInit = true
	}
	return nil
}

// HandleCacheEvents populates local cache based on events received from controller
func (d *MCPDiscovery) HandleCacheEvents(config model.Config, event model.Event) {
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()
	switch event {
	//TODO: break these two events apart
	case model.EventAdd, model.EventUpdate:
		newSvcInstancesByIP, newSvcInstancesByHost := convertInstances(config)
		// It is safe to NOT check if IP exists
		// this is becasue we always receive ServiceEntry's
		// endpoints in full state over MCP so we can just
		// union two maps without checking
		if cacheByIP, exist := d.cacheByEndpointIP[config.Namespace]; exist {
			for ip, svcInstances := range newSvcInstancesByIP {
				cacheByIP[ip] = svcInstances
			}
		} else {
			d.cacheByEndpointIP[config.Namespace] = newSvcInstancesByIP
		}
		if cacheByHost, exist := d.cacheByHostName[config.Namespace]; exist {
			for hostname, svcInstances := range newSvcInstancesByHost {
				cacheByHost[hostname] = svcInstances
			}
		} else {
			d.cacheByHostName[config.Namespace] = newSvcInstancesByHost
		}
	case model.EventDelete:
		svcInstancesByIP, svcInstancesByHost := convertInstances(config)
		cacheByIP, ok := d.cacheByEndpointIP[config.Namespace]
		if ok {
			for ip := range svcInstancesByIP {
				delete(cacheByIP, ip)
			}
		}
		// delete the parent map if no other config exist
		if len(d.cacheByEndpointIP[config.Namespace]) == 0 {
			delete(d.cacheByEndpointIP, config.Namespace)
		}
		cacheByHost, ok := d.cacheByHostName[config.Namespace]
		if ok {
			for hostname := range svcInstancesByHost {
				delete(cacheByHost, hostname)
			}
		}
		// delete the parent map if no other config exist
		if len(d.cacheByHostName[config.Namespace]) == 0 {
			delete(d.cacheByHostName, config.Namespace)
		}
	}
}

// Services list declarations of all SyntheticServiceEntries in the system
func (d *MCPDiscovery) Services() ([]*model.Service, error) {
	//TODO: convert to read from cache
	services := make([]*model.Service, 0)

	syntheticServiceEntries, err := d.List(schemas.SyntheticServiceEntry.Type, model.NamespaceAll)
	if err != nil {
		return nil, err
	}
	for _, cfg := range syntheticServiceEntries {
		services = append(services, convertServices(cfg)...)
	}
	return services, nil
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
func (d *MCPDiscovery) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	if err := d.initializeCache(); err != nil {
		return nil, err
	}
	out := make([]*model.ServiceInstance, 0)

	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]

	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()

	// svcInstances by a given namespace
	if proxy.ConfigNamespace != model.NamespaceAll {
		instancesByIP, ok := d.cacheByEndpointIP[proxy.ConfigNamespace]
		if ok {
			if svcInstances, exist := instancesByIP[proxyIP]; exist {
				out = append(out, svcInstances...)
			}
		}
		return out, nil
	}

	// svcInstances from all namespaces
	for _, instancesByIP := range d.cacheByEndpointIP {
		if svcInstances, exist := instancesByIP[proxyIP]; exist {
			out = append(out, svcInstances...)
		}
	}
	return out, nil
}

// InstancesByPort implements a service catalog operation
func (d *MCPDiscovery) InstancesByPort(svc *model.Service, servicePort int, labels labels.Collection) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)
	d.cacheMutex.Lock()
	defer d.cacheMutex.Unlock()

	instances, found := d.cacheByHostName[svc.Attributes.Namespace][svc.Hostname]
	if found {
		for _, instance := range instances {
			if instance.Service.Hostname == svc.Hostname &&
				labels.HasSubsetOf(instance.Labels) &&
				portMatchSingle(instance, servicePort) {
				out = append(out, instance)
			}
		}
	}
	return out, nil
}

func convertInstances(cfg model.Config) (map[string][]*model.ServiceInstance, map[host.Name][]*model.ServiceInstance) {
	byIP := make(map[string][]*model.ServiceInstance)
	byHost := make(map[host.Name][]*model.ServiceInstance)
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	services := convertServices(cfg)
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
					Endpoint: model.NetworkEndpoint{
						UID:         parseUID(cfg),
						Address:     string(service.Hostname),
						Port:        int(serviceEntryPort.Number),
						ServicePort: convertPort(serviceEntryPort),
					},
					Service: service,
					Labels:  nil,
					TLSMode: model.DisabledTLSModeLabel,
				}
				if svcInstances, exist := byHost[service.Hostname]; exist {
					svcInstances = append(svcInstances, svcInstance)
					byHost[service.Hostname] = svcInstances
				} else {
					byHost[service.Hostname] = []*model.ServiceInstance{svcInstance}
				}
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					svcInstance := convertEndpoint(cfg, service, serviceEntryPort, endpoint)
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

func parseUID(cfg model.Config) string {
	return "kubernetes://" + cfg.Name + "." + cfg.Namespace
}

// returns true if an instance's port matches with any in the provided list
func portMatchSingle(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.Endpoint.ServicePort.Port
}

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: protocol.Parse(port.Protocol),
	}
}

func convertServices(cfg model.Config) []*model.Service {
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	creationTime := cfg.CreationTimestamp

	out := make([]*model.Service, 0)

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
					out = append(out, &model.Service{
						CreationTime: creationTime,
						MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
						Hostname:     host.Name(hostname),
						Address:      newAddress,
						Ports:        svcPorts,
						Resolution:   resolution,
						Attributes: model.ServiceAttributes{
							ServiceRegistry: string(serviceregistry.MCPRegistry),
							Name:            hostname,
							Namespace:       cfg.Namespace,
							ExportTo:        exportTo,
						},
					})
				} else if net.ParseIP(address) != nil {
					out = append(out, &model.Service{
						CreationTime: creationTime,
						MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
						Hostname:     host.Name(hostname),
						Address:      address,
						Ports:        svcPorts,
						Resolution:   resolution,
						Attributes: model.ServiceAttributes{
							ServiceRegistry: string(serviceregistry.MCPRegistry),
							Name:            hostname,
							Namespace:       cfg.Namespace,
							ExportTo:        exportTo,
						},
					})
				}
			}
		} else {
			out = append(out, &model.Service{
				CreationTime: creationTime,
				MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
				Hostname:     host.Name(hostname),
				Address:      constants.UnspecifiedIP,
				Ports:        svcPorts,
				Resolution:   resolution,
				Attributes: model.ServiceAttributes{
					ServiceRegistry: string(serviceregistry.MCPRegistry),
					Name:            hostname,
					Namespace:       cfg.Namespace,
					ExportTo:        exportTo,
				},
			})
		}
	}

	return out
}

func convertEndpoint(cfg model.Config, service *model.Service, servicePort *networking.Port,
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
		Endpoint: model.NetworkEndpoint{
			UID:         parseUID(cfg),
			Address:     addr,
			Family:      family,
			Port:        int(instancePort),
			ServicePort: convertPort(servicePort),
			Network:     endpoint.Network,
			Locality:    endpoint.Locality,
			LbWeight:    endpoint.Weight,
		},
		Service: service,
		Labels:  endpoint.Labels,
		TLSMode: tlsMode,
	}
}

// TODO: this serviceInstance is poorly constructed,
// figure out how to populate other critical fields
func convertNotReadyEndpoints(service *model.Service, servicePort *networking.Port, ip string, port int) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	family := model.AddressFamilyTCP
	out = append(out, &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address:     ip,
			Family:      family,
			Port:        port,
			ServicePort: convertPort(servicePort),
		},
		Service: service,
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
func (d *MCPDiscovery) GetService(hostname host.Name) (*model.Service, error) {
	log.Warnf("GetService %s", errUnsupported)
	return nil, nil
}

// ManagementPorts Not Supported
func (d *MCPDiscovery) ManagementPorts(addr string) model.PortList {
	log.Warnf("ManagementPorts %s", errUnsupported)
	return nil
}

// WorkloadHealthCheckInfo Not Supported
func (d *MCPDiscovery) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	log.Warnf("WorkloadHealthCheckInfo %s", errUnsupported)
	return nil
}

// GetIstioServiceAccounts Not Supported
func (d *MCPDiscovery) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	log.Warnf("GetIstioServiceAccounts %s", errUnsupported)
	return nil
}

// GetProxyWorkloadLabels Not Supported
func (d *MCPDiscovery) GetProxyWorkloadLabels(*model.Proxy) (labels.Collection, error) {
	log.Warnf("GetProxyWorkloadLabels %s", errUnsupported)
	return nil, nil
}

// model Controller

// AppendServiceHandler Not Supported
func (d *MCPDiscovery) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	log.Warnf("AppendServiceHandler %s", errUnsupported)
	return nil
}

// AppendInstanceHandler Not Supported
func (d *MCPDiscovery) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	log.Warnf("AppendInstanceHandler %s", errUnsupported)
	return nil
}

// Run until a signal is received
func (d *MCPDiscovery) Run(stop <-chan struct{}) {
	log.Warnf("Run %s", errUnsupported)
}
