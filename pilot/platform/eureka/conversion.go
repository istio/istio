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

package eureka

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"istio.io/pilot/model"
)

// Convert Eureka applications to services. If provided, only convert applications in the hostnames whitelist,
// otherwise convert all.
func convertServices(apps []*application, hostnames map[string]bool) map[string]*model.Service {
	services := make(map[string]*model.Service)
	for _, app := range apps {
		for _, instance := range app.Instances {
			if len(hostnames) > 0 && !hostnames[instance.Hostname] {
				continue
			}

			if instance.Status != statusUp {
				continue
			}

			ports := convertPorts(instance)
			if len(ports) == 0 {
				continue
			}

			service := services[instance.Hostname]
			if service == nil {
				service = &model.Service{
					Hostname:     instance.Hostname,
					Address:      "",
					Ports:        make(model.PortList, 0),
					ExternalName: "",
				}
				services[instance.Hostname] = service
			}

			protocol := convertProtocol(instance.Metadata)
			for _, port := range ports {
				if servicePort, exists := service.Ports.GetByPort(port.Port); exists {
					if servicePort.Protocol != protocol {
						glog.Warningf(
							"invalid Eureka config: "+
								"%s:%d has conflicting protocol definitions %s, %s",
							instance.Hostname, servicePort.Port,
							servicePort.Protocol, protocol)
					}
					continue
				}

				service.Ports = append(service.Ports, port)
			}
		}
	}
	return services
}

// Convert Eureka applications to service instances. The services argument must contain a map of hostnames to
// services. Only service instances with a corresponding service are converted.
func convertServiceInstances(services map[string]*model.Service, apps []*application) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, app := range apps {
		for _, instance := range app.Instances {
			if services[instance.Hostname] == nil {
				continue
			}

			if instance.Status != statusUp {
				continue
			}

			for _, port := range convertPorts(instance) {
				out = append(out, &model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address:     instance.IPAddress,
						Port:        port.Port,
						ServicePort: port,
					},
					Service: services[instance.Hostname],
					Tags:    convertTags(instance.Metadata),
				})
			}
		}
	}
	return out
}

func convertPorts(instance *instance) model.PortList {
	out := make(model.PortList, 0, 2) // Eureka instances have 0..2 enabled ports
	protocol := convertProtocol(instance.Metadata)
	for _, port := range []port{instance.Port, instance.SecurePort} {
		if !port.Enabled {
			continue
		}

		out = append(out, &model.Port{
			Name:     fmt.Sprint(port.Port),
			Port:     port.Port,
			Protocol: protocol,
		})
	}
	return out
}

const protocolMetadata = "istio.protocol" // metadata key for port protocol

// supported protocol metadata values
const (
	metadataUDP   = "udp"
	metadataTCP   = "tcp"
	metadataHTTP  = "http"
	metadataHTTP2 = "http2"
	metadataHTTPS = "https"
	metadataGRPC  = "grpc"
)

func convertProtocol(md metadata) model.Protocol {
	if md != nil {
		protocol := strings.ToLower(md[protocolMetadata])
		switch protocol {
		case metadataUDP:
			return model.ProtocolUDP
		case metadataTCP:
			return model.ProtocolTCP
		case metadataHTTP:
			return model.ProtocolHTTP
		case metadataHTTP2:
			return model.ProtocolHTTP2
		case metadataHTTPS:
			return model.ProtocolHTTPS
		case metadataGRPC:
			return model.ProtocolGRPC
		case "":
			// fallthrough to default protocol
		default:
			glog.Warningf("unsupported protocol value: %s", protocol)
		}
	}
	return model.ProtocolTCP // default protocol
}

func convertTags(metadata metadata) model.Tags {
	tags := make(model.Tags)
	for k, v := range metadata {
		tags[k] = v
	}

	// filter out special tags
	delete(tags, protocolMetadata)
	delete(tags, "@class")

	return tags
}
