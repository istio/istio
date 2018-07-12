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
	"strconv"
	"strings"

	"time"

	authn "istio.io/api/authentication/v1alpha1"
)

// Hostname describes a (possibly wildcarded) hostname
type Hostname string

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
	Hostname Hostname `json:"hostname"`

	// Address specifies the service IPv4 address of the load balancer
	Address string `json:"address,omitempty"`

	// ClusterVIPs specifies the service address of the load balancer
	// in each of the clusters where the service resides
	ClusterVIPs map[string]string `json:"cluster-vips,omitempty"`

	// Ports is the set of network ports where the service is listening for
	// connections
	Ports PortList `json:"ports,omitempty"`

	// ExternalName is only set for external services and holds the external
	// service DNS name.  External services are name-based solution to represent
	// external service instances as a service inside the cluster.
	// Deprecated : made obsolete by the MeshExternal and Resolution flags.
	ExternalName Hostname `json:"external"`

	// ServiceAccounts specifies the service accounts that run the service.
	ServiceAccounts []string `json:"serviceaccounts,omitempty"`

	// MeshExternal (if true) indicates that the service is external to the mesh.
	// These services are defined using Istio's ServiceEntry spec.
	MeshExternal bool

	// LoadBalancingDisabled indicates that no load balancing should be done for this service.
	// Deprecated : made obsolete by the MeshExternal and Resolution flags.
	LoadBalancingDisabled bool `json:"-"`

	// Resolution indicates how the service instances need to be resolved before routing
	// traffic. Most services in the service registry will use static load balancing wherein
	// the proxy will decide the service instance that will receive the traffic. Service entries
	// could either use DNS load balancing (i.e. proxy will query DNS server for the IP of the service)
	// or use the passthrough model (i.e. proxy will forward the traffic to the network endpoint requested
	// by the caller)
	Resolution Resolution

	// CreationTime records the time this service was created, if available.
	CreationTime time.Time `json:"creationTime,omitempty"`
}

// Resolution indicates how the service instances need to be resolved before routing
// traffic.
type Resolution int

const (
	// ClientSideLB implies that the proxy will decide the endpoint from its local lb pool
	ClientSideLB Resolution = iota
	// DNSLB implies that the proxy will resolve a DNS address and forward to the resolved address
	DNSLB
	// Passthrough implies that the proxy should forward traffic to the destination IP requested by the caller
	Passthrough
)

const (
	// UnspecifiedIP constant for empty IP address
	UnspecifiedIP = "0.0.0.0"

	// IstioDefaultConfigNamespace constant for default namespace
	IstioDefaultConfigNamespace = "default"
)

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

const (
	// ProtocolGRPC declares that the port carries gRPC traffic
	ProtocolGRPC Protocol = "GRPC"
	// ProtocolHTTP declares that the port carries HTTP/1.1 traffic.
	// Note that HTTP/1.0 or earlier may not be supported by the proxy.
	ProtocolHTTP Protocol = "HTTP"
	// ProtocolHTTP2 declares that the port carries HTTP/2 traffic
	ProtocolHTTP2 Protocol = "HTTP2"
	// ProtocolHTTPS declares that the port carries HTTPS traffic
	ProtocolHTTPS Protocol = "HTTPS"
	// ProtocolTCP declares the the port uses TCP.
	// This is the default protocol for a service port.
	ProtocolTCP Protocol = "TCP"
	// ProtocolTLS declares that the port carries TLS traffic.
	// TLS traffic is assumed to contain SNI as part of the handshake.
	ProtocolTLS Protocol = "TLS"
	// ProtocolUDP declares that the port uses UDP.
	// Note that UDP protocol is not currently supported by the proxy.
	ProtocolUDP Protocol = "UDP"
	// ProtocolMongo declares that the port carries mongoDB traffic
	ProtocolMongo Protocol = "Mongo"
	// ProtocolRedis declares that the port carries redis traffic
	ProtocolRedis Protocol = "Redis"
	// ProtocolUnsupported - value to signify that the protocol is unsupported
	ProtocolUnsupported Protocol = "UnsupportedProtocol"
)

// AddressFamily indicates the kind of transport used to reach a NetworkEndpoint
type AddressFamily int

const (
	// AddressFamilyTCP represents an address that connects to a TCP endpoint. It consists of an IP address or host and
	// a port number.
	AddressFamilyTCP AddressFamily = iota
	// AddressFamilyUnix represents an address that connects to a Unix Domain Socket. It consists of a socket file path.
	AddressFamilyUnix
)

func (f AddressFamily) String() string {
	switch f {
	case AddressFamilyTCP:
		return "tcp"
	case AddressFamilyUnix:
		return "unix"
	default:
		return fmt.Sprintf("%d", f)
	}
}

