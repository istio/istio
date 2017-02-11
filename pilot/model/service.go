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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package model

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Service describes an Istio service (e.g., catalog.mystore.com:8080)
// Each service has a fully qualified domain name (FQDN) and one or more
// ports where the service is listening for connections. *Optionally*, a
// service can have a single load balancer/virtual IP address associated
// with it, such that the DNS queries for the FQDN resolves to the virtual
// IP address (a load balancer IP).
//
// E.g., in kubernetes, a service foo is associated with
// foo.default.svc.cluster.local hostname, has a virtual IP of 10.0.1.1 and
// listens on ports 80, 8080
type Service struct {
	// Hostname of the service, e.g. "catalog.mystore.com"
	Hostname string `json:"hostname"`

	// Address specifies the service IPv4 address of the load balancer
	Address string `json:"address,omitempty"`

	// Ports is the set of network ports where the service is listening for
	// connections
	Ports PortList `json:"ports,omitempty"`
}

// Port represents a network port where a service is listening for
// connections. The port should be annotated with the type of protocol
// used by the port.
type Port struct {
	// Name ascribes a human readable name for the port object. When a
	// service has multiple ports, the name field is mandatory
	Name string `json:"name,omitempty"`

	// Port number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service. See NetworkEndpoint definition below.
	Port int `json:"port"`

	// Protocol to be used for the port.
	Protocol Protocol `json:"protocol,omitempty"`
}

// PortList is a set of ports
type PortList []*Port

// Protocol defines network protocols for ports
type Protocol string

// Protocols used by the services
const (
	ProtocolGRPC  Protocol = "GRPC"
	ProtocolHTTPS Protocol = "HTTPS"
	ProtocolHTTP2 Protocol = "HTTP2"
	ProtocolHTTP  Protocol = "HTTP"
	ProtocolTCP   Protocol = "TCP"
	ProtocolUDP   Protocol = "UDP"
)

// NetworkEndpoint defines a network address (IP:port) associated with an instance of the
// service. A service has one or more instances each running in a
// container/VM/pod. If a service has multiple ports, then the same
// instance IP is expected to be listening on multiple ports (one per each
// service port). Note that the port associated with an instance does not
// have to be the same as the port associated with the service. Depending
// on the network setup (NAT, overlays), this could vary.
//
// For e.g., if catalog.mystore.com is accessible through port 80 and 8080,
// and it maps to an instance with IP 172.16.0.1, such that connections to
// port 80 are forwarded to port 55446, and connections to port 8080 are
// forwarded to port 33333,
//
// then internally, we have two two endpoint structs for the
// service catalog.mystore.com
//  --> 172.16.0.1:54546 (with ServicePort pointing to 80) and
//  --> 172.16.0.1:33333 (with ServicePort pointing to 8080)
type NetworkEndpoint struct {
	// Address of the network endpoint, typically an IPv4 address
	Address string `json:"ip_address,omitempty"`

	// Port number where this instance is listening for connections This
	// need not be the same as the port where the service is accessed.
	// e.g., catalog.mystore.com:8080 -> 172.16.0.1:55446
	Port int `json:"port"`

	// Port declaration from the service declaration This is the port for
	// the service associated with this instance (e.g.,
	// catalog.mystore.com)
	ServicePort *Port `json:"port"`
}

// Tag is non empty set of arbitrary strings. Each version of a service can
// be differentiated by a unique set of tags associated with the
// version. These tags are assigned to all instances of a particular
// service version. For example, lets say catalog.mystore.com has 2
// versions v1 and v2. v1 instances could have tags gitCommit=aeiou234,
// region=us-east, while v2 instances could have tags
// name=kittyCat,region=us-east.
type Tag map[string]string

// TagList is a set of tags
//
// FIXME rename to something else for clarity. Its not clear why one needs
// TagList when Tag by itself is a set of key=value pairs
type TagList []Tag

// ServiceInstance represents an individual instance of a specific version
// of a service. It binds a network endpoint (ip:port), the service
// description (which is oblivious to various versions) and a set of tags
// that describe the service version associated with this instance.
//
// For example, the set of service instances associated with catalog.mystore.com
// are modeled like this
//      --> NetworkEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Tag(foo=bar)
//      --> NetworkEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Tag(foo=bar)
//      --> NetworkEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Tag(kitty=cat)
//      --> NetworkEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Tag(kitty=cat)
type ServiceInstance struct {
	Endpoint NetworkEndpoint `json:"endpoint,omitempty"`
	Service  *Service        `json:"service,omitempty"`
	Tag      Tag             `json:"tag,omitempty"`
}

