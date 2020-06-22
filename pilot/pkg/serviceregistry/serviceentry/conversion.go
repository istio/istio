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

package serviceentry

import (
	"net"
	"strings"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/schema/collections"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/spiffe"
)

// TODO: rename 'external' to service_entries or other specific name, the term 'external' is too broad

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: protocol.Parse(port.Protocol),
	}
}

// ServiceToServiceEntry converts from internal Service representation to ServiceEntry
// This does not include endpoints - they'll be represented as EndpointSlice or EDS.
//
// See convertServices() for the reverse conversion, used by Istio to handle ServiceEntry configs.
// See kube.ConvertService for the conversion from K8S to internal Service.
func ServiceToServiceEntry(svc *model.Service) *model.Config {
	gvk := collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind()
	se := &networking.ServiceEntry{
		// Host is fully qualified: name, namespace, domainSuffix
		Hosts: []string{string(svc.Hostname)},

		// Internal Service and K8S Service have a single Address.
		// ServiceEntry can represent multiple - but we are not using that. SE may be merged.
		// Will be 0.0.0.0 if not specified as ClusterIP or ClusterIP==None. In such case resolution is Passthrough.
		//
		Addresses: []string{svc.Address},

		//Location:             0,

		// Internal resolution:
		//  - Passthrough - for ClusterIP=None and no ExternalName
		//  - ClientSideLB - regular ClusterIP clusters (VIP, resolved via EDS)
		//  - DNSLB - if ExternalName is specified. Also meshExternal is set.

		WorkloadSelector: &networking.WorkloadSelector{Labels: svc.Attributes.LabelSelectors},

		// This is based on alpha.istio.io/canonical-serviceaccounts and
		//  alpha.istio.io/kubernetes-serviceaccounts.
		SubjectAltNames: svc.ServiceAccounts,
	}

	// Based on networking.istio.io/exportTo annotation
	for k, v := range svc.Attributes.ExportTo {
		if v {
			// k is Private or Public
			se.ExportTo = append(se.ExportTo, string(k))
		}
	}

	if svc.MeshExternal {
		se.Location = networking.ServiceEntry_MESH_EXTERNAL // 0 - default
	} else {
		se.Location = networking.ServiceEntry_MESH_INTERNAL
	}

	// Reverse in convertServices. Note that enum values are different
	// TODO: make the enum match, should be safe (as long as they're used as enum)
	var resolution networking.ServiceEntry_Resolution
	switch svc.Resolution {
	case model.Passthrough: // 2
		resolution = networking.ServiceEntry_NONE // 0
	case model.DNSLB: // 1
		resolution = networking.ServiceEntry_DNS // 2
	case model.ClientSideLB: // 0
		resolution = networking.ServiceEntry_STATIC // 1
	}
	se.Resolution = resolution

	// Port is mapped from ServicePort
	for _, p := range svc.Ports {
		se.Ports = append(se.Ports, &networking.Port{
			Number: uint32(p.Port),
			Name:   p.Name,
			// Protocol is converted to protocol.Instance - reverse conversion will use the name.
			Protocol: string(p.Protocol),
		})
	}

	cfg := &model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind:  gvk,
			Name:              "synthetic-" + svc.Attributes.Name,
			Namespace:         svc.Attributes.Namespace,
			CreationTimestamp: svc.CreationTime,
		},
		Spec: se,
	}

	// TODO: WorkloadSelector

	// TODO: preserve ServiceRegistry. The reverse conversion sets it to 'external'
	// TODO: preserve UID ? It seems MCP didn't preserve it - but that code path was not used much.

	// TODO: ClusterExternalPorts map - for NodePort services, with "traffic.istio.io/nodeSelector" ann
	// It's a per-cluster map

	// TODO: ClusterExternalAddresses - for LB types, per cluster. Populated from K8S, missing
	// in SE. Used for multi-network support.
	return cfg
}

// convertServices transforms a ServiceEntry config to a list of internal Service objects.
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

	var labelSelectors map[string]string
	if serviceEntry.WorkloadSelector != nil {
		labelSelectors = serviceEntry.WorkloadSelector.Labels
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
							LabelSelectors:  labelSelectors,
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
							LabelSelectors:  labelSelectors,
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
					LabelSelectors:  labelSelectors,
				},
				ServiceAccounts: serviceEntry.SubjectAltNames,
			})
		}
	}

	return out
}

func convertEndpoint(service *model.Service, servicePort *networking.Port,
	endpoint *networking.WorkloadEntry) *model.ServiceInstance {
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

	tlsMode := getTLSModeFromWorkloadEntry(endpoint)
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
			out = append(out, convertEndpoint(service, port, wle))
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
					out = append(out, convertEndpoint(service, serviceEntryPort, endpoint))
				}
			}
		}
	}
	return out
}

func getTLSModeFromWorkloadEntry(wle *networking.WorkloadEntry) string {
	// * Use security.istio.io/tlsMode if its present
	// * If not, set TLS mode if ServiceAccount is specified
	tlsMode := model.DisabledTLSModeLabel
	if val, exists := wle.Labels[label.TLSMode]; exists {
		tlsMode = val
	} else if wle.ServiceAccount != "" {
		tlsMode = model.IstioMutualTLSModeLabel
	}

	return tlsMode
}

// The foreign service instance has pointer to the foreign service and its service port.
// We need to create our own but we can retain the endpoint already created.
// TODO(rshriram): we currently ignore the pod(endpoint) ports and setup 1-1 mapping
// from service port to endpoint port. Need to figure out a way to map k8s pod port to
// appropriate service entry port
func convertForeignServiceInstances(foreignInstance *model.ServiceInstance, serviceEntryServices []*model.Service,
	serviceEntry *networking.ServiceEntry) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range serviceEntryServices {
		for _, serviceEntryPort := range serviceEntry.Ports {
			ep := *foreignInstance.Endpoint
			ep.ServicePortName = serviceEntryPort.Name
			ep.EndpointPort = serviceEntryPort.Number
			ep.EnvoyEndpoint = nil
			out = append(out, &model.ServiceInstance{
				Endpoint:    &ep,
				Service:     service,
				ServicePort: convertPort(serviceEntryPort),
			})
		}
	}
	return out
}

// Convenience function to convert a workloadEntry into a ServiceInstance object encoding the endpoint (without service
// port names) and the namespace - k8s will consume this service instance when selecting workload entries
// TODO(rshriram): we currently ignore the workload entry (endpoint) ports. K8S will setup 1-1 mapping
// from service port to endpoint port. Need to figure out a way to map workload entry port to
// appropriate k8s service port
func convertWorkloadEntryToServiceInstanceForK8S(namespace string,
	we *networking.WorkloadEntry) *model.ServiceInstance {
	addr := we.GetAddress()
	if strings.HasPrefix(addr, model.UnixAddressPrefix) {
		// k8s can't use uds for service objects
		return nil
	}
	tlsMode := getTLSModeFromWorkloadEntry(we)
	sa := ""
	if we.ServiceAccount != "" {
		sa = spiffe.MustGenSpiffeURI(namespace, we.ServiceAccount)
	}
	return &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address: addr,
			Network: we.Network,
			Locality: model.Locality{
				Label: we.Locality,
			},
			LbWeight:       we.Weight,
			Labels:         we.Labels,
			TLSMode:        tlsMode,
			ServiceAccount: sa,
		},
		Service: &model.Service{
			Attributes: model.ServiceAttributes{
				Namespace: namespace,
			},
		},
	}
}
