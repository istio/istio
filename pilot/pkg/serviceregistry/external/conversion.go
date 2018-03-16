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
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:                 port.Name,
		Port:                 int(port.Number),
		Protocol:             convertProtocol(port.Protocol),
		AuthenticationPolicy: meshconfig.AuthenticationPolicy_NONE,
	}
}

func convertService(externalService *networking.ExternalService) []*model.Service {
	out := make([]*model.Service, 0)

	for _, host := range externalService.Hosts {
		var resolution model.Resolution
		switch externalService.Discovery {
		case networking.ExternalService_NONE:
			resolution = model.Passthrough
		case networking.ExternalService_DNS:
			resolution = model.DNSLB
		case networking.ExternalService_STATIC:
			resolution = model.ClientSideLB
		}

		svcPorts := make(model.PortList, 0, len(externalService.Ports))
		for _, port := range externalService.Ports {
			svcPorts = append(svcPorts, convertPort(port))
		}

		out = append(out, &model.Service{
			MeshExternal: true,
			Hostname:     host,
			Ports:        svcPorts,
			Resolution:   resolution,
		})
	}

	return out
}

func convertNetworkEndpoint(services []*model.Service, servicePort *networking.Port, endpoint *networking.ExternalService_Endpoint) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range services {
		instancePort := endpoint.Ports[servicePort.Name]
		if instancePort == 0 {
			instancePort = servicePort.Number
		}

		serviceInstance := &model.ServiceInstance{
			Endpoint: model.NetworkEndpoint{
				Address:     endpoint.Address,
				Port:        int(instancePort),
				ServicePort: convertPort(servicePort),
			},
			// TODO AvailabilityZone
			Service: service,
			Labels:  endpoint.Labels,
		}
		out = append(out, serviceInstance)
	}
	return out
}

func convertInstances(externalService *networking.ExternalService) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	services := convertService(externalService)

	for _, endpoint := range externalService.Endpoints {
		for _, servicePort := range externalService.Ports {

			out = append(out, convertNetworkEndpoint(services, servicePort, endpoint)...)
		}
	}
	return out
}

// parseHostname extracts service name from the service hostname
func parseHostname(hostname string) (string, error) {
	// TODO does the hostname even need to be parsed?
	return hostname, nil
}

func convertProtocol(name string) model.Protocol {
	protocol := model.ConvertCaseInsensitiveStringToProtocol(name)
	if protocol == model.ProtocolUnsupported {
		log.Warnf("unsupported protocol value: %s", name)
		return model.ProtocolTCP
	}
	return protocol
}
