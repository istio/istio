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
	"sync"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/mitchellh/copystructure"

	authn "istio.io/api/authentication/v1alpha1"

	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
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
	// Attributes contains additional attributes associated with the service
	// used mostly by mixer and RBAC for policy enforcement purposes.
	Attributes ServiceAttributes

	// Ports is the set of network ports where the service is listening for
	// connections
	Ports PortList `json:"ports,omitempty"`

	// ServiceAccounts specifies the service accounts that run the service.
	ServiceAccounts []string `json:"serviceAccounts,omitempty"`

	// CreationTime records the time this service was created, if available.
	CreationTime time.Time `json:"creationTime,omitempty"`

	// Name of the service, e.g. "catalog.mystore.com"
	Hostname host.Name `json:"hostname"`

	// Address specifies the service IPv4 address of the load balancer
	Address string `json:"address,omitempty"`

	// ClusterVIPs specifies the service address of the load balancer
	// in each of the clusters where the service resides
	ClusterVIPs map[string]string `json:"cluster-vips,omitempty"`

	// Resolution indicates how the service instances need to be resolved before routing
	// traffic. Most services in the service registry will use static load balancing wherein
	// the proxy will decide the service instance that will receive the traffic. Service entries
	// could either use DNS load balancing (i.e. proxy will query DNS server for the IP of the service)
	// or use the passthrough model (i.e. proxy will forward the traffic to the network endpoint requested
	// by the caller)
	Resolution Resolution

	// Protect concurrent ClusterVIPs read/write
	Mutex sync.RWMutex

	// MeshExternal (if true) indicates that the service is external to the mesh.
	// These services are defined using Istio's ServiceEntry spec.
	MeshExternal bool
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

// String converts Resolution in to String.
func (resolution Resolution) String() string {
	switch resolution {
	case ClientSideLB:
		return "ClientSide"
	case DNSLB:
		return "DNS"
	case Passthrough:
		return "Passthrough"
	default:
		return fmt.Sprintf("%d", int(resolution))
	}
}

const (
	// IstioDefaultConfigNamespace constant for default namespace
	IstioDefaultConfigNamespace = "default"

	// LocalityLabel indicates the region/zone/subzone of an instance. It is used to override the native
	// registry's value.
	//
	// Note: because k8s labels does not support `/`, so we use `.` instead in k8s.
	LocalityLabel = "istio-locality"
	// k8s istio-locality label separator
	k8sSeparator = "."
)

const (
	// TLSModeLabelShortname name used for determining endpoint level tls transport socket configuration
	TLSModeLabelShortname = "tlsMode"

	// TLSModeLabelName is the name of label given to service instances to determine whether to use mTLS or
	// fallback to plaintext/tls
	TLSModeLabelName = "security.istio.io/" + TLSModeLabelShortname

	// DisabledTLSModeLabel implies that this endpoint should receive traffic as is (mostly plaintext)
	DisabledTLSModeLabel = "disabled"

	// IstioMutualTLSModeLabel implies that the endpoint is ready to receive Istio mTLS connections.
	IstioMutualTLSModeLabel = "istio"

	// IstioCanonicalServiceLabelName is the name of label for the Istio Canonical Service for a workload instance.
	IstioCanonicalServiceLabelName = "service.istio.io/canonical-name"

	// IstioCanonicalServiceRevisionLabelName is the name of label for the Istio Canonical Service revision for a workload instance.
	IstioCanonicalServiceRevisionLabelName = "service.istio.io/canonical-revision"
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
	// service.
	Port int `json:"port"`

	// Protocol to be used for the port.
	Protocol protocol.Instance `json:"protocol,omitempty"`
}

// PortList is a set of ports
type PortList []*Port

// AddressFamily indicates the kind of transport used to reach an IstioEndpoint
type AddressFamily int

const (
	// AddressFamilyTCP represents an address that connects to a TCP endpoint. It consists of an IP address or host and
	// a port number.
	AddressFamilyTCP AddressFamily = iota
	// AddressFamilyUnix represents an address that connects to a Unix Domain Socket. It consists of a socket file path.
	AddressFamilyUnix
)

