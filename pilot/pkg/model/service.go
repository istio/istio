// Copyright Istio Authors
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
	"fmt"
	"strconv"
	"strings"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/mitchellh/copystructure"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/network"
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
	// used mostly by RBAC for policy enforcement purposes.
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

	// ClusterVIPs specifies the service address of the load balancer
	// in each of the clusters where the service resides
	ClusterVIPs AddressMap `json:"clusterVIPs,omitempty"`

	// DefaultAddress specifies the default service IP of the load balancer.
	// Do not access directly. Use GetAddressForProxy
	DefaultAddress string `json:"defaultAddress,omitempty"`

	// AutoAllocatedAddress specifies the automatically allocated
	// IPv4 address out of the reserved Class E subnet
	// (240.240.0.0/16) for service entries with non-wildcard
	// hostnames. The IPs assigned to services are not
	// synchronized across istiod replicas as the DNS resolution
	// for these service entries happens completely inside a pod
	// whose proxy is managed by one istiod. That said, the algorithm
	// to allocate IPs is pretty deterministic that at stable state, two
	// istiods will allocate the exact same set of IPs for a given set of
	// service entries.
	AutoAllocatedAddress string `json:"autoAllocatedAddress,omitempty"`

	// Resolution indicates how the service instances need to be resolved before routing
	// traffic. Most services in the service registry will use static load balancing wherein
	// the proxy will decide the service instance that will receive the traffic. Service entries
	// could either use DNS load balancing (i.e. proxy will query DNS server for the IP of the service)
	// or use the passthrough model (i.e. proxy will forward the traffic to the network endpoint requested
	// by the caller)
	Resolution Resolution

	// MeshExternal (if true) indicates that the service is external to the mesh.
	// These services are defined using Istio's ServiceEntry spec.
	MeshExternal bool

	// ResourceVersion represents the internal version of this object.
	ResourceVersion string
}

func (s *Service) Key() string {
	if s == nil {
		return ""
	}

	return s.Attributes.Namespace + "/" + string(s.Hostname)
}

// Resolution indicates how the service instances need to be resolved before routing traffic.
type Resolution int

const (
	// ClientSideLB implies that the proxy will decide the endpoint from its local lb pool
	ClientSideLB Resolution = iota
	// DNSLB implies that the proxy will resolve a DNS address and forward to the resolved address
	DNSLB
	// Passthrough implies that the proxy should forward traffic to the destination IP requested by the caller
	Passthrough
	// DNSRoundRobinLB implies that the proxy will resolve a DNS address and forward to the resolved address
	DNSRoundRobinLB
)

