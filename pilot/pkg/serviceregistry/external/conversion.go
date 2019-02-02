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

package external

import (
	"net"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: model.ParseProtocol(port.Protocol),
	}
}

func convertServices(config model.Config) []*model.Service {
	serviceEntry := config.Spec.(*networking.ServiceEntry)
	creationTime := config.CreationTimestamp

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

	var exportTo map[model.Visibility]bool
	if len(serviceEntry.ExportTo) > 0 {
		exportTo = make(map[model.Visibility]bool)
		for _, e := range serviceEntry.ExportTo {
			exportTo[model.Visibility(e)] = true
		}
	}

	for _, host := range serviceEntry.Hosts {
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
						Hostname:     model.Hostname(host),
						Address:      newAddress,
						Ports:        svcPorts,
						Resolution:   resolution,
						Attributes: model.ServiceAttributes{
							Name:      host,
							Namespace: config.Namespace,
							ExportTo:  exportTo,
						},
					})
				} else if net.ParseIP(address) != nil {
					out = append(out, &model.Service{
						CreationTime: creationTime,
						MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
						Hostname:     model.Hostname(host),
						Address:      address,
						Ports:        svcPorts,
						Resolution:   resolution,
						Attributes: model.ServiceAttributes{
							Name:      host,
							Namespace: config.Namespace,
							ExportTo:  exportTo,
						},
					})
				}
			}
		} else {
			out = append(out, &model.Service{
				CreationTime: creationTime,
				MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
				Hostname:     model.Hostname(host),
				Address:      model.UnspecifiedIP,
				Ports:        svcPorts,
				Resolution:   resolution,
				Attributes: model.ServiceAttributes{
					Name:      host,
					Namespace: config.Namespace,
					ExportTo:  exportTo,
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
		// TODO ServiceAccount
		Service: service,
		Labels:  endpoint.Labels,
	}
}

func convertInstances(config model.Config) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	serviceEntry := config.Spec.(*networking.ServiceEntry)
	for _, service := range convertServices(config) {
		for _, serviceEntryPort := range serviceEntry.Ports {
			if len(serviceEntry.Endpoints) == 0 &&
				serviceEntry.Resolution == networking.ServiceEntry_DNS {
				// when service entry has discovery type DNS and no endpoints
				// we create endpoints from service's host
				// Do not use serviceentry.hosts as a service entry is converted into
				// multiple services (one for each host)
				out = append(out, &model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address:     string(service.Hostname),
						Port:        int(serviceEntryPort.Number),
						ServicePort: convertPort(serviceEntryPort),
					},
					// TODO ServiceAccount
					Service: service,
					Labels:  nil,
				})
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					out = append(out, convertEndpoint(service, serviceEntryPort, endpoint))
				}
			}
		}
	}
	return out
}
