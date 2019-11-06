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
	"errors"
	"fmt"
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

// DiscoveryOptions stores the configurable attributes of a Control
type DiscoveryOptions struct {
	Env          *model.Environment
	ClusterID    string
	DomainSuffix string
}

// mixerEnabled checks to see if mixer is enabled in the environment
// so we can set the UID on eds endpoints
func (o *DiscoveryOptions) mixerEnabled() bool {
	return o.Env != nil && o.Env.Mesh != nil && (o.Env.Mesh.MixerCheckServer != "" || o.Env.Mesh.MixerReportServer != "")
}

// MCPDiscovery provides discovery interface for SyntheticServiceEntries
type MCPDiscovery struct {
	*SyntheticServiceEntryController
	*DiscoveryOptions
	cacheMutex sync.RWMutex
	// keys [namespace][endpointIP]
	cacheByEndpointIP map[string]map[string][]*model.ServiceInstance
	//cacheByHostName map[host.Name]map[string][]*model.ServiceInstance
	// TODO:^^^
	cacheInit bool
}

// NewMCPDiscovery provides a new instance of Discovery
func NewMCPDiscovery(controller CoreDataModel, options *DiscoveryOptions) *MCPDiscovery {
	discovery := &MCPDiscovery{
		SyntheticServiceEntryController: controller.(*SyntheticServiceEntryController),
		DiscoveryOptions:                options,
		cacheByEndpointIP:               make(map[string]map[string][]*model.ServiceInstance),
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
			d.cacheByEndpointIP[conf.Namespace] = convertInstances(conf)
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
		newSvcInstances := convertInstances(config)
		cacheByIP, exist := d.cacheByEndpointIP[config.Namespace]
		if exist {
			// It is safe to NOT check if IP exists
			// this is becasue we always receive ServiceEntry's
			// endpoints in full state over MCP so we can just
			// union two maps without checking
			for ip, svcInstances := range newSvcInstances {
				cacheByIP[ip] = svcInstances
			}
		} else {
			d.cacheByEndpointIP[config.Namespace] = newSvcInstances
		}
	case model.EventDelete:
		svcInstances := convertInstances(config)
		cacheByIP, ok := d.cacheByEndpointIP[config.Namespace]
		if ok {
			for ip := range svcInstances {
				delete(cacheByIP, ip)
			}
		}
		// delete the parent map if no other config exist
		if len(d.cacheByEndpointIP[config.Namespace]) == 0 {
			delete(d.cacheByEndpointIP, config.Namespace)
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
func (d *MCPDiscovery) InstancesByPort(svc *model.Service, servicePort int, lbls labels.Collection) ([]*model.ServiceInstance, error) {
	//TODO: convert to read from cache
	out := make([]*model.ServiceInstance, 0)
	if svc != nil {
		sseConf, err := d.List(schemas.SyntheticServiceEntry.Type, svc.Attributes.Namespace)
		if err != nil {
			return nil, err
		}

		for _, conf := range sseConf {
			se, ok := conf.Spec.(*networking.ServiceEntry)
			if !ok {
				return nil, errors.New("instancesByPort: wrong type")
			}
			// add both ready and not ready endpoints
			out = append(out, d.serviceInstancesBySvcPort(svc, servicePort, se, conf)...)
		}
	}
	return out, nil
}

func convertInstances(cfg model.Config) map[string][]*model.ServiceInstance {
	out := make(map[string][]*model.ServiceInstance)
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	services := convertServices(cfg)
	for _, service := range services {
		for _, serviceEntryPort := range serviceEntry.Ports {
			//TODO: put them in cacheByHostName
			if len(serviceEntry.Endpoints) == 0 &&
				serviceEntry.Resolution == networking.ServiceEntry_DNS {
				// when service entry has discovery type DNS and no endpoints
				// we create endpoints from service's host
				// Do not use serviceentry.hosts as a service entry is converted into
				// multiple services (one for each host)
				//		out = append(out, &model.ServiceInstance{
				//			Endpoint: model.NetworkEndpoint{
				//				Address:     string(service.Hostname),
				//				Port:        int(serviceEntryPort.Number),
				//				ServicePort: convertPort(serviceEntryPort),
				//			},
				//			Service: service,
				//			Labels:  nil,
				//			TLSMode: model.DisabledTLSModeLabel,
				//		})
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					svcInstance := convertEndpoint(service, serviceEntryPort, endpoint)
					if svcInstances, exist := out[endpoint.Address]; exist {
						svcInstances = append(svcInstances, svcInstance)
						out[endpoint.Address] = svcInstances
					} else {
						out[endpoint.Address] = []*model.ServiceInstance{svcInstance}
					}
				}
				// add notReadyEndpoints if there are any
				notReadyEps := notReadyEndpoints(cfg)
				for ip, port := range notReadyEps {
					svcInstancesFromNotReadyEps := convertNotReadyEndpoints(service, serviceEntryPort, ip, port)
					if svcInstances, exist := out[ip]; exist {
						svcInstances = append(svcInstances, svcInstancesFromNotReadyEps...)
						out[ip] = svcInstances
					} else {
						out[ip] = svcInstancesFromNotReadyEps
					}
				}
			}
		}
	}
	return out
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

func convertEndpoint(service *model.Service, servicePort *networking.Port,
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

func (d *MCPDiscovery) serviceInstancesFromNotReadyEps(
	proxyIP string,
	se *networking.ServiceEntry,
	svcPort *networking.Port,
	conf model.Config,
	hostname string) (out []*model.ServiceInstance) {
	var uid string
	if d.mixerEnabled() {
		uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
	}
	notReadyEps := notReadyEndpoints(conf)
	for ip, port := range notReadyEps {
		if proxyIP == "" || proxyIP == ip {
			si := &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					UID:     uid,
					Address: ip,
					Port:    port,
					ServicePort: &model.Port{
						Name:     svcPort.Name,
						Port:     int(svcPort.Number),
						Protocol: protocol.Instance(svcPort.Protocol),
					},
				},
				// ServiceAccount is retrieved in Kube/Controller
				// using IdentityPodAnnotation
				//ServiceAccount and Address: ???, //TODO: nino-k
				Service: &model.Service{
					Hostname:   host.Name(hostname),
					Ports:      convertServicePorts(se.Ports),
					Resolution: model.Resolution(int(se.Resolution)),
					Attributes: model.ServiceAttributes{
						Name:      conf.Name,
						Namespace: conf.Namespace,
						UID:       uid,
					},
				},
				Labels: labels.Instance(conf.Labels),
			}
			out = append(out, si)
		}
	}
	return
}

func (d *MCPDiscovery) serviceInstancesBySvcPort(
	svc *model.Service,
	servicePort int,
	se *networking.ServiceEntry,
	conf model.Config,
) (out []*model.ServiceInstance) {
	var uid string
	if d.mixerEnabled() {
		uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
	}
	for _, svcPort := range se.Ports {
		for _, h := range se.Hosts {
			if host.Name(h) == svc.Hostname && servicePort == int(svcPort.Number) {
				for _, ep := range se.Endpoints {
					for _, epPort := range ep.Ports {
						si := &model.ServiceInstance{
							Endpoint: model.NetworkEndpoint{
								UID:      uid,
								Address:  ep.Address,
								Port:     int(epPort),
								Locality: ep.Locality,
								LbWeight: ep.Weight,
								Network:  ep.Network,
								ServicePort: &model.Port{
									Name:     svcPort.Name,
									Port:     int(svcPort.Number),
									Protocol: protocol.Instance(svcPort.Protocol),
								},
							},
							// ServiceAccount is retrieved in Kube/Controller
							// using IdentityPodAnnotation
							//ServiceAccount, Address: ???, //TODO: nino-k
							Service: &model.Service{
								Hostname:   host.Name(h),
								Ports:      convertServicePorts(se.Ports),
								Resolution: model.Resolution(int(se.Resolution)),
								Attributes: model.ServiceAttributes{
									Name:      conf.Name,
									Namespace: conf.Namespace,
									UID:       uid,
								},
							},
							Labels: labels.Instance(conf.Labels),
						}
						out = append(out, si)
					}
				}
				// and add notReady endpoints
				out = append(out, d.serviceInstancesFromNotReadyEps("", se, svcPort, conf, h)...)
			}
		}
	}
	return
}

func convertServicePorts(ports []*networking.Port) model.PortList {
	out := make(model.PortList, 0)
	for _, port := range ports {
		out = append(out, &model.Port{
			Name:     port.Name,
			Port:     int(port.Number),
			Protocol: protocol.Instance(port.Protocol),
		})
	}

	return out
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
