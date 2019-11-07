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
}

// NewMCPDiscovery provides a new instance of Discovery
func NewMCPDiscovery(controller CoreDataModel, options *DiscoveryOptions) *MCPDiscovery {
	return &MCPDiscovery{
		SyntheticServiceEntryController: controller.(*SyntheticServiceEntryController),
		DiscoveryOptions:                options,
	}
}

// Services list declarations of all SyntheticServiceEntries in the system
func (d *MCPDiscovery) Services() ([]*model.Service, error) {
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
	out := make([]*model.ServiceInstance, 0)

	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]

	// add not ready endpoint
	d.notReadyEndpointsMu.Lock()
	defer d.notReadyEndpointsMu.Unlock()
	for addr, conf := range d.notReadyEndpoints {
		notReadyIP, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		notReadyPort, err := strconv.Atoi(port)
		if proxyIP != notReadyIP || err != nil {
			continue
		}
		se, ok := conf.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, errors.New("getProxyServiceInstances: wrong type")
		}
		for _, h := range se.Hosts {
			for _, svcPort := range se.Ports {
				out = append(out, d.notReadyServiceInstance(se, svcPort, conf, h, notReadyIP, notReadyPort))
			}
		}
	}

	// add any other endpoint
	svcInstances, err := d.serviceInstancesFromConfig(proxy.ConfigNamespace, proxy.IPAddresses[0])
	if err != nil {
		return nil, err
	}
	out = append(out, svcInstances...)
	return out, nil
}

// InstancesByPort implements a service catalog operation
func (d *MCPDiscovery) InstancesByPort(svc *model.Service, servicePort int, lbls labels.Collection) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)

	d.notReadyEndpointsMu.Lock()
	defer d.notReadyEndpointsMu.Unlock()

	for addr, conf := range d.notReadyEndpoints {
		notReadyIP, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		notReadyPort, err := strconv.Atoi(port)
		if err != nil {
			continue
		}
		se, ok := conf.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, errors.New("instancesByPort: wrong type")
		}

		for _, svcPort := range se.Ports {
			for _, h := range se.Hosts {
				if host.Name(h) == svc.Hostname && servicePort == int(svcPort.Number) {
					// add not ready endpoint
					out = append(out, d.notReadyServiceInstance(se, svcPort, conf, h, notReadyIP, notReadyPort))
				}
			}
		}
	}
	// add other endpoints
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
			for _, h := range se.Hosts {
				for _, svcPort := range se.Ports {
					if host.Name(h) == svc.Hostname && int(svcPort.Number) == servicePort {
						var uid string
						if d.mixerEnabled() {
							uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
						}
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
					}
				}
			}
		}
	}
	return out, nil
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

func (d *MCPDiscovery) serviceInstancesFromConfig(namespace, proxyIP string) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)
	sseConf, err := d.List(schemas.SyntheticServiceEntry.Type, namespace)
	if err != nil {
		return nil, err
	}

	for _, conf := range sseConf {
		se, ok := conf.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, errors.New("serviceInstancesFromConfig: wrong type")
		}
		for _, h := range se.Hosts {
			for _, svcPort := range se.Ports {
				// add other endpoints
				out = append(out, d.serviceInstancesByProxyIP(proxyIP, se, svcPort, conf, h)...)
			}
		}
	}
	return out, nil

}

func (d *MCPDiscovery) notReadyServiceInstance(
	se *networking.ServiceEntry,
	svcPort *networking.Port,
	conf *model.Config,
	hostname,
	ip string,
	port int) *model.ServiceInstance {
	var uid string
	if d.mixerEnabled() {
		uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
	}
	out := &model.ServiceInstance{
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
	return out
}

func (d *MCPDiscovery) serviceInstancesByProxyIP(
	proxyIP string,
	se *networking.ServiceEntry,
	svcPort *networking.Port,
	conf model.Config,
	hostname string) (out []*model.ServiceInstance) {
	var uid string
	if d.mixerEnabled() {
		uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
	}
	for _, ep := range se.Endpoints {
		if ep.Address == proxyIP {
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
	}
	return out
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