// ServiceDiscovery enumerates Istio service instances.
type ServiceDiscovery interface {
	// Services list declarations of all services in the system
	Services() []*Service

	// GetService retrieves a service by host name if it exists
	GetService(hostname string) (*Service, bool)

	// Instances retrieves instances for a service and its ports that match
	// any of the supplied tags. All instances match an empty tag list.
	//
	// For example, consider the example of catalog.mystore.com as described in NetworkEndpoints
	// Instances(catalog.myservice.com, 80) ->
	//      --> NetworkEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Tag(foo=bar)
	//      --> NetworkEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Tag(foo=bar)
	//      --> NetworkEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Tag(kitty=cat)
	//      --> NetworkEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Tag(kitty=cat)
	//
	// Calling Instances with specific tags returns a trimmed list.
	// e.g., Instances(catalog.myservice.com, 80, foo=bar) ->
	//      --> NetworkEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Tag(foo=bar)
	//      --> NetworkEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Tag(foo=bar)
	//
	// Similar concepts apply for calling this function with a specific
	// port, hostname and tags.
	Instances(hostname string, ports []string, tags TagList) []*ServiceInstance

	// HostInstances lists service instances for a given set of IPv4 addresses.
	HostInstances(addrs map[string]bool) []*ServiceInstance
}

// SubsetOf is true if the tag has identical values for the keys
func (tag Tag) SubsetOf(that Tag) bool {
	for k, v := range tag {
		if that[k] != v {
			return false
		}
	}
	return true
}

// HasSubsetOf returns true if the input tag is a super set of one of the
// tags in the list or if the tag list is empty
func (tags TagList) HasSubsetOf(that Tag) bool {
	if len(tags) == 0 {
		return true
	}
	for _, tag := range tags {
		if tag.SubsetOf(that) {
			return true
		}
	}
	return false
}

// GetNames returns port names
func (ports PortList) GetNames() []string {
	names := make([]string, 0)
	for _, port := range ports {
		names = append(names, port.Name)
	}
	return names
}

// Get retrieves a port declaration by name
func (ports PortList) Get(name string) (*Port, bool) {
	for _, port := range ports {
		if port.Name == name {
			return port, true
		}
	}
	return nil, false
}

// Key generates a unique string referencing service instances for a given port and a tag
func (s *Service) Key(port *Port, tag Tag) string {
	// TODO: check port is non nil and membership of port in service
	return ServiceKey(s.Hostname, PortList{port}, TagList{tag})
}

// ServiceKey generates a service key for a collection of ports and tags
func ServiceKey(hostname string, servicePorts PortList, serviceTags TagList) string {
	// example: name.namespace:http:env=prod;env=test,version=my-v1
	var buffer bytes.Buffer
	buffer.WriteString(hostname)
	np := len(servicePorts)
	nt := len(serviceTags)

	if nt == 1 && serviceTags[0] == nil {
		nt = 0
	}

	if np == 0 && nt == 0 {
		return buffer.String()
	} else if np == 1 && nt == 0 && servicePorts[0].Name == "" {
		return buffer.String()
	} else {
		buffer.WriteString(":")
	}

	if np > 0 {
		ports := make([]string, np)
		for i := 0; i < np; i++ {
			ports[i] = servicePorts[i].Name
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
			tags[i] = serviceTags[i].String()
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

// ParseServiceKey is the inverse of the Service.String() method
func ParseServiceKey(s string) (hostname string, ports PortList, tags TagList) {
	parts := strings.Split(s, ":")
	hostname = parts[0]

	var names []string
	if len(parts) > 1 {
		names = strings.Split(parts[1], ",")
	} else {
		names = []string{""}
	}

	for _, name := range names {
		ports = append(ports, &Port{Name: name})
	}

	if len(parts) > 2 && len(parts[2]) > 0 {
		for _, tag := range strings.Split(parts[2], ";") {
			tags = append(tags, ParseTagString(tag))
		}
	}
	return
}

func (tag Tag) String() string {
	labels := make([]string, 0)
	for k, v := range tag {
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

// ParseTagString extracts a tag from a string
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
