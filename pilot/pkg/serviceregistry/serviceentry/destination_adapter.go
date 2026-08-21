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
	"net/netip"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	destination "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
)

// DestinationInputs is the dual-run source-neutral projection of one
// ServiceEntry. Existing registry ownership remains unchanged.
type DestinationInputs struct {
	Frontends    []destination.FrontendDefinition
	Destinations []destination.DestinationDefinition
}

// ConvertToDestinationInputs separates addressable frontend intent from
// outbound destination intent. Each host/address remains a frontend, while
// each host/port has one reusable destination definition.
func ConvertToDestinationInputs(cfg config.Config) DestinationInputs {
	serviceEntry, ok := cfg.Spec.(*networking.ServiceEntry)
	if !ok || serviceEntry == nil {
		return DestinationInputs{}
	}
	source := model.ConfigKey{Kind: kind.ServiceEntry, Namespace: cfg.Namespace, Name: cfg.Name}
	resolution := serviceEntryResolution(serviceEntry.Resolution)
	ports := make([]destination.DestinationPort, 0, len(serviceEntry.Ports))
	for _, port := range serviceEntry.Ports {
		ports = append(ports, destination.DestinationPort{
			Name: port.Name, Number: int(port.Number), Protocol: protocol.Parse(port.Protocol),
		})
	}
	exportTo := make([]visibility.Instance, 0, len(serviceEntry.ExportTo))
	for _, value := range serviceEntry.ExportTo {
		exportTo = append(exportTo, visibility.Instance(value))
	}
	selector := map[string]string(nil)
	if serviceEntry.WorkloadSelector != nil {
		selector = serviceEntry.WorkloadSelector.Labels
	}
	addresses := normalizedServiceEntryAddresses(serviceEntry.Addresses)
	if len(addresses) == 0 {
		addresses = []string{constants.UnspecifiedIP}
	}

	result := DestinationInputs{}
	for _, hostname := range serviceEntry.Hosts {
		definitions := make([]destination.DefinitionID, 0, len(serviceEntry.Ports))
		for n, servicePort := range serviceEntry.Ports {
			id := destination.DefinitionID{
				Source: source, UID: cfg.UID, Port: hostname + "/" + servicePort.Name,
			}
			definitions = append(definitions, id)
			endpointPort := servicePort.TargetPort
			if endpointPort == 0 {
				endpointPort = servicePort.Number
			}
			endpointSource := serviceEntryEndpointSource(source, host.Name(hostname), endpointPort, serviceEntry.Resolution)
			result.Destinations = append(result.Destinations, destination.DestinationDefinition{
				ID: id, Namespace: cfg.Namespace, Ports: []destination.DestinationPort{ports[n]},
				Endpoints:    endpointSource,
				Connection:   destination.ConnectionPolicy{Protocol: ports[n].Protocol},
				MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
				Metadata: destination.DestinationMetadata{
					ServiceAccounts: append([]string(nil), serviceEntry.SubjectAltNames...),
					Extension:       "serviceentry/" + serviceEntry.Resolution.String(),
				},
				Dependencies: []model.ConfigKey{source}, CreationTime: cfg.CreationTimestamp, Version: cfg.ResourceVersion,
			})
		}
		for _, address := range addresses {
			result.Frontends = append(result.Frontends, destination.FrontendDefinition{
				ID: source, UID: cfg.UID, Hostname: host.Name(hostname), Addresses: []string{address},
				DefaultAddress: address, Ports: append([]destination.DestinationPort(nil), ports...),
				DefaultDestinations: append([]destination.DefinitionID(nil), definitions...),
				Resolution:          resolution, MeshExternal: serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
				ServiceAccounts: append([]string(nil), serviceEntry.SubjectAltNames...),
				SubjectAltNames: append([]string(nil), serviceEntry.SubjectAltNames...),
				ExportTo:        append([]visibility.Instance(nil), exportTo...), Selector: selector,
				Type: "ServiceEntry", CreationTime: cfg.CreationTimestamp, Version: cfg.ResourceVersion,
			})
		}
	}
	return result
}

func serviceEntryResolution(resolution networking.ServiceEntry_Resolution) model.Resolution {
	switch resolution {
	case networking.ServiceEntry_NONE:
		return model.Passthrough
	case networking.ServiceEntry_DNS:
		return model.DNSLB
	case networking.ServiceEntry_DNS_ROUND_ROBIN:
		return model.DNSRoundRobinLB
	case networking.ServiceEntry_DYNAMIC_DNS:
		return model.DynamicDNS
	default:
		return model.ClientSideLB
	}
}

func serviceEntryEndpointSource(
	source model.ConfigKey,
	hostname host.Name,
	port uint32,
	resolution networking.ServiceEntry_Resolution,
) destination.EndpointSource {
	switch resolution {
	case networking.ServiceEntry_DNS, networking.ServiceEntry_DNS_ROUND_ROBIN:
		return destination.EndpointSource{Kind: destination.DNS, Source: source, Hostname: hostname, Port: port,
			Extension: resolution.String()}
	case networking.ServiceEntry_DYNAMIC_DNS:
		return destination.EndpointSource{Kind: destination.DynamicDNS, Source: source, Hostname: hostname, Port: port}
	case networking.ServiceEntry_NONE:
		return destination.EndpointSource{Kind: destination.ExtensionResolved, Source: source, Port: port,
			Extension: "serviceentry/passthrough"}
	default:
		return destination.EndpointSource{Kind: destination.StaticEndpoints, Source: source, Port: port,
			Extension: "serviceentry/static"}
	}
}

func normalizedServiceEntryAddresses(addresses []string) []string {
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if ip, err := netip.ParseAddr(address); err == nil {
			result = append(result, ip.String())
			continue
		}
		if prefix, err := netip.ParsePrefix(address); err == nil {
			if prefix.Bits() == prefix.Addr().BitLen() {
				result = append(result, prefix.Addr().String())
			} else {
				result = append(result, prefix.String())
			}
		}
	}
	return result
}