// TrafficDirection defines whether traffic exists a service instance or enters a service instance
type TrafficDirection string

const (
	// TrafficDirectionInbound indicates inbound traffic
	TrafficDirectionInbound TrafficDirection = "inbound"
	// TrafficDirectionOutbound indicates outbound traffic
	TrafficDirectionOutbound TrafficDirection = "outbound"
)

// ParseProtocol from string ignoring case
func ParseProtocol(s string) Protocol {
	switch strings.ToLower(s) {
	case "tcp":
		return ProtocolTCP
	case "udp":
		return ProtocolUDP
	case "grpc":
		return ProtocolGRPC
	case "http":
		return ProtocolHTTP
	case "http2":
		return ProtocolHTTP2
	case "https":
		return ProtocolHTTPS
	case "tls":
		return ProtocolTLS
	case "mongo":
		return ProtocolMongo
	case "redis":
		return ProtocolRedis
	}

	return ProtocolUnsupported
}

// IsHTTP2 is true for protocols that use HTTP/2 as transport protocol
func (p Protocol) IsHTTP2() bool {
	switch p {
	case ProtocolHTTP2, ProtocolGRPC:
		return true
	default:
		return false
	}
}

// IsHTTP is true for protocols that use HTTP as transport protocol
func (p Protocol) IsHTTP() bool {
	switch p {
	case ProtocolHTTP, ProtocolHTTP2, ProtocolGRPC:
		return true
	default:
		return false
	}
}

// IsTCP is true for protocols that use TCP as transport protocol
func (p Protocol) IsTCP() bool {
	switch p {
	case ProtocolTCP, ProtocolHTTPS, ProtocolTLS, ProtocolMongo, ProtocolRedis:
		return true
	default:
		return false
	}
}