// String converts addressfamily into string (tcp/unix)
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

	// trafficDirectionOutboundSrvPrefix the prefix for a DNS SRV type subset key
	trafficDirectionOutboundSrvPrefix = string(TrafficDirectionOutbound) + "_"
	// trafficDirectionInboundSrvPrefix the prefix for a DNS SRV type subset key
	trafficDirectionInboundSrvPrefix = string(TrafficDirectionInbound) + "_"
)

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
// Since a ServiceInstance has a single IstioEndpoint, which has a single port,
// multiple ServiceInstances are required to represent a workload that listens
// on multiple ports.
//
// The labels associated with a service instance are unique per a network endpoint.
// There is one well defined set of labels for each service instance network endpoint.
//
// For example, the set of service instances associated with catalog.mystore.com
// are modeled like this
//      --> IstioEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
//      --> IstioEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
//      --> IstioEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Labels(kitty=cat)
//      --> IstioEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Labels(kitty=cat)
type ServiceInstance struct {
	Service     *Service       `json:"service,omitempty"`
	ServicePort *Port          `json:"servicePort,omitempty"`
	Endpoint    *IstioEndpoint `json:"endpoint,omitempty"`
}

// GetLocality returns the availability zone from an instance. If service instance label for locality
// is set we use this. Otherwise, we use the one set by the registry:
//   - k8s: region/zone, extracted from node's failure-domain.beta.kubernetes.io/{region,zone}
// 	 - consul: defaults to 'instance.Datacenter'
//
// This is used by CDS/EDS to group the endpoints by locality.
func (instance *ServiceInstance) GetLocality() string {
	return GetLocalityOrDefault(instance.Endpoint.Labels[LocalityLabel], instance.Endpoint.Locality)
}

// DeepCopy creates a copy of ServiceInstance.
func (instance *ServiceInstance) DeepCopy() *ServiceInstance {
	return &ServiceInstance{
		Service:  instance.Service.DeepCopy(),
		Endpoint: instance.Endpoint.DeepCopy(),
		ServicePort: &Port{
			Name:     instance.ServicePort.Name,
			Port:     instance.ServicePort.Port,
			Protocol: instance.ServicePort.Protocol,
		},
	}
}

// GetLocalityOrDefault returns the locality from the supplied label, or falls back to
// the supplied default locality if the supplied label is empty. Because Kubernetes
// labels don't support `/`, we replace "." with "/" in the supplied label as a workaround.
func GetLocalityOrDefault(label, defaultLocality string) string {
	if len(label) > 0 {
		// if there are /'s present we don't need to replace
		if strings.Contains(label, "/") {
			return label
		}
		// replace "." with "/"
		return strings.Replace(label, k8sSeparator, "/", -1)
	}
	return defaultLocality
}

// IstioEndpoint defines a network address (IP:port) associated with an instance of the
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
//
// TODO: Investigate removing ServiceInstance entirely.
type IstioEndpoint struct {
	// Labels points to the workload or deployment labels.
	Labels labels.Instance

	// Family indicates what type of endpoint, such as TCP or Unix Domain Socket.
	// Default is TCP.
	Family AddressFamily

	// Address is the address of the endpoint, using envoy proto.
	Address string

	// ServicePortName tracks the name of the port, to avoid 'eventual consistency' issues.
	// Sometimes the Endpoint is visible before Service - so looking up the port number would
	// fail. Instead the mapping to number is made when the clusters are computed. The lazy
	// computation will also help with 'on-demand' and 'split horizon' - where it will be skipped
	// for not used clusters or endpoints behind a gate.
	ServicePortName string

	// UID identifies the workload, for telemetry purpose.
	UID string

	// EnvoyEndpoint is a cached LbEndpoint, converted from the data, to
	// avoid recomputation
	EnvoyEndpoint *endpoint.LbEndpoint

	// ServiceAccount holds the associated service account.
	ServiceAccount string

	// Network holds the network where this endpoint is present
	Network string

	// The locality where the endpoint is present. / separated string
	Locality string

	// EndpointPort is the port where the workload is listening, can be different
	// from the service port.
	EndpointPort uint32

	// The load balancing weight associated with this endpoint.
	LbWeight uint32

	// Attributes contains additional attributes associated with the service
	// used mostly by mixer and RBAC for policy enforcement purposes.
	Attributes ServiceAttributes

	// TLSMode endpoint is injected with istio sidecar and ready to configure Istio mTLS
	TLSMode string
}

// ServiceAttributes represents a group of custom attributes of the service.
type ServiceAttributes struct {
	// ServiceRegistry indicates the backing service registry system where this service
	// was sourced from.
	// TODO: move the ServiceRegistry type from platform.go to model
	ServiceRegistry string
	// Name is "destination.service.name" attribute
	Name string
	// Namespace is "destination.service.namespace" attribute
	Namespace string
	// UID is "destination.service.uid" attribute
	UID string
	// ExportTo defines the visibility of Service in
	// a namespace when the namespace is imported.
	ExportTo map[visibility.Instance]bool

	// For Kubernetes platform

	// ClusterExternalAddresses is a mapping between a cluster name and the external
	// address(es) to access the service from outside the cluster.
	// Used by the aggregator to aggregate the Attributes.ClusterExternalAddresses
	// for clusters where the service resides
	ClusterExternalAddresses map[string][]string
}

