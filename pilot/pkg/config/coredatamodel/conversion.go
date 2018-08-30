// Copyright 2018 Istio Authors
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
	"strings"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

var ServiceResolutionMapping = map[networking.ServiceEntry_Resolution]model.Resolution{
	networking.ServiceEntry_NONE:   model.Passthrough,
	networking.ServiceEntry_DNS:    model.DNSLB,
	networking.ServiceEntry_STATIC: model.ClientSideLB,
}

func ConvertServices(se *networking.ServiceEntry, namespace string, creationTime time.Time) []*model.Service {
	services := []*model.Service{}
	svcPorts := make(model.PortList, 0, len(se.Ports))
	for _, port := range se.Ports {
		svcPorts = append(svcPorts, convertPort(port))
	}

	resolution := ServiceResolutionMapping[se.Resolution]

	for _, host := range se.GetHosts() {
		addresses := se.GetAddresses()

		var newAddress string
		if len(addresses) > 0 {
			for _, address := range addresses {
				if ip, _, cidrErr := net.ParseCIDR(address); cidrErr == nil {
					newAddress = ip.String()
				} else if net.ParseIP(address) != nil {
					newAddress = address
				}

				services = append(services, &model.Service{
					CreationTime: time.Now(),
					MeshExternal: se.GetLocation() == networking.ServiceEntry_MESH_EXTERNAL,
					Hostname:     model.Hostname(host),
					Address:      newAddress,
					Ports:        svcPorts,
					Resolution:   resolution,
					Attributes: model.ServiceAttributes{
						Name:      host,
						Namespace: namespace,
					},
				})
			}
		} else {
			newAddress = model.UnspecifiedIP

			services = append(services, &model.Service{
				CreationTime: time.Now(),
				MeshExternal: se.GetLocation() == networking.ServiceEntry_MESH_EXTERNAL,
				Hostname:     model.Hostname(host),
				Address:      newAddress,
				Ports:        svcPorts,
				Resolution:   resolution,
				Attributes: model.ServiceAttributes{
					Name:      host,
					Namespace: namespace,
				},
			})
		}
	}

	return services
}

func ConvertInstances(serviceEntry *networking.ServiceEntry, namespace string, creationTime time.Time, filters ...func(*model.ServiceInstance) bool) []*model.ServiceInstance {
	instances := make([]*model.ServiceInstance, 0)
	for _, service := range ConvertServices(serviceEntry, namespace, creationTime) {
		for _, serviceEntryPort := range serviceEntry.Ports {
			if len(serviceEntry.Endpoints) == 0 &&
				serviceEntry.Resolution == networking.ServiceEntry_DNS {
				// when service entry has discovery type DNS and no endpoints
				// we create endpoints from service's host
				// Do not use serviceentry.hosts as a service entry is converted into
				// multiple services (one for each host)
				instance := &model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address:     string(service.Hostname),
						Port:        int(serviceEntryPort.Number),
						ServicePort: convertPort(serviceEntryPort),
					},
					// TODO AvailabilityZone, ServiceAccount
					Service: service,
					Labels:  nil,
				}
				match := true
				for _, filter := range filters {
					match = match && filter(instance)
				}
				if match {
					instances = append(instances, instance)
				}
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					instance := convertEndpoint(service, serviceEntryPort, endpoint)
					match := true
					for _, filter := range filters {
						match = match && filter(instance)
					}
					if match {
						instances = append(instances, instance)
					}
				}
			}
		}
	}
	return instances
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
		},
		// TODO AvailabilityZone, ServiceAccount
		Service: service,
		Labels:  endpoint.Labels,
	}
}
