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

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/spiffe"
)

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
							ServiceRegistry: string(serviceregistry.External),
							Name:            hostname,
							Namespace:       cfg.Namespace,
							ExportTo:        exportTo,
						},
						ServiceAccounts: serviceEntry.SubjectAltNames,
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
							ServiceRegistry: string(serviceregistry.External),
							Name:            hostname,
							Namespace:       cfg.Namespace,
							ExportTo:        exportTo,
						},
						ServiceAccounts: serviceEntry.SubjectAltNames,
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
					ServiceRegistry: string(serviceregistry.External),
					Name:            hostname,
					Namespace:       cfg.Namespace,
					ExportTo:        exportTo,
				},
				ServiceAccounts: serviceEntry.SubjectAltNames,
			})
		}
	}

	return out
}

func convertEndpoint(service *model.Service, servicePort *networking.Port,
	endpoint *networking.WorkloadEntry, tlsMode string) *model.ServiceInstance {
	var instancePort uint32
	addr := endpoint.GetAddress()
	if strings.HasPrefix(addr, model.UnixAddressPrefix) {
		instancePort = 0
		addr = strings.TrimPrefix(addr, model.UnixAddressPrefix)
	} else {
		instancePort = endpoint.Ports[servicePort.Name]
		if instancePort == 0 {
			instancePort = servicePort.Number
		}
	}

	sa := ""
	if endpoint.ServiceAccount != "" {
		sa = spiffe.MustGenSpiffeURI(service.Attributes.Namespace, endpoint.ServiceAccount)
	}
	return &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         addr,
			EndpointPort:    instancePort,
			ServicePortName: servicePort.Name,
			Network:         endpoint.Network,
			Locality: model.Locality{
				Label: endpoint.Locality,
			},
			LbWeight:       endpoint.Weight,
			Labels:         endpoint.Labels,
			TLSMode:        tlsMode,
			ServiceAccount: sa,
		},
		Service:     service,
		ServicePort: convertPort(servicePort),
	}
}

// convertWorkloadInstances translates a WorkloadEntry into ServiceInstances. This logic is largely the
// same as the ServiceEntry convertInstances.
func convertWorkloadInstances(wle *networking.WorkloadEntry, services []*model.Service, se *networking.ServiceEntry) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range services {
		for _, port := range se.Ports {

			// * Use security.istio.io/tlsMode if its present
			// * If not, set TLS mode if ServiceAccount is specified
			tlsMode := model.DisabledTLSModeLabel
			if val, exists := wle.Labels[label.TLSMode]; exists {
				tlsMode = val
			} else if wle.ServiceAccount != "" {
				tlsMode = model.IstioMutualTLSModeLabel
			}
			ep := convertEndpoint(service, port, wle, tlsMode)
			out = append(out, ep)
		}
	}
	return out
}

func convertInstances(cfg model.Config, services []*model.Service) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	if services == nil {
		services = convertServices(cfg)
	}
	for _, service := range services {
		for _, serviceEntryPort := range serviceEntry.Ports {
			if len(serviceEntry.Endpoints) == 0 &&
				serviceEntry.Resolution == networking.ServiceEntry_DNS {
				// when service entry has discovery type DNS and no endpoints
				// we create endpoints from service's host
				// Do not use serviceentry.hosts as a service entry is converted into
				// multiple services (one for each host)
				out = append(out, &model.ServiceInstance{
					Endpoint: &model.IstioEndpoint{
						Address:         string(service.Hostname),
						EndpointPort:    serviceEntryPort.Number,
						ServicePortName: serviceEntryPort.Name,
						Labels:          nil,
						TLSMode:         model.DisabledTLSModeLabel,
					},
					Service:     service,
					ServicePort: convertPort(serviceEntryPort),
				})
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					tlsMode := model.GetTLSModeFromEndpointLabels(endpoint.Labels)
					out = append(out, convertEndpoint(service, serviceEntryPort, endpoint, tlsMode))
				}
			}
		}
	}
	return out
}

// The foreign service instance has pointer to the foreign service and its service port.
// We need to create our own but we can retain the endpoint already created.
func convertForeignServiceInstances(foreignInstance *model.ServiceInstance, serviceEntryServices []*model.Service,
	serviceEntry *networking.ServiceEntry) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range serviceEntryServices {
		for _, serviceEntryPort := range serviceEntry.Ports {
			ep := *foreignInstance.Endpoint
			ep.ServicePortName = serviceEntryPort.Name
			out = append(out, &model.ServiceInstance{
				Endpoint:    &ep,
				Service:     service,
				ServicePort: convertPort(serviceEntryPort),
			})
		}
	}
	return out
}