// ServiceDiscovery enumerates Istio service instances.
// nolint: lll
//go:generate counterfeiter -o ../networking/core/v1alpha3/fakes/fake_service_discovery.gen.go --fake-name ServiceDiscovery . ServiceDiscovery
type ServiceDiscovery interface {
	// Services list declarations of all services in the system
	Services() ([]*Service, error)

	// GetService retrieves a service by host name if it exists
	GetService(hostname host.Name) (*Service, error)

	// InstancesByPort retrieves instances for a service on the given ports with labels that match
	// any of the supplied labels. All instances match an empty tag list.
	//
	// For example, consider an example of catalog.mystore.com:
	// Instances(catalog.myservice.com, 80) ->
	//      --> IstioEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//      --> IstioEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//      --> IstioEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Labels(kitty=cat)
	//      --> IstioEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Labels(kitty=cat)
	//
	// Calling Instances with specific labels returns a trimmed list.
	// e.g., Instances(catalog.myservice.com, 80, foo=bar) ->
	//      --> IstioEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//      --> IstioEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
	//
	// Similar concepts apply for calling this function with a specific
	// port, hostname and labels.
	//
	// Introduced in Istio 0.8. It is only called with 1 port.
	// CDS (clusters.go) calls it for building 'dnslb' type clusters.
	// EDS calls it for building the endpoints result.
	// Consult istio-dev before using this for anything else (except debugging/tools)
	InstancesByPort(svc *Service, servicePort int, labels labels.Collection) ([]*ServiceInstance, error)

	// GetProxyServiceInstances returns the service instances that co-located with a given Proxy
	//
	// Co-located generally means running in the same network namespace and security context.
	//
	// A Proxy operating as a Sidecar will return a non-empty slice.  A stand-alone Proxy
	// will return an empty slice.
	//
	// There are two reasons why this returns multiple ServiceInstances instead of one:
	// - A ServiceInstance has a single IstioEndpoint which has a single Port.  But a Service
	//   may have many ports.  So a workload implementing such a Service would need
	//   multiple ServiceInstances, one for each port.
	// - A single workload may implement multiple logical Services.
	//
	// In the second case, multiple services may be implemented by the same physical port number,
	// though with a different ServicePort and IstioEndpoint for each.  If any of these overlapping
	// services are not HTTP or H2-based, behavior is undefined, since the listener may not be able to
	// determine the intended destination of a connection without a Host header on the request.
	GetProxyServiceInstances(*Proxy) ([]*ServiceInstance, error)

	GetProxyWorkloadLabels(*Proxy) (labels.Collection, error)

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

	// GetIstioServiceAccounts returns a list of service accounts looked up from
	// the specified service hostname and ports.
	// Deprecated - service account tracking moved to XdsServer, incremental.
	GetIstioServiceAccounts(svc *Service, ports []int) []string
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
		if port.Port == num && port.Protocol != protocol.UDP {
			return port, true
		}
	}
	return nil, false
}

// External predicate checks whether the service is external
func (s *Service) External() bool {
	return s.MeshExternal
}

// Key generates a unique string referencing service instances for a given port and labels.
// The separator character must be exclusive to the regular expressions allowed in the
// service declaration.
// Deprecated
func (s *Service) Key(port *Port, l labels.Instance) string {
	// TODO: check port is non nil and membership of port in service
	return ServiceKey(s.Hostname, PortList{port}, labels.Collection{l})
}

// ServiceKey generates a service key for a collection of ports and labels
// Deprecated
//
// Interface wants to turn `Name` into `fmt.Stringer`, completely defeating the purpose of the type alias.
// nolint: interfacer
func ServiceKey(hostname host.Name, servicePorts PortList, labelsList labels.Collection) string {
	// example: name.namespace|http|env=prod;env=test,version=my-v1
	var buffer bytes.Buffer
	buffer.WriteString(string(hostname))
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
		l := make([]string, nt)
		for i := 0; i < nt; i++ {
			l[i] = labelsList[i].String()
		}
		sort.Strings(l)
		for i := 0; i < nt; i++ {
			if i > 0 {
				buffer.WriteString(";")
			}
			buffer.WriteString(l[i])
		}
	}
	return buffer.String()
}

// ParseServiceKey is the inverse of the Service.String() method
// Deprecated
func ParseServiceKey(s string) (hostname host.Name, ports PortList, lc labels.Collection) {
	parts := strings.Split(s, "|")
	hostname = host.Name(parts[0])

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
			lc = append(lc, labels.Parse(tag))
		}
	}
	return
}

