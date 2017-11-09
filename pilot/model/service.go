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

	proxyconfig "istio.io/api/proxy/v1/config"
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

	// ExternalName is only set for external services and holds the external
	// service DNS name.  External services are name-based solution to represent
	// external service instances as a service inside the cluster.
	ExternalName string `json:"external"`

	// ServiceAccounts specifies the service accounts that run the service.
	ServiceAccounts []string `json:"serviceaccounts,omitempty"`

	// LoadBalancingDisabled indicates that no load balancing should be done for this service.
	LoadBalancingDisabled bool `json:"-"`
}

// AuthMigrationPort represents an alias port for a service port that would
// be used for authentication migration.
// As of speaking, this information is extracted from service annotations.
type AuthMigrationPort struct {
	Port                 int                              `json:"port,omitempty"`
	AuthenticationPolicy proxyconfig.AuthenticationPolicy `json:"policy,omitempty"`
	Active               bool                             `json:"active,omitempty"`
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

	// In combine with the mesh's AuthPolicy, controls authentication for
	// Envoy-to-Envoy communication.
	// This value is extracted from service annotation.
	AuthenticationPolicy proxyconfig.AuthenticationPolicy `json:"authentication_policy"`

	// Migration port for authentication.
	// This value is extracted from service annotation.
	AuthMigrationPort *AuthMigrationPort `json:"auth_migration_port,omitempty"`
}

// PortList is a set of ports
type PortList []*Port

// Protocol defines network protocols for ports
type Protocol string

const (
	// ProtocolGRPC declares that the port carries gRPC traffic
	ProtocolGRPC Protocol = "GRPC"
	// ProtocolHTTPS declares that the port carries HTTPS traffic
	ProtocolHTTPS Protocol = "HTTPS"
	// ProtocolHTTP2 declares that the port carries HTTP/2 traffic
	ProtocolHTTP2 Protocol = "HTTP2"
	// ProtocolHTTP declares that the port carries HTTP/1.1 traffic.
	// Note that HTTP/1.0 or earlier may not be supported by the proxy.
	ProtocolHTTP Protocol = "HTTP"
	// ProtocolTCP declares the the port uses TCP.
	// This is the default protocol for a service port.
	ProtocolTCP Protocol = "TCP"
	// ProtocolUDP declares that the port uses UDP.
	// Note that UDP protocol is not currently supported by the proxy.
	ProtocolUDP Protocol = "UDP"
	// ProtocolMongo declares that the port carries mongoDB traffic
	ProtocolMongo Protocol = "Mongo"
	// ProtocolRedis declares that the port carries redis traffic
	ProtocolRedis Protocol = "Redis"
)

// IsHTTP is true for protocols that use HTTP as transport protocol
func (p Protocol) IsHTTP() bool {
	switch p {
	case ProtocolHTTP, ProtocolHTTP2, ProtocolGRPC:
		return true
	default:
		return false
	}
}

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
	ServicePort *Port `json:"service_port"`
}

// Labels is a non empty set of arbitrary strings. Each version of a service can
// be differentiated by a unique set of labels associated with the version. These
// labels are assigned to all instances of a particular service version. For
// example, lets say catalog.mystore.com has 2 versions v1 and v2. v1 instances
// could have labels gitCommit=aeiou234, region=us-east, while v2 instances could
// have labels name=kittyCat,region=us-east.
type Labels map[string]string

// LabelsCollection is a collection of labels used for comparing labels against a
// collection of labels
type LabelsCollection []Labels

// ServiceInstance represents an individual instance of a specific version
// of a service. It binds a network endpoint (ip:port), the service
// description (which is oblivious to various versions) and a set of labels
// that describe the service version associated with this instance.
//
// The labels associated with a service instance are unique per a network endpoint.
// There is one well defined set of labels for each service instance network endpoint.
//
// For example, the set of service instances associated with catalog.mystore.com
// are modeled like this
//      --> NetworkEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
//      --> NetworkEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
//      --> NetworkEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Labels(kitty=cat)
//      --> NetworkEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Labels(kitty=cat)
type ServiceInstance struct {
	Endpoint         NetworkEndpoint `json:"endpoint,omitempty"`
	Service          *Service        `json:"service,omitempty"`
	Labels           Labels          `json:"labels,omitempty"`
	AvailabilityZone string          `json:"az,omitempty"`
	ServiceAccount   string          `json:"serviceaccount,omitempty"`
}

