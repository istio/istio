// Copyright 2017 Google Inc.
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
	"fmt"
	"sort"
	"strings"
)

// ServiceDiscovery enumerates Istio service instances
type ServiceDiscovery interface {
	// Services list declarations of all services and their tags
	Services() []*Service
	// Instances takes a union across a set of tags and a set of named ports
	// defined in the service parameter. An empty tag set or a port set implies
	// the union of all available tags and ports, respectively.
	Instances(s *Service) []*ServiceInstance
	// HostInstances lists service instances for a given set of IPv4 addresses.
	HostInstances(addrs map[string]bool) []*ServiceInstance
}

// Service describes an Istio service
type Service struct {
	// Name of the service
	Name string `json:"name"`
	// Namespace of the service name, optional
	Namespace string `json:"namespace,omitempty"`
	// Tags is a set of declared distinct tags for the service
	Tags []Tag `json:"tags,omitempty"`
	// Ports is a set of declared network service ports
	Ports []Port `json:"ports,omitempty"`
}

// Tag describes an Istio service tag which provides finer-grained control
// over the set of service endpoints.
// Tag is a non-empty set of key-value pairs.
type Tag map[string]string

// Endpoint defines a network endpoint
type Endpoint struct {
	// Address of the endpoint, typically an IP address
	Address string `json:"ip_address,omitempty"`
	// Port on the host address
	Port Port `json:"port"`
}

// ServiceInstance binds an endpoint to a service and a tag.
type ServiceInstance struct {
	Endpoint Endpoint `json:"endpoint,omitempty"`
	Service  *Service `json:"service,omitempty"`
	Tag      *Tag     `json:"tag,omitempty"`
}

// Port represents a network port
type Port struct {
	// Port value (may be distinct from the service port with the same name)
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
	ProtocolHTTP2 Protocol = "HTTP2"
	ProtocolHTTP  Protocol = "HTTP"
	ProtocolTCP   Protocol = "TCP"
	ProtocolUDP   Protocol = "UDP"
)

func (s *Service) String() string {
	// example: name.namespace:http:env=prod;env=test,version=my-v1
	var buffer bytes.Buffer
	buffer.WriteString(s.Name)
	if len(s.Namespace) > 0 {
		buffer.WriteString(".")
		buffer.WriteString(s.Namespace)
	}

	np := len(s.Ports)
	nt := len(s.Tags)

	if np == 0 && nt == 0 {
		return buffer.String()
	} else if np == 1 && nt == 0 && s.Ports[0].Name == "" {
		return buffer.String()
	} else {
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
		for i := 0; i < nt; i++ {
			tags[i] = s.Tags[i].String()
		}
		sort.Strings(tags)
		for i := 0; i < nt; i++ {
			if i > 0 {
				buffer.WriteString(";")
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

	dot := strings.Index(parts[0], ".")
	if dot < 0 {
		name = parts[0]
	} else {
		name = parts[0][:dot]
		namespace = parts[0][dot+1:]
	}

	var names []string
	if len(parts) > 1 {
		names = strings.Split(parts[1], ",")
	} else {
		names = []string{""}
	}

	var ports []Port
	for _, name := range names {
		ports = append(ports, Port{Name: name})
	}

	var tags []Tag
	if len(parts) > 2 && len(parts[2]) > 0 {
		for _, tag := range strings.Split(parts[2], ";") {
			tags = append(tags, ParseTagString(tag))
		}
	}

	return &Service{
		Name:      name,
		Namespace: namespace,
		Ports:     ports,
		Tags:      tags,
	}
}

func (t Tag) String() string {
	labels := make([]string, 0)
	for k, v := range t {
		if len(v) > 0 {
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		} else {
			labels = append(labels, k)
		}
	}
	sort.Strings(labels)

	var buffer bytes.Buffer
	var first = true
	for _, label := range labels {
		if !first {
			buffer.WriteString(",")
		} else {
			first = false
		}
		buffer.WriteString(label)
	}
	return buffer.String()
}

func ParseTagString(s string) Tag {
	tag := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		kv := strings.Split(pair, "=")
		if len(kv) > 1 {
			tag[kv[0]] = kv[1]
		} else {
			tag[kv[0]] = ""
		}
	}
	return tag
}