// IsTLS is true for protocols on top of TLS (e.g. HTTPS)
func (p Protocol) IsTLS() bool {
	switch p {
	case ProtocolHTTPS, ProtocolTLS:
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
	// Family indicates what type of endpoint, such as TCP or Unix Domain Socket.
	Family AddressFamily

	// Address of the network endpoint. If Family is `AddressFamilyTCP`, it is
	// typically an IPv4 address. If Family is `AddressFamilyUnix`, it is the
	// path to the domain socket.
	Address string

	// Port number where this instance is listening for connections This
	// need not be the same as the port where the service is accessed.
	// e.g., catalog.mystore.com:8080 -> 172.16.0.1:55446
	// Ignored for `AddressFamilyUnix`.
	Port int

	// Port declaration from the service declaration This is the port for
	// the service associated with this instance (e.g.,
	// catalog.mystore.com)
	ServicePort *Port

	// Defines a platform-specific workload instance identifier (optional).
	UID string
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

// Probe represents a health probe associated with an instance of service.
type Probe struct {
	Port *Port  `json:"port,omitempty"`
	Path string `json:"path,omitempty"`
}

// ProbeList is a set of probes
type ProbeList []*Probe

// ServiceInstance represents an individual instance of a specific version
// of a service. It binds a network endpoint (ip:port), the service
// description (which is oblivious to various versions) and a set of labels
// that describe the service version associated with this instance.
//
// Since a ServiceInstance has a single NetworkEndpoint, which has a single port,
// multiple ServiceInstances are required to represent a workload that listens
// on multiple ports.
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

const (
	// AZLabel indicates the region/zone of an instance. It is used if the native
	// registry doesn't provide one.
	AZLabel = "istio-az"
)

// GetAZ returns the availability zone from an instance.
// - k8s: region/zone, extracted from node's failure-domain.beta.kubernetes.io/{region,zone}
// - consul: defaults to 'instance.Datacenter'
//
// This is used by EDS to group the endpoints by AZ and by .
// TODO: remove me?
func (si *ServiceInstance) GetAZ() string {
	if si.AvailabilityZone != "" {
		return si.AvailabilityZone
	}
	return si.Labels[AZLabel]
}

// ServiceAttributes represents a group of custom attributes of the service.
type ServiceAttributes struct {
	// Name is "destination.service.name" attribute
	Name string
	// Namespace is "destination.service.namespace" attribute
	Namespace string
	// UID is "destination.service.uid" attribute
	UID string
}

// ServiceDiscovery enumerates Istio service instances.
type ServiceDiscovery interface {
	// Services list declarations of all services in the system
	Services() ([]*Service, error)

	// GetService retrieves a service by host name if it exists
	GetService(hostname Hostname) (*Service, error)

	// GetServiceAttributes retrieves the custom attributes of a service
	GetServiceAttributes(hostname Hostname) (*ServiceAttributes, error)

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
	// Deprecated: made obsolete by InstancesByPort
	Instances(hostname Hostname, ports []string, labels LabelsCollection) ([]*ServiceInstance, error)

	// InstancesByPort retrieves instances for a service on the given ports with labels that match
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
	//
	// Introduced in Istio 0.8. It is only called with 1 port.
	// CDS (clusters.go) calls it for building 'dnslb' type clusters.
	// EDS calls it for building the endpoints result.
	// Consult istio-dev before using this for anything else (except debugging/tools)
	InstancesByPort(hostname Hostname, servicePort int, labels LabelsCollection) ([]*ServiceInstance, error)

	// GetProxyServiceInstances returns the service instances that co-located with a given Proxy
	//
	// Co-located generally means running in the same network namespace and security context.
	//
	// A Proxy operating as a Sidecar will return a non-empty slice.  A stand-alone Proxy
	// will return an empty slice.
	//
	// There are two reasons why this returns multiple ServiceInstances instead of one:
	// - A ServiceInstance has a single NetworkEndpoint which has a single Port.  But a Service
	//   may have many ports.  So a workload implementing such a Service would need
	//   multiple ServiceInstances, one for each port.
	// - A single workload may implement multiple logical Services.
	//
	// In the second case, multiple services may be implemented by the same physical port number,
	// though with a different ServicePort and NetworkEndpoint for each.  If any of these overlapping
	// services are not HTTP or H2-based, behavior is undefined, since the listener may not be able to
	// determine the intended destination of a connection without a Host header on the request.
	GetProxyServiceInstances(*Proxy) ([]*ServiceInstance, error)

	// ManagementPorts lists set of management ports associated with an IPv4 address.
	// These management ports are typically used by the platform for out of band management
	// tasks such as health checks, etc. In a scenario where the proxy functions in the
	// transparent mode (traps all traffic to and from the service instance IP address),
	// the configuration generated for the proxy will not manipulate traffic destined for
	// the management ports
	ManagementPorts(addr string) PortList

	// WorkloadHealthCheckInfo lists set of probes associated with an IPv4 address.
	// These probes are used by the platform to identify requests that are performing
	// health checks.
	WorkloadHealthCheckInfo(addr string) ProbeList
}

// ServiceAccounts exposes Istio service accounts
type ServiceAccounts interface {
	// GetIstioServiceAccounts returns a list of service accounts looked up from
	// the specified service hostname and ports.
	GetIstioServiceAccounts(hostname Hostname, ports []string) []string
}

// String returns Hostname as a string; Hostname is already an alias of a string, so this is really for convenience over
// explicit casting.
func (h Hostname) String() string {
	return string(h)
}

// Matches returns true if this Hostname "matches" the other hostname. Hostnames match if:
// - they're fully resolved (i.e. not wildcarded) and match exactly (i.e. an exact string match)
// - one or both are wildcarded (e.g. "*.foo.com"), in which case we use wildcard resolution rules
// to determine if h is covered by o.
// e.g.:
//  Hostname("foo.com").Matches("foo.com")   = true
//  Hostname("foo.com").Matches("bar.com")   = false
//  Hostname("*.com").Matches("foo.com")     = true
//  Hostname("*.com").Matches("foo.com")     = true
//  Hostname("*.foo.com").Matches("foo.com") = false
func (h Hostname) Matches(o Hostname) bool {
	if !strings.Contains(string(h), "*") && !strings.Contains(string(o), "*") {
		return h == o
	}
	la, sa := strings.Contains(string(h), "*"), strings.Contains(string(o), "*")
	longer, shorter := strings.TrimLeft(string(h), "*"), strings.TrimLeft(string(o), "*")
	if len(longer) < len(shorter) {
		longer, shorter = shorter, longer
		la, sa = sa, la
	}

	matches := strings.HasSuffix(longer, shorter)
	if matches && la && !sa && strings.TrimSuffix(longer, shorter) == "." {
		// we match, but the longer is a wildcard and the shorter is not; we need to ensure we don't match input
		// like `*.foo.com` to `foo.com` in that case (to avoid matching a domain literal to a wildcard subdomain)
		return false
	}
	return matches
}

// Hostnames is a collection of Hostname; it exists so it's easy to sort hostnames consistently across Pilot.
// In a few locations we care about the order hostnames appear in Envoy config: primarily HTTP routes, but also in
// gateways, and for SNI. In those locations, we sort hostnames longest to shortest with wildcards last.
type Hostnames []Hostname

// prove we implement the interface at compile time
var _ sort.Interface = Hostnames{}

func (h Hostnames) Len() int {
	return len(h)
}

func (h Hostnames) Less(i, j int) bool {
	a, b := h[i], h[j]
	if len(a) == 0 && len(b) == 0 {
		return true // doesn't matter, they're both the empty string
	}
	// we sort longest to shortest, alphabetically, with wildcards last
	ai, aj := strings.Contains(a.String(), "*"), strings.Contains(b.String(), "*")
	if ai && !aj {
		// h[i] is a wildcard, but h[j] isn't; therefore h[j] < h[i]
		return false
	} else if !ai && aj {
		// h[j] is a wildcard, but h[i] isn't; therefore h[i] < h[j]
		return true
	}
	// they're either both wildcards, or both not; in either case we sort them longest to shortest, alphabetically
	if len(a) == len(b) {
		return a < b
	}
	return len(a) > len(b)
}

func (h Hostnames) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// SubsetOf is true if the label has identical values for the keys
func (l Labels) SubsetOf(that Labels) bool {
	for k, v := range l {
		if that[k] != v {
			return false
		}
	}
	return true
}

// Equals returns true if the labels are identical
func (l Labels) Equals(that Labels) bool {
	if l == nil {
		return that == nil
	}
	if that == nil {
		return l == nil
	}
	return l.SubsetOf(that) && that.SubsetOf(l)
}

// HasSubsetOf returns true if the input labels are a super set of one labels in a
// collection or if the tag collection is empty
func (labels LabelsCollection) HasSubsetOf(that Labels) bool {
	if len(labels) == 0 {
		return true
	}
	for _, label := range labels {
		if label.SubsetOf(that) {
			return true
		}
	}
	return false
}

// IsSupersetOf returns true if the input labels are a subset set of any set of labels in a
// collection
func (labels LabelsCollection) IsSupersetOf(that Labels) bool {

	if len(labels) == 0 {
		return len(that) == 0
	}

	for _, label := range labels {
		if that.SubsetOf(label) {
			return true
		}
	}
	return false
}

// Match returns true if port matches with authentication port selector criteria.
func (port Port) Match(portSelector *authn.PortSelector) bool {
	if portSelector == nil {
		return true
	}
	switch portSelector.Port.(type) {
	case *authn.PortSelector_Name:
		return portSelector.GetName() == port.Name
	case *authn.PortSelector_Number:
		return portSelector.GetNumber() == uint32(port.Port)
	default:
		return false
	}
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
// Deprecated
func (s *Service) Key(port *Port, labels Labels) string {
	// TODO: check port is non nil and membership of port in service
	return ServiceKey(s.Hostname, PortList{port}, LabelsCollection{labels})
}

// ServiceKey generates a service key for a collection of ports and labels
// Deprecated
//
// Interface wants to turn `Hostname` into `fmt.Stringer`, completely defeating the purpose of the type alias.
// nolint: interfacer
func ServiceKey(hostname Hostname, servicePorts PortList, labelsList LabelsCollection) string {
	// example: name.namespace|http|env=prod;env=test,version=my-v1
	var buffer bytes.Buffer
	buffer.WriteString(hostname.String())
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
// Deprecated
func ParseServiceKey(s string) (hostname Hostname, ports PortList, labels LabelsCollection) {
	parts := strings.Split(s, "|")
	hostname = Hostname(parts[0])

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

// BuildSubsetKey generates a unique string referencing service instances for a given service name, a subset and a port.
// The proxy queries Pilot with this key to obtain the list of instances in a subset.
func BuildSubsetKey(direction TrafficDirection, subsetName string, hostname Hostname, port int) string {
	return fmt.Sprintf("%s|%d|%s|%s", direction, port, subsetName, hostname)
}

// IsValidSubsetKey checks if a string is valid for subset key parsing.
func IsValidSubsetKey(s string) bool {
	return strings.Count(s, "|") == 3
}

// ParseSubsetKey is the inverse of the BuildSubsetKey method
func ParseSubsetKey(s string) (direction TrafficDirection, subsetName string, hostname Hostname, port int) {
	parts := strings.Split(s, "|")
	direction = TrafficDirection(parts[0])
	port, _ = strconv.Atoi(parts[1])
	subsetName = parts[2]
	hostname = Hostname(parts[3])
	return
}

func (l Labels) String() string {
	labels := make([]string, 0, len(l))
	for k, v := range l {
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

// GetServiceAddressForProxy returns a Service's IP address specific to the cluster where the node resides
func (s Service) GetServiceAddressForProxy(node *Proxy) string {
	if node.ClusterID != "" && s.ClusterVIPs[node.ClusterID] != "" {
		return s.ClusterVIPs[node.ClusterID]
	}
	return s.Address
}