// BuildSubsetKey generates a unique string referencing service instances for a given service name, a subset and a port.
// The proxy queries Pilot with this key to obtain the list of instances in a subset.
func BuildSubsetKey(direction TrafficDirection, subsetName string, hostname host.Name, port int) string {
	return string(direction) + "|" + strconv.Itoa(port) + "|" + subsetName + "|" + string(hostname)
}

// BuildDNSSrvSubsetKey generates a unique string referencing service instances for a given service name, a subset and a port.
// The proxy queries Pilot with this key to obtain the list of instances in a subset.
// This is used only for the SNI-DNAT router. Do not use for other purposes.
// The DNS Srv format of the cluster is also used as the default SNI string for Istio mTLS connections
func BuildDNSSrvSubsetKey(direction TrafficDirection, subsetName string, hostname host.Name, port int) string {
	return string(direction) + "_." + strconv.Itoa(port) + "_." + subsetName + "_." + string(hostname)
}

// IsValidSubsetKey checks if a string is valid for subset key parsing.
func IsValidSubsetKey(s string) bool {
	return strings.Count(s, "|") == 3
}

// ParseSubsetKey is the inverse of the BuildSubsetKey method
func ParseSubsetKey(s string) (direction TrafficDirection, subsetName string, hostname host.Name, port int) {
	var parts []string
	dnsSrvMode := false
	// This could be the DNS srv form of the cluster that uses outbound_.port_.subset_.hostname
	// Since we do not want every callsite to implement the logic to differentiate between the two forms
	// we add an alternate parser here.
	if strings.HasPrefix(s, trafficDirectionOutboundSrvPrefix) ||
		strings.HasPrefix(s, trafficDirectionInboundSrvPrefix) {
		parts = strings.SplitN(s, ".", 4)
		dnsSrvMode = true
	} else {
		parts = strings.Split(s, "|")
	}

	if len(parts) < 4 {
		return
	}

	direction = TrafficDirection(strings.TrimSuffix(parts[0], "_"))
	port, _ = strconv.Atoi(strings.TrimSuffix(parts[1], "_"))
	subsetName = parts[2]

	if dnsSrvMode {
		subsetName = strings.TrimSuffix(parts[2], "_")
	}

	hostname = host.Name(parts[3])
	return
}

// GetServiceAddressForProxy returns a Service's IP address specific to the cluster where the node resides
func (s *Service) GetServiceAddressForProxy(node *Proxy) string {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()
	if node.ClusterID != "" && s.ClusterVIPs[node.ClusterID] != "" {
		return s.ClusterVIPs[node.ClusterID]
	}
	return s.Address
}

// GetTLSModeFromEndpointLabels returns the value of the label
// security.istio.io/tlsMode if set. Do not return Enums or constants
// from this function as users could provide values other than istio/disabled
// and apply custom transport socket matchers here.
func GetTLSModeFromEndpointLabels(labels map[string]string) string {
	if labels != nil {
		if val, exists := labels[TLSModeLabelName]; exists {
			return val
		}
	}
	return DisabledTLSModeLabel
}

// DeepCopy creates a clone of Service.
// TODO : See if there is any efficient alternative to this function - copystructure can not be used as is because
// Service has sync.RWMutex that can not be copied.
func (s *Service) DeepCopy() *Service {
	attrs := copyInternal(s.Attributes)
	ports := copyInternal(s.Ports)
	accounts := copyInternal(s.ServiceAccounts)
	clusterVIPs := copyInternal(s.ClusterVIPs)

	return &Service{
		Attributes:      attrs.(ServiceAttributes),
		Ports:           ports.(PortList),
		ServiceAccounts: accounts.([]string),
		CreationTime:    s.CreationTime,
		Hostname:        s.Hostname,
		Address:         s.Address,
		ClusterVIPs:     clusterVIPs.(map[string]string),
		Resolution:      s.Resolution,
		MeshExternal:    s.MeshExternal,
	}
}

// DeepCopy creates a clone of IstioEndpoint.
func (ep *IstioEndpoint) DeepCopy() *IstioEndpoint {
	return copyInternal(ep).(*IstioEndpoint)
}

func copyInternal(v interface{}) interface{} {
	copied, err := copystructure.Copy(v)
	if err != nil {
		// There are 2 locations where errors are generated in copystructure.Copy:
		//  * The reflection walk over the structure fails, which should never happen
		//  * A configurable copy function returns an error. This is only used for copying times, which never returns an error.
		// Therefore, this should never happen
		panic(err)
	}
	return copied
}
