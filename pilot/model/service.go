// Copyright 2016 Google Inc.
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

package model

import (
	"bytes"
	"sort"
	"strings"
)

// ServiceDiscovery enumerates Istio service instances
type ServiceDiscovery interface {
	// Services list all services and their tags
	Services() []*Service
	// Endpoints retrieves service instances for a service.
	// The query takes a union across a set of tags and a set of named ports
	// defined in the service parameter.
	// Empty tag set or port set implies the union of all available tags and
	// ports, respectively.
	Endpoints(s *Service) []*ServiceInstance
}

// Service describes an Istio service
type Service struct {
	// Name of the service
	Name string `json:"name"`
	// Namespace of the service name, optional
	Namespace string `json:"namespace,omitempty"`
	// Tags is a set of declared tags for the service.
	// An empty set is allowed but tag values must be non-empty strings.
	Tags []string `json:"tags,omitempty"`
	// Ports is a set of declared network ports for the service.
	// Port value is the service port.
	Ports []Port `json:"ports"`
}

// Endpoint defines a network endpoint
type Endpoint struct {
	// Address of the endpoint, typically an IP address
	Address string `json:"ip_address,omitempty"`
	// Port on the host address
	Port Port `json:"port"`
}

// Port represents a network port
type Port struct {
	// Port value
	Port int `json:"port"`
	// Name of the port classifies ports for a single service, optional
	Name string `json:"name,omitempty"`
	// Protocol to be used for the port
	Protocol Protocol `json:"protocol,omitempty"`
}

// Protocol defines network protocols for ports
type Protocol string

const (
	ProtocolGRPC  Protocol = "GRPC"
	ProtocolHTTPS Protocol = "HTTPS"
	ProtocolHTTP  Protocol = "HTTP"
	ProtocolTCP   Protocol = "TCP"
	ProtocolUDP   Protocol = "UDP"
)

// ServiceInstance binds an endpoint to a service and a tag.
// If the service has no tags, the tag value is an empty string;
// otherwise, the tag value is an element in the set of service tags.
type ServiceInstance struct {
	Endpoint Endpoint `json:"endpoint,omitempty"`
	Service  *Service `json:"service,omitempty"`
	Tag      string   `json:"tag,omitempty"`
}

func (s *Service) String() string {
	// example: name.namespace:http:my-v1,prod
	var buffer bytes.Buffer
	buffer.WriteString(s.Name)
	if len(s.Namespace) > 0 {
		buffer.WriteString(".")
		buffer.WriteString(s.Namespace)
	}

	np := len(s.Ports)
	nt := len(s.Tags)

	if np > 0 || nt > 0 {
		buffer.WriteString(":")
	}

	if np > 0 {
		ports := make([]string, np)
		for i := 0; i < np; i++ {
			ports[i] = s.Ports[i].Name
		}
		sort.Strings(ports)
		for i := 0; i < np; i++ {
			if i > 0 {
				buffer.WriteString(",")
			}
			buffer.WriteString(ports[i])
		}
	}

	if nt > 0 {
		buffer.WriteString(":")
		tags := make([]string, nt)
		copy(tags, s.Tags)
		sort.Strings(tags)
		for i := 0; i < nt; i++ {
			if i > 0 {
				buffer.WriteString(",")
			}
			buffer.WriteString(tags[i])
		}
	}
	return buffer.String()
}

// ParseServiceString is the inverse of the Service.String() method
func ParseServiceString(s string) *Service {
	parts := strings.Split(s, ":")
	var name, namespace string
	var names, tags []string

	dot := strings.Index(parts[0], ".")
	if dot < 0 {
		name = parts[0]
	} else {
		name = parts[0][:dot]
		namespace = parts[0][dot+1:]
	}

	if len(parts) > 1 {
		names = strings.Split(parts[1], ",")
		if len(parts) > 2 {
			tags = strings.Split(parts[2], ",")
		}
	}

	var ports []Port
	for _, name := range names {
		ports = append(ports, Port{Name: name})
	}

	return &Service{
		Name:      name,
		Namespace: namespace,
		Ports:     ports,
		Tags:      tags,
	}
}