// String converts Resolution in to String.
func (resolution Resolution) String() string {
	switch resolution {
	case ClientSideLB:
		return "ClientSide"
	case DNSLB:
		return "DNS"
	case DNSRoundRobinLB:
		return "DNSRoundRobin"
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

type workloadKind int

const (
	// PodKind indicates the workload is from pod
	PodKind workloadKind = iota
	// WorkloadEntryKind indicates the workload is from workloadentry
	WorkloadEntryKind
)

func (k workloadKind) String() string {
	if k == PodKind {
		return "Pod"
	}

	if k == WorkloadEntryKind {
		return "WorkloadEntry"
	}
	return ""
}

type WorkloadInstance struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	// Where the workloadInstance come from, valid values are`Pod` or `WorkloadEntry`
	Kind     workloadKind      `json:"kind"`
	Endpoint *IstioEndpoint    `json:"endpoint,omitempty"`
	PortMap  map[string]uint32 `json:"portMap,omitempty"`
	// Can only be selected by service entry of DNS type.
	DNSServiceEntryOnly bool `json:"dnsServiceEntryOnly,omitempty"`
}

// DeepCopy creates a copy of WorkloadInstance.
func (instance *WorkloadInstance) DeepCopy() *WorkloadInstance {
	pmap := map[string]uint32{}
	for k, v := range instance.PortMap {
		pmap[k] = v
	}
	return &WorkloadInstance{
		Name:      instance.Name,
		Namespace: instance.Namespace,
		Kind:      instance.Kind,
		PortMap:   pmap,
		Endpoint:  instance.Endpoint.DeepCopy(),
	}
}

// WorkloadInstancesEqual is a custom comparison of workload instances based on the fields that we need
// i.e. excluding the ports. Returns true if equal, false otherwise.
func WorkloadInstancesEqual(first, second *WorkloadInstance) bool {
	if first.Endpoint == nil || second.Endpoint == nil {
		return first.Endpoint == second.Endpoint
	}
	if first.Endpoint.Address != second.Endpoint.Address {
		return false
	}
	if first.Endpoint.Network != second.Endpoint.Network {
		return false
	}
	if first.Endpoint.TLSMode != second.Endpoint.TLSMode {
		return false
	}
	if !first.Endpoint.Labels.Equals(second.Endpoint.Labels) {
		return false
	}
	if first.Endpoint.ServiceAccount != second.Endpoint.ServiceAccount {
		return false
	}
	if first.Endpoint.Locality != second.Endpoint.Locality {
		return false
	}
	if first.Endpoint.GetLoadBalancingWeight() != second.Endpoint.GetLoadBalancingWeight() {
		return false
	}
	if first.Namespace != second.Namespace {
		return false
	}
	if first.Name != second.Name {
		return false
	}
	if first.Kind != second.Kind {
		return false
	}
	if !portMapEquals(first.PortMap, second.PortMap) {
		return false
	}
	return true
}

func portMapEquals(a, b map[string]uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// GetLocalityLabelOrDefault returns the locality from the supplied label, or falls back to
// the supplied default locality if the supplied label is empty. Because Kubernetes
// labels don't support `/`, we replace "." with "/" in the supplied label as a workaround.
func GetLocalityLabelOrDefault(label, defaultLabel string) string {
	if len(label) > 0 {
		// if there are /'s present we don't need to replace
		if strings.Contains(label, "/") {
			return label
		}
		// replace "." with "/"
		return strings.Replace(label, k8sSeparator, "/", -1)
	}
	return defaultLabel
}

// SplitLocalityLabel splits a locality label into region, zone and subzone strings.
func SplitLocalityLabel(locality string) (region, zone, subzone string) {
	items := strings.Split(locality, "/")
	switch len(items) {
	case 1:
		return items[0], "", ""
	case 2:
		return items[0], items[1], ""
	default:
		return items[0], items[1], items[2]
	}
}

// Locality information for an IstioEndpoint
type Locality struct {
	// Label for locality on the endpoint. This is a "/" separated string.
	Label string

	// ClusterID where the endpoint is located
	ClusterID cluster.ID
}

// Endpoint health status.
type HealthStatus int32

const (
	// Healthy.
	Healthy HealthStatus = 0
	// Unhealthy.
	UnHealthy HealthStatus = 1
)

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
// then internally, we have two endpoint structs for the
// service catalog.mystore.com
//  --> 172.16.0.1:55446 (with ServicePort pointing to 80) and
//  --> 172.16.0.1:33333 (with ServicePort pointing to 8080)
//
// TODO: Investigate removing ServiceInstance entirely.
type IstioEndpoint struct {
	// Labels points to the workload or deployment labels.
	Labels labels.Instance

	// Address is the address of the endpoint, using envoy proto.
	Address string

	// ServicePortName tracks the name of the port, this is used to select the IstioEndpoint by service port.
	ServicePortName string

	// EnvoyEndpoint is a cached LbEndpoint, converted from the data, to
	// avoid recomputation
	EnvoyEndpoint *endpoint.LbEndpoint

	// ServiceAccount holds the associated service account.
	ServiceAccount string

	// Network holds the network where this endpoint is present
	Network network.ID

	// The locality where the endpoint is present.
	Locality Locality

	// EndpointPort is the port where the workload is listening, can be different
	// from the service port.
	EndpointPort uint32

	// The load balancing weight associated with this endpoint.
	LbWeight uint32

	// TLSMode endpoint is injected with istio sidecar and ready to configure Istio mTLS
	TLSMode string

	// Namespace that this endpoint belongs to. This is for telemetry purpose.
	Namespace string

	// Name of the workload that this endpoint belongs to. This is for telemetry purpose.
	WorkloadName string

	// Specifies the hostname of the Pod, empty for vm workload.
	HostName string

	// If specified, the fully qualified Pod hostname will be "<hostname>.<subdomain>.<pod namespace>.svc.<cluster domain>".
	SubDomain string

	// The ingress tunnel supportability of this endpoint.
	// If this endpoint sidecar proxy does not support h2 tunnel, this endpoint will not show up in the EDS clusters
	// which are generated for h2 tunnel.
	TunnelAbility networking.TunnelAbility

	// Determines the discoverability of this endpoint throughout the mesh.
	DiscoverabilityPolicy EndpointDiscoverabilityPolicy `json:"-"`

	// Indicatesthe endpoint health status.
	HealthStatus HealthStatus
}

// GetLoadBalancingWeight returns the weight for this endpoint, normalized to always be > 0.
func (ep *IstioEndpoint) GetLoadBalancingWeight() uint32 {
	if ep.LbWeight > 0 {
		return ep.LbWeight
	}
	return 1
}

// IsDiscoverableFromProxy indicates whether this endpoint is discoverable from the given Proxy.
func (ep *IstioEndpoint) IsDiscoverableFromProxy(p *Proxy) bool {
	if ep == nil || ep.DiscoverabilityPolicy == nil {
		// If no policy was assigned, default to discoverable mesh-wide.
		// TODO(nmittler): Will need to re-think this default when cluster.local is actually cluster-local.
		return true
	}
	return ep.DiscoverabilityPolicy.IsDiscoverableFromProxy(ep, p)
}

// EndpointDiscoverabilityPolicy determines the discoverability of an endpoint throughout the mesh.
type EndpointDiscoverabilityPolicy interface {
	// IsDiscoverableFromProxy indicates whether an endpoint is discoverable from the given Proxy.
	IsDiscoverableFromProxy(*IstioEndpoint, *Proxy) bool

	// String returns name of this policy.
	String() string
}

type endpointDiscoverabilityPolicyImpl struct {
	name string
	f    func(*IstioEndpoint, *Proxy) bool
}

func (p *endpointDiscoverabilityPolicyImpl) IsDiscoverableFromProxy(ep *IstioEndpoint, proxy *Proxy) bool {
	return p.f(ep, proxy)
}

func (p *endpointDiscoverabilityPolicyImpl) String() string {
	return p.name
}

// AlwaysDiscoverable is an EndpointDiscoverabilityPolicy that allows an endpoint to be discoverable throughout the mesh.
var AlwaysDiscoverable EndpointDiscoverabilityPolicy = &endpointDiscoverabilityPolicyImpl{
	name: "AlwaysDiscoverable",
	f: func(*IstioEndpoint, *Proxy) bool {
		return true
	},
}

// DiscoverableFromSameCluster is an EndpointDiscoverabilityPolicy that only allows an endpoint to be discoverable
// from proxies within the same cluster.
var DiscoverableFromSameCluster EndpointDiscoverabilityPolicy = &endpointDiscoverabilityPolicyImpl{
	name: "DiscoverableFromSameCluster",
	f: func(ep *IstioEndpoint, p *Proxy) bool {
		return p.InCluster(ep.Locality.ClusterID)
	},
}

// ServiceAttributes represents a group of custom attributes of the service.
type ServiceAttributes struct {
	// ServiceRegistry indicates the backing service registry system where this service
	// was sourced from.
	// TODO: move the ServiceRegistry type from platform.go to model
	ServiceRegistry provider.ID
	// Name is "destination.service.name" attribute
	Name string
	// Namespace is "destination.service.namespace" attribute
	Namespace string
	// Labels applied to the service
	Labels map[string]string
	// ExportTo defines the visibility of Service in
	// a namespace when the namespace is imported.
	ExportTo map[visibility.Instance]bool

	// LabelSelectors are the labels used by the service to select workloads.
	// Applicable to both Kubernetes and ServiceEntries.
	LabelSelectors map[string]string

	// For Kubernetes platform

	// ClusterExternalAddresses is a mapping between a cluster name and the external
	// address(es) to access the service from outside the cluster.
	// Used by the aggregator to aggregate the Attributes.ClusterExternalAddresses
	// for clusters where the service resides
	ClusterExternalAddresses AddressMap

	// ClusterExternalPorts is a mapping between a cluster name and the service port
	// to node port mappings for a given service. When accessing the service via
	// node port IPs, we need to use the kubernetes assigned node ports of the service
	// The port that the user provides in the meshNetworks config is the service port.
	// We translate that to the appropriate node port here.
	ClusterExternalPorts map[cluster.ID]map[uint32]uint32
}

// DeepCopy creates a deep copy of ServiceAttributes, but skips internal mutexes.
func (s *ServiceAttributes) DeepCopy() ServiceAttributes {
	// Nested mutexes are configured to be ignored by copystructure.Copy.
	// Disabling `go vet` warning since this is actually safe in this case.
	// nolint: vet
	return copyInternal(*s).(ServiceAttributes)
}

// ServiceDiscovery enumerates Istio service instances.
// nolint: lll
type ServiceDiscovery interface {
	NetworkGatewaysWatcher

	// Services list declarations of all services in the system
	Services() []*Service

	// GetService retrieves a service by host name if it exists
	GetService(hostname host.Name) *Service

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
	InstancesByPort(svc *Service, servicePort int, labels labels.Collection) []*ServiceInstance

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
	GetProxyServiceInstances(*Proxy) []*ServiceInstance
	GetProxyWorkloadLabels(*Proxy) labels.Collection

	// GetIstioServiceAccounts returns a list of service accounts looked up from
	// the specified service hostname and ports.
	// Deprecated - service account tracking moved to XdsServer, incremental.
	GetIstioServiceAccounts(svc *Service, ports []int) []string

	// MCSServices returns information about the services that have been exported/imported via the
	// Kubernetes Multi-Cluster Services (MCS) ServiceExport API. Only applies to services in
	// Kubernetes clusters.
	MCSServices() []MCSServiceInfo
}

// MCSServiceInfo combines the name of a service with a particular Kubernetes cluster. This
// is used for debug information regarding the state of Kubernetes Multi-Cluster Services (MCS).
type MCSServiceInfo struct {
	Cluster         cluster.ID
	Name            string
	Namespace       string
	Exported        bool
	Imported        bool
	ClusterSetVIP   string
	Discoverability map[host.Name]string
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

// BuildSubsetKey generates a unique string referencing service instances for a given service name, a subset and a port.
// The proxy queries Pilot with this key to obtain the list of instances in a subset.
func BuildSubsetKey(direction TrafficDirection, subsetName string, hostname host.Name, port int) string {
	return string(direction) + "|" + strconv.Itoa(port) + "|" + subsetName + "|" + string(hostname)
}

// BuildInboundSubsetKey generates a unique string referencing service instances with port.
func BuildInboundSubsetKey(port int) string {
	return BuildSubsetKey(TrafficDirectionInbound, "", "", port)
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

// IsDNSSrvSubsetKey checks whether the given key is a DNSSrv key (built by BuildDNSSrvSubsetKey).
func IsDNSSrvSubsetKey(s string) bool {
	if strings.HasPrefix(s, trafficDirectionOutboundSrvPrefix) ||
		strings.HasPrefix(s, trafficDirectionInboundSrvPrefix) {
		return true
	}
	return false
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

// GetAddresses returns a Service's addresses.
// This method returns all the VIPs of a service if the ClusterID is explicitly set to "", otherwise only return the VIP
// specific to the cluster where the node resides
func (s *Service) GetAddresses(node *Proxy) []string {
	if node.Metadata != nil && node.Metadata.ClusterID == "" {
		return s.getAllAddresses()
	}

	return []string{s.GetAddressForProxy(node)}
}

// GetAddressForProxy returns a Service's address specific to the cluster where the node resides
func (s *Service) GetAddressForProxy(node *Proxy) string {
	if node.Metadata != nil {
		if node.Metadata.ClusterID != "" {
			addresses := s.ClusterVIPs.GetAddressesFor(node.Metadata.ClusterID)
			if len(addresses) > 0 {
				return addresses[0]
			}
		}

		if node.Metadata.DNSCapture && node.Metadata.DNSAutoAllocate &&
			s.DefaultAddress == constants.UnspecifiedIP && s.AutoAllocatedAddress != "" {
			return s.AutoAllocatedAddress
		}
	}

	return s.DefaultAddress
}

// getAllAddresses returns a Service's all addresses.
func (s *Service) getAllAddresses() []string {
	var addresses []string
	addressMap := s.ClusterVIPs.GetAddresses()
	for _, clusterAddresses := range addressMap {
		addresses = append(addresses, clusterAddresses...)
	}

	return addresses
}

// GetTLSModeFromEndpointLabels returns the value of the label
// security.istio.io/tlsMode if set. Do not return Enums or constants
// from this function as users could provide values other than istio/disabled
// and apply custom transport socket matchers here.
func GetTLSModeFromEndpointLabels(labels map[string]string) string {
	if labels != nil {
		if val, exists := labels[label.SecurityTlsMode.Name]; exists {
			return val
		}
	}
	return DisabledTLSModeLabel
}

// GetServiceAccounts returns aggregated list of service accounts of Service plus its instances.
func GetServiceAccounts(svc *Service, ports []int, discovery ServiceDiscovery) []string {
	sa := sets.Set{}

	instances := make([]*ServiceInstance, 0)
	// Get the service accounts running service within Kubernetes. This is reflected by the pods that
	// the service is deployed on, and the service accounts of the pods.
	for _, port := range ports {
		svcInstances := discovery.InstancesByPort(svc, port, labels.Collection{})
		instances = append(instances, svcInstances...)
	}

	for _, si := range instances {
		if si.Endpoint.ServiceAccount != "" {
			sa.Insert(si.Endpoint.ServiceAccount)
		}
	}
	sa.Insert(svc.ServiceAccounts...)

	return sa.UnsortedList()
}

// DeepCopy creates a clone of Service.
func (s *Service) DeepCopy() *Service {
	// nolint: govet
	out := *s
	out.Attributes = s.Attributes.DeepCopy()
	if s.Ports != nil {
		out.Ports = make(PortList, len(s.Ports))
		for i, port := range s.Ports {
			out.Ports[i] = &Port{
				Name:     port.Name,
				Port:     port.Port,
				Protocol: port.Protocol,
			}
		}
	}

	if s.ServiceAccounts != nil {
		out.ServiceAccounts = make([]string, len(s.ServiceAccounts))
		for i, sa := range s.ServiceAccounts {
			out.ServiceAccounts[i] = sa
		}
	}
	out.ClusterVIPs = s.ClusterVIPs.DeepCopy()
	return &out
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