// ServiceDiscovery enumerates Istio service instances.
type ServiceDiscovery interface {
	// Services list declarations of all services in the system
	Services() ([]*Service, error)

	// GetService retrieves a service by host name if it exists
	GetService(hostname string) (*Service, error)

	// Instances retrieves instances for a service and its ports that match
	// any of the supplied labels. All instances match an empty tag list.
	//
	// For example, consider the example of catalog.mystore.com as described in NetworkEndpoints
	// Instances(catalog.myservice.com, 80) ->
	//      --> NetworkEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//      --> NetworkEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//      --> NetworkEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Labels(kitty=cat)
	//      --> NetworkEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Labels(kitty=cat)
	//
	// Calling Instances with specific labels returns a trimmed list.
	// e.g., Instances(catalog.myservice.com, 80, foo=bar) ->
	//      --> NetworkEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//      --> NetworkEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//
	// Similar concepts apply for calling this function with a specific
	// port, hostname and labels.
	Instances(hostname string, ports []string, labels LabelsCollection) ([]*ServiceInstance, error)

	// HostInstances lists service instances for a given set of IPv4 addresses.
	HostInstances(addrs map[string]bool) ([]*ServiceInstance, error)

	// ManagementPorts lists set of management ports associated with an IPv4 address.
	// These management ports are typically used by the platform for out of band management
	// tasks such as health checks, etc. In a scenario where the proxy functions in the
	// transparent mode (traps all traffic to and from the service instance IP address),
	// the configuration generated for the proxy will not manipulate traffic destined for
	// the management ports
	ManagementPorts(addr string) PortList
}

// ServiceAccounts exposes Istio service accounts
type ServiceAccounts interface {
	// GetIstioServiceAccounts returns a list of service accounts looked up from
	// the specified service hostname and ports.
	GetIstioServiceAccounts(hostname string, ports []string) []string
}

// SubsetOf is true if the tag has identical values for the keys
func (t Labels) SubsetOf(that Labels) bool {
	for k, v := range t {
		if that[k] != v {
			return false
		}
	}
	return true
}

// Equals returns true if the labels are identical
func (t Labels) Equals(that Labels) bool {
	if t == nil {
		return that == nil
	}
	if that == nil {
		return t == nil
	}
	return t.SubsetOf(that) && that.SubsetOf(t)
}

// HasSubsetOf returns true if the input labels are a super set of one labels in a
// collection or if the tag collection is empty
func (labels LabelsCollection) HasSubsetOf(that Labels) bool {
	if len(labels) == 0 {
		return true
	}
	for _, tag := range labels {
		if tag.SubsetOf(that) {
			return true
		}
	}
	return false
}

// GetNames returns port names
func (ports PortList) GetNames() []string {
	names := make([]string, 0, len(ports))
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

// GetByPort retrieves a port declaration by port value
func (ports PortList) GetByPort(num int) (*Port, bool) {
	for _, port := range ports {
		if port.Port == num {
			return port, true
		}
	}
	return nil, false
}

// External predicate checks whether the service is external
func (s *Service) External() bool {
	return s.ExternalName != ""
}

// Key generates a unique string referencing service instances for a given port and labels.
// The separator character must be exclusive to the regular expressions allowed in the
// service declaration.
func (s *Service) Key(port *Port, tag Labels) string {
	// TODO: check port is non nil and membership of port in service
	return ServiceKey(s.Hostname, PortList{port}, LabelsCollection{tag})
}

// ServiceKey generates a service key for a collection of ports and labels
func ServiceKey(hostname string, servicePorts PortList, labelsList LabelsCollection) string {
	// example: name.namespace|http|env=prod;env=test,version=my-v1
	var buffer bytes.Buffer
	buffer.WriteString(hostname)
	np := len(servicePorts)
	nt := len(labelsList)

	if nt == 1 && labelsList[0] == nil {
		nt = 0
	}

	if np == 0 && nt == 0 {
		return buffer.String()
	} else if np == 1 && nt == 0 && servicePorts[0].Name == "" {
		return buffer.String()
	} else {
		buffer.WriteString("|")
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
		buffer.WriteString("|")
		labels := make([]string, nt)
		for i := 0; i < nt; i++ {
			labels[i] = labelsList[i].String()
		}
		sort.Strings(labels)
		for i := 0; i < nt; i++ {
			if i > 0 {
				buffer.WriteString(";")
			}
			buffer.WriteString(labels[i])
		}
	}
	return buffer.String()
}

// ParseServiceKey is the inverse of the Service.String() method
func ParseServiceKey(s string) (hostname string, ports PortList, labels LabelsCollection) {
	parts := strings.Split(s, "|")
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
			labels = append(labels, ParseLabelsString(tag))
		}
	}
	return
}

func (t Labels) String() string {
	labels := make([]string, 0, len(t))
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

// ParseLabelsString extracts labels from a string
func ParseLabelsString(s string) Labels {
	pairs := strings.Split(s, ",")
	tag := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) > 1 {
			tag[kv[0]] = kv[1]
		} else {
			tag[kv[0]] = ""
		}
	}
	return tag
}
