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

package consul

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/hashicorp/consul/api"

	"istio.io/pilot/model"
)

const (
	protocolTagName = "protocol"
	externalTagName = "external"
)

func convertTags(tags []string) model.Tags {
	out := make(model.Tags, len(tags))
	for _, tag := range tags {
		vals := strings.Split(tag, "|")
		// Tags not of form "key|value" are ignored to avoid possible collisions
		if len(vals) > 1 {
			out[vals[0]] = vals[1]
		} else {
			glog.Warningf("Tag %v ignored since it is not of form key|value", tag)
		}
	}
	return out
}

func convertPort(port int, name string) *model.Port {
	if name == "" {
		name = "http"
	}

	return &model.Port{
		Name:     name,
		Port:     port,
		Protocol: convertProtocol(name),
	}
}

func convertService(endpoints []*api.CatalogService) *model.Service {
	name, addr, external := "", "", ""

	ports := make(map[int]*model.Port)
	for _, endpoint := range endpoints {
		name = endpoint.ServiceName

		port := convertPort(endpoint.ServicePort, endpoint.NodeMeta[protocolTagName])

		if svcPort, exists := ports[port.Port]; exists && svcPort.Protocol != port.Protocol {
			glog.Warningf("Service %v has two instances on same port %v but different protocols (%v, %v)",
				name, port.Port, svcPort.Protocol, port.Protocol)
		} else {
			ports[port.Port] = port
		}

		// TODO This will not work if service is a mix of external and local services
		// or if a service has more than one external name
		if endpoint.NodeMeta[externalTagName] != "" {
			external = endpoint.NodeMeta[externalTagName]
		}
	}

	svcPorts := make(model.PortList, 0, len(ports))
	for _, port := range ports {
		svcPorts = append(svcPorts, port)
	}

	out := &model.Service{
		Hostname:     serviceHostname(name),
		Ports:        svcPorts,
		Address:      addr,
		ExternalName: external,
	}

	return out
}

func convertInstance(instance *api.CatalogService) *model.ServiceInstance {
	tags := convertTags(instance.ServiceTags)
	port := convertPort(instance.ServicePort, instance.NodeMeta[protocolTagName])

	addr := instance.ServiceAddress
	if addr == "" {
		addr = instance.Address
	}

	return &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address:     addr,
			Port:        instance.ServicePort,
			ServicePort: port,
		},

		Service: &model.Service{
			Hostname: serviceHostname(instance.ServiceName),
			Address:  instance.ServiceAddress,
			Ports:    model.PortList{port},
			// TODO ExternalName come from metadata?
			ExternalName: instance.NodeMeta[externalTagName],
		},
		Tags: tags,
	}
}

// serviceHostname produces FQDN for a consul service
func serviceHostname(name string) string {
	// TODO include datacenter in Hostname?
	// consul DNS uses "redis.service.us-east-1.consul" -> "[<optional_tag>].<svc>.service.[<optional_datacenter>].consul"
	return fmt.Sprintf("%s.service.consul", name)
}

// parseHostname extracts service name from the service hostname
func parseHostname(hostname string) (name string, err error) {
	parts := strings.Split(hostname, ".")
	if len(parts) < 1 {
		err = fmt.Errorf("missing service name from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	return
}

func convertProtocol(name string) model.Protocol {
	switch name {
	case "tcp":
		return model.ProtocolTCP
	case "udp":
		return model.ProtocolUDP
	case "grpc":
		return model.ProtocolGRPC
	case "http":
		return model.ProtocolHTTP
	case "http2":
		return model.ProtocolHTTP2
	case "https":
		return model.ProtocolHTTPS
	default:
		return model.ProtocolHTTP
	}

}
