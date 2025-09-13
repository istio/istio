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
	"bytes"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/maps"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/workloadapi/security"
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

	// AutoAllocatedIPv4Address and AutoAllocatedIPv6Address specifies
	// the automatically allocated IPv4/IPv6 address out of the reserved
	// Class E subnet (240.240.0.0/16) or reserved Benchmarking IP range
	// (2001:2::/48) in RFC5180.for service entries with non-wildcard
	// hostnames. The IPs assigned to services are not
	// synchronized across istiod replicas as the DNS resolution
	// for these service entries happens completely inside a pod
	// whose proxy is managed by one istiod. That said, the algorithm
	// to allocate IPs is pretty deterministic that at stable state, two
	// istiods will allocate the exact same set of IPs for a given set of
	// service entries.
	AutoAllocatedIPv4Address string `json:"autoAllocatedIPv4Address,omitempty"`
	AutoAllocatedIPv6Address string `json:"autoAllocatedIPv6Address,omitempty"`

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

// UseInferenceSemantics determines which logic we should use for Service
// This allows InferencePools and Services to both be represented by Service, but have different
// semantics.
func (s *Service) UseInferenceSemantics() bool {
	return s.Attributes.Labels[constants.InternalServiceSemantics] == constants.ServiceSemanticsInferencePool
}

func (s *Service) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: s.Attributes.Name, Namespace: s.Attributes.Namespace}
}

func (s *Service) Key() string {
	if s == nil {
		return ""
	}

	return s.Attributes.Namespace + "/" + string(s.Hostname)
}

var serviceCmpOpts = []cmp.Option{cmpopts.IgnoreFields(AddressMap{}, "mutex")}

func (s *Service) CmpOpts() []cmp.Option {
	return serviceCmpOpts
}

func (s *Service) SupportsDrainingEndpoints() bool {
	return (features.PersistentSessionLabel != "" && s.Attributes.Labels[features.PersistentSessionLabel] != "") ||
		(features.PersistentSessionHeaderLabel != "" && s.Attributes.Labels[features.PersistentSessionHeaderLabel] != "")
}

// SupportsUnhealthyEndpoints marks if this service should send unhealthy endpoints
func (s *Service) SupportsUnhealthyEndpoints() bool {
	if features.GlobalSendUnhealthyEndpoints.Load() {
		// Enable process-wide
		return true
	}
	if s != nil && s.Attributes.TrafficDistribution != TrafficDistributionAny {
		// When we are doing location aware routing, we need some way to indicate if endpoints are healthy, otherwise we don't
		// know when to spill over to other zones.
		// For the older DestinationRule localityLB, we do this by requiring outlier detection.
		// If they use the newer Kubernetes-native TrafficDistribution we don't want to require an Istio-specific outlier rule,
		// and instead will use endpoint health which requires sending unhealthy endpoints.
		return true
	}
	return false
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
	// Alias defines a Service that is an alias for another.
	Alias
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
	// LocalityLabel indicates the region/zone/subzone of an instance. It is used to override the native
	// registry's value.
	//
	// Note: because k8s labels does not support `/`, so we use `.` instead in k8s.
	LocalityLabel = pm.LocalityLabel
)

const (
	// TunnelLabel defines the label workloads describe to indicate that they support tunneling.
	// Values are expected to be a CSV list, sorted by preference, of protocols supported.
	// Currently supported values:
	// * "http": indicates tunneling over HTTP over TCP. HTTP/2 vs HTTP/1.1 may be supported by ALPN negotiation.
	// Planned future values:
	// * "http3": indicates tunneling over HTTP over QUIC. This is distinct from "http", since we cannot do ALPN
	//   negotiation for QUIC vs TCP.
	// Users should appropriately parse the full list rather than doing a string literal check to
	// ensure future-proofing against new protocols being added.
	TunnelLabel = "networking.istio.io/tunnel"
	// TunnelLabelShortName is a short name for TunnelLabel to be used in optimized scenarios.
	TunnelLabelShortName = "tunnel"
	// TunnelHTTP indicates tunneling over HTTP over TCP. HTTP/2 vs HTTP/1.1 may be supported by ALPN
	// negotiation. Note: ALPN negotiation is not currently implemented; HTTP/2 will always be used.
	// This is future-proofed, however, because only the `h2` ALPN is exposed.
	TunnelHTTP = "http"
)

const (
	// TLSModeLabelShortname name used for determining endpoint level tls transport socket configuration
	TLSModeLabelShortname = "tlsMode"

	// DisabledTLSModeLabel implies that this endpoint should receive traffic as is (mostly plaintext)
	DisabledTLSModeLabel = "disabled"

	// IstioMutualTLSModeLabel implies that the endpoint is ready to receive Istio mTLS connections.
	IstioMutualTLSModeLabel = "istio"

	// IstioCanonicalServiceLabelName is the name of label for the Istio Canonical Service for a workload instance.
	IstioCanonicalServiceLabelName = pm.IstioCanonicalServiceLabelName

	// IstioCanonicalServiceRevisionLabelName is the name of label for the Istio Canonical Service revision for a workload instance.
	IstioCanonicalServiceRevisionLabelName = pm.IstioCanonicalServiceRevisionLabelName
)

func SupportsTunnel(labels map[string]string, tunnelType string) bool {
	tl, f := labels[TunnelLabel]
	if !f {
		return false
	}
	if tl == tunnelType {
		// Fast-path the case where we have only one label
		return true
	}
	// Else check everything. Tunnel label is a comma-separated list.
	return sets.New(strings.Split(tl, ",")...).Contains(tunnelType)
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
	// service.
	Port int `json:"port"`

	// Protocol to be used for the port.
	Protocol protocol.Instance `json:"protocol,omitempty"`
}

func (p Port) String() string {
	return fmt.Sprintf("Name:%s Port:%d Protocol:%v", p.Name, p.Port, p.Protocol)
}

// PortList is a set of ports
type PortList []*Port

// TrafficDirection defines whether traffic exists a service instance or enters a service instance
type TrafficDirection string

const (
	// TrafficDirectionInbound indicates inbound traffic
	TrafficDirectionInbound TrafficDirection = "inbound"
	// TrafficDirectionInboundVIP indicates inbound traffic for vip
	TrafficDirectionInboundVIP TrafficDirection = "inbound-vip"
	// TrafficDirectionOutbound indicates outbound traffic
	TrafficDirectionOutbound TrafficDirection = "outbound"

	// trafficDirectionOutboundSrvPrefix the prefix for a DNS SRV type subset key
	trafficDirectionOutboundSrvPrefix = string(TrafficDirectionOutbound) + "_"
	// trafficDirectionInboundSrvPrefix the prefix for a DNS SRV type subset key
	trafficDirectionInboundSrvPrefix = string(TrafficDirectionInbound) + "_"

	// dnsCacheConfigNameSuffix is the suffix used for DNS cache config names
	dnsCacheConfigNameSuffix = "_dfp_dns_cache"
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
//
//	--> IstioEndpoint(172.16.0.1:8888), Service(catalog.myservice.com), Labels(foo=bar)
//	--> IstioEndpoint(172.16.0.2:8888), Service(catalog.myservice.com), Labels(foo=bar)
//	--> IstioEndpoint(172.16.0.3:8888), Service(catalog.myservice.com), Labels(kitty=cat)
//	--> IstioEndpoint(172.16.0.4:8888), Service(catalog.myservice.com), Labels(kitty=cat)
type ServiceInstance struct {
	Service     *Service       `json:"service,omitempty"`
	ServicePort *Port          `json:"servicePort,omitempty"`
	Endpoint    *IstioEndpoint `json:"endpoint,omitempty"`
}

func (instance *ServiceInstance) CmpOpts() []cmp.Option {
	res := []cmp.Option{}
	res = append(res, istioEndpointCmpOpts...)
	res = append(res, serviceCmpOpts...)
	return res
}

// ServiceTarget includes a Service object, along with a specific service port
// and target port. This is basically a smaller version of ServiceInstance,
// intended to avoid the need to have the full object when only port information
// is needed.
type ServiceTarget struct {
	Service *Service
	Port    ServiceInstancePort
}

type (
	ServicePort = *Port
	// ServiceInstancePort defines a port that has both a port and targetPort (which distinguishes it from model.Port)
	// Note: ServiceInstancePort only makes sense in the context of a specific ServiceInstance, because TargetPort depends on a specific instance.
	ServiceInstancePort struct {
		ServicePort
		TargetPort uint32
	}
)

func ServiceInstanceToTarget(e *ServiceInstance) ServiceTarget {
	return ServiceTarget{
		Service: e.Service,
		Port: ServiceInstancePort{
			ServicePort: e.ServicePort,
			TargetPort:  e.Endpoint.EndpointPort,
		},
	}
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

func (instance *WorkloadInstance) CmpOpts() []cmp.Option {
	return istioEndpointCmpOpts
}

// DeepCopy creates a copy of WorkloadInstance.
func (instance *WorkloadInstance) DeepCopy() *WorkloadInstance {
	out := *instance
	out.PortMap = maps.Clone(instance.PortMap)
	out.Endpoint = instance.Endpoint.DeepCopy()
	return &out
}

// WorkloadInstancesEqual is a custom comparison of workload instances based on the fields that we need.
// Returns true if equal, false otherwise.
func WorkloadInstancesEqual(first, second *WorkloadInstance) bool {
	if first.Endpoint == nil || second.Endpoint == nil {
		return first.Endpoint == second.Endpoint
	}

	if !slices.EqualUnordered(first.Endpoint.Addresses, second.Endpoint.Addresses) {
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
	if !maps.Equal(first.PortMap, second.PortMap) {
		return false
	}
	return true
}

// GetLocalityLabel returns the locality from the supplied label. Because Kubernetes
// labels don't support `/`, we replace "." with "/" in the supplied label as a workaround.
func GetLocalityLabel(label string) string {
	return pm.GetLocalityLabel(label)
}

// Locality information for an IstioEndpoint
type Locality struct {
	// Label for locality on the endpoint. This is a "/" separated string.
	Label string

	// ClusterID where the endpoint is located
	ClusterID cluster.ID
}

// HealthStatus indicates the status of the Endpoint.
type HealthStatus int32

const (
	// Healthy indicates an endpoint is ready to accept traffic
	Healthy HealthStatus = 1
	// UnHealthy indicates an endpoint is not ready to accept traffic
	UnHealthy HealthStatus = 2
	// Draining is a special case, which is used only when persistent sessions are enabled. This indicates an endpoint
	// was previously healthy, but is now shutting down.
	// Without persistent sessions, an endpoint that is shutting down will be marked as Terminating.
	Draining HealthStatus = 3
	// Terminating marks an endpoint as shutting down. Similar to "unhealthy", this means we should not send it traffic.
	// But unlike "unhealthy", this means we do not consider it when calculating failover.
	Terminating HealthStatus = 4
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
//
//	--> 172.16.0.1:55446 (with ServicePort pointing to 80) and
//	--> 172.16.0.1:33333 (with ServicePort pointing to 8080)
type IstioEndpoint struct {
	// Labels points to the workload or deployment labels.
	Labels labels.Instance

	// Addresses are the addresses of the endpoint, using envoy proto:
	// https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/endpoint/v3/endpoint_components.proto#config-endpoint-v3-endpoint-additionaladdress
	// This field can support multiple addresses for an Dual Stack endpoint, especially for an endpoint which contains both ipv4 or ipv6 addresses.
	// There should be some constraints below:
	// 1. Each address of the endpoint must have the same metadata.
	// 2. The function Key() of IstioEndpoint returns the first IP address of this field in string format.
	// 3. The IP address of field `address` in Envoy Endpoint is equal to the first address of this field.
	// When the additional_addresses field is populated for EDS in Envoy configuration, Envoy will use an Happy Eyeballs algorithm.
	// Therefore Envoy will first attempt connecting to the IP address in the `address` field of Envoy Endpoint.
	// If the first attempt fails, then it will interleave IP addresses in the `additional_addresses` field based on IP version, as described in rfc8305,
	// and attempt connections with them with a delay of 300ms each. The first connection to succeed will be used.
	// Note: it uses Hash Based Load Balancing Policies for multiple addresses support Endpoint, and only the first address of the
	// endpoint will be used as the hash key for the ring or maglev list, however, the upstream address that load balancer ends up
	// connecting to will depend on the one that ends up "winning" using the Happy Eyeballs algorithm.
	// Please refer to https://docs.google.com/document/d/1AjmTcMWwb7nia4rAgqE-iqIbSbfiXCI4h1vk-FONFdM/ for more details.
	Addresses []string

	// ServicePortName tracks the name of the port, this is used to select the IstioEndpoint by service port.
	ServicePortName string
	// LegacyClusterPortKey provides an alternative key from ServicePortName to support legacy quirks in the API.
	// Basically, EDS merges by port name, but CDS historically ignored port name and matched on number.
	// Note that for Kubernetes Service, this is identical - its only ServiceEntry where these checks can differ
	LegacyClusterPortKey int

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

	// Determines the discoverability of this endpoint throughout the mesh.
	DiscoverabilityPolicy EndpointDiscoverabilityPolicy `json:"-"`

	// Indicates the endpoint health status.
	HealthStatus HealthStatus

	// SendUnhealthyEndpoints indicates whether this endpoint should be sent when it is unhealthy
	// Note: this is more appropriate at the Service level, but some codepaths require this in areas without the service
	// object present.
	SendUnhealthyEndpoints bool

	// If in k8s, the node where the pod resides
	NodeName string
}

func (ep *IstioEndpoint) SupportsTunnel(tunnelType string) bool {
	return SupportsTunnel(ep.Labels, tunnelType)
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

// MetadataClone returns the cloned endpoint metadata used for telemetry purposes.
// This should be used when the endpoint labels should be updated.
func (ep *IstioEndpoint) MetadataClone() *EndpointMetadata {
	return &EndpointMetadata{
		Network:      ep.Network,
		TLSMode:      ep.TLSMode,
		WorkloadName: ep.WorkloadName,
		Namespace:    ep.Namespace,
		Labels:       maps.Clone(ep.Labels),
		ClusterID:    ep.Locality.ClusterID,
	}
}

// Metadata returns the endpoint metadata used for telemetry purposes.
func (ep *IstioEndpoint) Metadata() *EndpointMetadata {
	return &EndpointMetadata{
		Network:      ep.Network,
		TLSMode:      ep.TLSMode,
		WorkloadName: ep.WorkloadName,
		Namespace:    ep.Namespace,
		Labels:       ep.Labels,
		ClusterID:    ep.Locality.ClusterID,
	}
}

var istioEndpointCmpOpts = []cmp.Option{cmpopts.IgnoreUnexported(IstioEndpoint{}), endpointDiscoverabilityPolicyImplCmpOpt, cmp.AllowUnexported()}

func (ep *IstioEndpoint) CmpOpts() []cmp.Option {
	return istioEndpointCmpOpts
}

func (ep *IstioEndpoint) FirstAddressOrNil() string {
	if ep == nil || len(ep.Addresses) == 0 {
		return ""
	}
	return ep.Addresses[0]
}

// Key returns a function suitable for usage to distinguish this IstioEndpoint from another
func (ep *IstioEndpoint) Key() string {
	return ep.FirstAddressOrNil() + "/" + ep.WorkloadName + "/" + ep.ServicePortName
}

// EndpointMetadata represents metadata set on Envoy LbEndpoint used for telemetry purposes.
type EndpointMetadata struct {
	// Network holds the network where this endpoint is present
	Network network.ID

	// TLSMode endpoint is injected with istio sidecar and ready to configure Istio mTLS
	TLSMode string

	// Name of the workload that this endpoint belongs to. This is for telemetry purpose.
	WorkloadName string

	// Namespace that this endpoint belongs to. This is for telemetry purpose.
	Namespace string

	// Labels points to the workload or deployment labels.
	Labels labels.Instance

	// ClusterID where the endpoint is located
	ClusterID cluster.ID
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

var endpointDiscoverabilityPolicyImplCmpOpt = cmp.Comparer(func(x, y endpointDiscoverabilityPolicyImpl) bool {
	return x.String() == y.String()
})

func (p *endpointDiscoverabilityPolicyImpl) CmpOpts() []cmp.Option {
	return []cmp.Option{endpointDiscoverabilityPolicyImplCmpOpt}
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
	ExportTo sets.Set[visibility.Instance]

	// LabelSelectors are the labels used by the service to select workloads.
	// Applicable to both Kubernetes and ServiceEntries.
	LabelSelectors map[string]string

	// Aliases is the resolved set of aliases for this service. This is computed based on a global view of all Service's `AliasFor`
	// fields.
	// For example, if I had two Services with `externalName: foo`, "a" and "b", then the "foo" service would have Aliases=[a,b].
	Aliases []NamespacedHostname

	// For Kubernetes platform

	// ClusterExternalAddresses is a mapping between a cluster name and the external
	// address(es) to access the service from outside the cluster.
	// Used by the aggregator to aggregate the Attributes.ClusterExternalAddresses
	// for clusters where the service resides
	ClusterExternalAddresses *AddressMap

	// ClusterExternalPorts is a mapping between a cluster name and the service port
	// to node port mappings for a given service. When accessing the service via
	// node port IPs, we need to use the kubernetes assigned node ports of the service
	// The port that the user provides in the meshNetworks config is the service port.
	// We translate that to the appropriate node port here.
	ClusterExternalPorts map[cluster.ID]map[uint32]uint32

	PassthroughTargetPorts map[uint32]uint32

	K8sAttributes
}

type NamespacedHostname struct {
	Hostname  host.Name
	Namespace string
}

type K8sAttributes struct {
	// Type holds the value of the corev1.Type of the Kubernetes service
	// spec.Type
	Type string

	// spec.ExternalName
	ExternalName string

	// NodeLocal means the proxy will only forward traffic to node local endpoints
	// spec.InternalTrafficPolicy == Local
	NodeLocal bool

	// TrafficDistribution determines the service-level traffic distribution.
	// This may be overridden by locality load balancing settings.
	TrafficDistribution TrafficDistribution

	// ObjectName is the object name of the underlying object. This may differ from the Service.Attributes.Name for legacy semantics.
	ObjectName string

	// spec.PublishNotReadyAddresses
	PublishNotReadyAddresses bool
}

type TrafficDistribution int

const (
	// TrafficDistributionAny allows any destination
	TrafficDistributionAny TrafficDistribution = iota
	// TrafficDistributionPreferPreferSameZone prefers traffic in same zone, failing over to same region and then network.
	TrafficDistributionPreferSameZone
	// TrafficDistributionPreferNode prefers traffic in same node, failing over to same subzone, then zone, region, and network.
	TrafficDistributionPreferSameNode
)

func GetTrafficDistribution(specValue *string, annotations map[string]string) TrafficDistribution {
	if specValue != nil {
		switch *specValue {
		case corev1.ServiceTrafficDistributionPreferSameZone, corev1.ServiceTrafficDistributionPreferClose:
			return TrafficDistributionPreferSameZone
		case corev1.ServiceTrafficDistributionPreferSameNode:
			return TrafficDistributionPreferSameNode
		}
	}
	// The TrafficDistribution field is quite new, so we allow a legacy annotation option as well
	// This also has some custom types
	trafficDistributionAnnotationValue := strings.ToLower(annotations[annotation.NetworkingTrafficDistribution.Name])
	switch trafficDistributionAnnotationValue {
	case strings.ToLower(corev1.ServiceTrafficDistributionPreferClose), strings.ToLower(corev1.ServiceTrafficDistributionPreferSameZone):
		return TrafficDistributionPreferSameZone
	case strings.ToLower(corev1.ServiceTrafficDistributionPreferSameNode):
		return TrafficDistributionPreferSameNode
	default:
		if trafficDistributionAnnotationValue != "" {
			log.Warnf("Unknown traffic distribution annotation, defaulting to any")
		}
		return TrafficDistributionAny
	}
}

// DeepCopy creates a deep copy of ServiceAttributes, but skips internal mutexes.
func (s *ServiceAttributes) DeepCopy() ServiceAttributes {
	// AddressMap contains a mutex, which is safe to copy in this case.
	// nolint: govet
	out := *s

	out.Labels = maps.Clone(s.Labels)
	if s.ExportTo != nil {
		out.ExportTo = s.ExportTo.Copy()
	}

	out.LabelSelectors = maps.Clone(s.LabelSelectors)
	out.ClusterExternalAddresses = s.ClusterExternalAddresses.DeepCopy()

	if s.ClusterExternalPorts != nil {
		out.ClusterExternalPorts = make(map[cluster.ID]map[uint32]uint32, len(s.ClusterExternalPorts))
		for k, m := range s.ClusterExternalPorts {
			out.ClusterExternalPorts[k] = maps.Clone(m)
		}
	}

	out.Aliases = slices.Clone(s.Aliases)
	out.PassthroughTargetPorts = maps.Clone(out.PassthroughTargetPorts)

	// AddressMap contains a mutex, which is safe to return a copy in this case.
	// nolint: govet
	return out
}

// Equals checks whether the attributes are equal from the passed in service.
func (s *ServiceAttributes) Equals(other *ServiceAttributes) bool {
	if s == nil {
		return other == nil
	}
	if other == nil {
		return s == nil
	}

	if !maps.Equal(s.Labels, other.Labels) {
		return false
	}

	if !maps.Equal(s.LabelSelectors, other.LabelSelectors) {
		return false
	}

	if !maps.Equal(s.ExportTo, other.ExportTo) {
		return false
	}

	if !slices.Equal(s.Aliases, other.Aliases) {
		return false
	}

	if s.ClusterExternalAddresses.Len() != other.ClusterExternalAddresses.Len() {
		return false
	}

	for k, v1 := range s.ClusterExternalAddresses.GetAddresses() {
		if v2, ok := other.ClusterExternalAddresses.Addresses[k]; !ok || !slices.Equal(v1, v2) {
			return false
		}
	}

	if len(s.ClusterExternalPorts) != len(other.ClusterExternalPorts) {
		return false
	}

	for k, v1 := range s.ClusterExternalPorts {
		if v2, ok := s.ClusterExternalPorts[k]; !ok || !maps.Equal(v1, v2) {
			return false
		}
	}
	return s.Name == other.Name && s.Namespace == other.Namespace &&
		s.ServiceRegistry == other.ServiceRegistry && s.K8sAttributes == other.K8sAttributes
}

// ServiceDiscovery enumerates Istio service instances.
// nolint: lll
type ServiceDiscovery interface {
	NetworkGatewaysWatcher

	// Services list declarations of all services in the system
	Services() []*Service

	// GetService retrieves a service by host name if it exists
	GetService(hostname host.Name) *Service

	// GetProxyServiceTargets returns the service targets that co-located with a given Proxy
	//
	// Co-located generally means running in the same network namespace and security context.
	//
	// A Proxy operating as a Sidecar will return a non-empty slice.  A stand-alone Proxy
	// will return an empty slice.
	//
	// There are two reasons why this returns multiple ServiceTargets instead of one:
	// - A ServiceTargets has a single Port.  But a Service
	//   may have many ports.  So a workload implementing such a Service would need
	//   multiple ServiceTargets, one for each port.
	// - A single workload may implement multiple logical Services.
	//
	// In the second case, multiple services may be implemented by the same physical port number,
	// though with a different ServicePort and IstioEndpoint for each.  If any of these overlapping
	// services are not HTTP or H2-based, behavior is undefined, since the listener may not be able to
	// determine the intended destination of a connection without a Host header on the request.
	GetProxyServiceTargets(*Proxy) []ServiceTarget
	GetProxyWorkloadLabels(*Proxy) labels.Instance

	// MCSServices returns information about the services that have been exported/imported via the
	// Kubernetes Multi-Cluster Services (MCS) ServiceExport API. Only applies to services in
	// Kubernetes clusters.
	MCSServices() []MCSServiceInfo
	AmbientIndexes
}

type AmbientIndexes interface {
	ServicesWithWaypoint(key string) []ServiceWaypointInfo
	AddressInformation(addresses sets.String) ([]AddressInfo, sets.String)
	AdditionalPodSubscriptions(
		proxy *Proxy,
		allAddresses sets.String,
		currentSubs sets.String,
	) sets.String
	Policies(requested sets.Set[ConfigKey]) []WorkloadAuthorization
	ServicesForWaypoint(WaypointKey) []ServiceInfo
	WorkloadsForWaypoint(WaypointKey) []WorkloadInfo
}

// WaypointKey is a multi-address extension of NetworkAddress which is commonly used for lookups in AmbientIndex
// We likely need to consider alternative keying options internally such as hostname as we look to expand beyond istio-waypoint
// This extension can ideally support that type of lookup in the interface without introducing scope creep into things
// like NetworkAddress
type WaypointKey struct {
	Namespace string
	Hostnames []string

	Network   string
	Addresses []string

	IsNetworkGateway bool
}

// WaypointKeyForProxy builds a key from a proxy to lookup
func WaypointKeyForProxy(node *Proxy) WaypointKey {
	return waypointKeyForProxy(node, false)
}

func WaypointKeyForNetworkGatewayProxy(node *Proxy) WaypointKey {
	return waypointKeyForProxy(node, true)
}

func waypointKeyForProxy(node *Proxy, externalAddresses bool) WaypointKey {
	key := WaypointKey{
		Namespace:        node.ConfigNamespace,
		Network:          node.Metadata.Network.String(),
		IsNetworkGateway: externalAddresses, // true if this is a network gateway proxy, false if it is a regular waypoint proxy
	}
	for _, svct := range node.ServiceTargets {
		key.Hostnames = append(key.Hostnames, svct.Service.Hostname.String())

		var ips []string
		if externalAddresses {
			ips = svct.Service.Attributes.ClusterExternalAddresses.GetAddressesFor(node.GetClusterID())
		} else {
			ips = svct.Service.ClusterVIPs.GetAddressesFor(node.GetClusterID())
		}
		// if we find autoAllocated addresses then ips should contain constants.UnspecifiedIP which should not be used
		foundAutoAllocated := false
		if svct.Service.AutoAllocatedIPv4Address != "" {
			key.Addresses = append(key.Addresses, svct.Service.AutoAllocatedIPv4Address)
			foundAutoAllocated = true
		}
		if svct.Service.AutoAllocatedIPv6Address != "" {
			key.Addresses = append(key.Addresses, svct.Service.AutoAllocatedIPv6Address)
			foundAutoAllocated = true
		}
		if !foundAutoAllocated {
			key.Addresses = append(key.Addresses, ips...)
		}
	}
	return key
}

// NoopAmbientIndexes provides an implementation of AmbientIndexes that always returns nil, to easily "skip" it.
type NoopAmbientIndexes struct{}

func (u NoopAmbientIndexes) AddressInformation(sets.String) ([]AddressInfo, sets.String) {
	return nil, nil
}

func (u NoopAmbientIndexes) AdditionalPodSubscriptions(
	*Proxy,
	sets.String,
	sets.String,
) sets.String {
	return nil
}

func (u NoopAmbientIndexes) Policies(sets.Set[ConfigKey]) []WorkloadAuthorization {
	return nil
}

func (u NoopAmbientIndexes) ServicesForWaypoint(WaypointKey) []ServiceInfo {
	return nil
}

func (u NoopAmbientIndexes) Waypoint(string, string) []netip.Addr {
	return nil
}

func (u NoopAmbientIndexes) WorkloadsForWaypoint(WaypointKey) []WorkloadInfo {
	return nil
}

func (u NoopAmbientIndexes) ServicesWithWaypoint(string) []ServiceWaypointInfo {
	return nil
}

var _ AmbientIndexes = NoopAmbientIndexes{}

type AddressInfo struct {
	*workloadapi.Address
	Marshaled *anypb.Any
}

func (i AddressInfo) Equals(other AddressInfo) bool {
	return protoconv.Equals(i.Address, other.Address)
}

func (i AddressInfo) Aliases() []string {
	switch addr := i.Type.(type) {
	case *workloadapi.Address_Workload:
		aliases := make([]string, 0, len(addr.Workload.Addresses))
		network := addr.Workload.Network
		for _, workloadAddr := range addr.Workload.Addresses {
			ip, _ := netip.AddrFromSlice(workloadAddr)
			aliases = append(aliases, network+"/"+ip.String())
		}
		return aliases
	case *workloadapi.Address_Service:
		aliases := make([]string, 0, len(addr.Service.Addresses))
		for _, networkAddr := range addr.Service.Addresses {
			ip, _ := netip.AddrFromSlice(networkAddr.Address)
			aliases = append(aliases, networkAddr.Network+"/"+ip.String())
		}
		return aliases
	}
	return nil
}

func (i AddressInfo) ResourceName() string {
	var name string
	switch addr := i.Type.(type) {
	case *workloadapi.Address_Workload:
		name = workloadResourceName(addr.Workload)
	case *workloadapi.Address_Service:
		name = serviceResourceName(addr.Service)
	}
	return name
}

type ServiceWaypointInfo struct {
	Service            *workloadapi.Service
	IngressUseWaypoint bool
	WaypointHostname   string
}

type TypedObject struct {
	types.NamespacedName
	Kind kind.Kind
}

type ServicePortName struct {
	PortName       string
	TargetPortName string
}

type ServiceInfo struct {
	Service *workloadapi.Service
	// LabelSelectors for the Service. Note these are only used internally, not sent over XDS
	LabelSelector LabelSelector
	// PortNames provides a mapping of ServicePort -> port names. Note these are only used internally, not sent over XDS
	PortNames map[int32]ServicePortName
	// Source is the type that introduced this service.
	Source TypedObject
	// Scope of the service - either local or global based on namespace or service label matching
	Scope    ServiceScope
	Waypoint WaypointBindingStatus
	// MarshaledAddress contains the pre-marshaled representation.
	// Note: this is an Address -- not a Service.
	MarshaledAddress *anypb.Any
	// AsAddress contains a pre-created AddressInfo representation. This ensures we do not need repeated conversions on
	// the hotpath
	AsAddress AddressInfo
}

func (i ServiceInfo) GetLabelSelector() map[string]string {
	return i.LabelSelector.Labels
}

func (i ServiceInfo) GetStatusTarget() TypedObject {
	return i.Source
}

type ConditionType string

const (
	WaypointBound    ConditionType = "istio.io/WaypointBound"
	ZtunnelAccepted  ConditionType = "ZtunnelAccepted"
	WaypointAccepted ConditionType = "WaypointAccepted"
)

type ConditionSet = map[ConditionType]*Condition

type Condition struct {
	ObservedGeneration int64
	Reason             string
	Message            string
	Status             bool
}

func (c *Condition) Equals(v *Condition) bool {
	return c.ObservedGeneration == v.ObservedGeneration &&
		c.Reason == v.Reason &&
		c.Message == v.Message &&
		c.Status == v.Status
}

func (i ServiceInfo) GetConditions() ConditionSet {
	set := ConditionSet{
		// Write all conditions here, then override if we want them set.
		// This ensures we can properly prune the condition if its no longer needed (such as if there is no waypoint attached at all).
		WaypointBound: nil,
	}

	if i.Waypoint.ResourceName != "" {
		buildMsg := strings.Builder{}
		buildMsg.WriteString("Successfully attached to waypoint ")
		buildMsg.WriteString(i.Waypoint.ResourceName)

		if i.Waypoint.IngressUseWaypoint {
			buildMsg.WriteString(". Ingress traffic will traverse the waypoint")
		} else if i.Waypoint.IngressLabelPresent {
			buildMsg.WriteString(". Ingress traffic is not using the waypoint, set the istio.io/ingress-use-waypoint label to true if desired.")
		}

		set[WaypointBound] = &Condition{
			Status:  true,
			Reason:  string(WaypointAccepted),
			Message: buildMsg.String(),
		}
	} else if i.Waypoint.Error != nil {
		set[WaypointBound] = &Condition{
			Status:  false,
			Reason:  i.Waypoint.Error.Reason,
			Message: i.Waypoint.Error.Message,
		}
	}

	return set
}

type WaypointBindingStatus struct {
	// ResourceName that clients should use when addressing traffic to this Service.
	ResourceName string
	// IngressUseWaypoint specifies whether ingress gateways should use the waypoint for this service.
	IngressUseWaypoint bool
	// IngressLabelPresent specifies whether the istio.io/ingress-use-waypoint label is set on the service.
	IngressLabelPresent bool
	// Error represents some error
	Error *StatusMessage
}

type StatusMessage struct {
	Reason  string
	Message string
}

func (i WaypointBindingStatus) Equals(other WaypointBindingStatus) bool {
	return i.ResourceName == other.ResourceName &&
		i.IngressUseWaypoint == other.IngressUseWaypoint &&
		i.IngressLabelPresent == other.IngressLabelPresent &&
		ptr.Equal(i.Error, other.Error)
}

func (i ServiceInfo) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: i.Service.Name, Namespace: i.Service.Namespace}
}

func (i ServiceInfo) GetNamespace() string {
	return i.Service.Namespace
}

func (i ServiceInfo) Equals(other ServiceInfo) bool {
	return equalUsingPremarshaled(i.Service, i.MarshaledAddress, other.Service, other.MarshaledAddress) &&
		maps.Equal(i.LabelSelector.Labels, other.LabelSelector.Labels) &&
		maps.Equal(i.PortNames, other.PortNames) &&
		i.Source == other.Source &&
		i.Scope == other.Scope &&
		i.Waypoint.Equals(other.Waypoint)
}

func (i ServiceInfo) ResourceName() string {
	return serviceResourceName(i.Service)
}

func serviceResourceName(s *workloadapi.Service) string {
	return s.Namespace + "/" + s.Hostname
}

type ServiceScope string

const (
	// Local ServiceScope specifies that istiod will not automatically expose the matching services' endpoints at the
	// cluster's east/west gateway. Istio will also not automatically share locolly matching endpoints with the
	// cluster's local dataplane that are not within the local cluster.
	Local ServiceScope = "LOCAL"
	// Global ServiceScope specifies that istiod will automatically expose the matching services' endpoints at the
	// cluster's east/west gateway. Istio will also automatically share globally matching endpoints with the cluster's
	// local dataplane that are in the local and remote clusters.
	Global ServiceScope = "GLOBAL"
)

type WorkloadInfo struct {
	Workload *workloadapi.Workload
	// Labels for the workload. Note these are only used internally, not sent over XDS
	Labels map[string]string
	// Source is the type that introduced this workload.
	Source kind.Kind
	// CreationTime is the time when the workload was created. Note this is used internally only.
	CreationTime time.Time
	// MarshaledAddress contains the pre-marshaled representation.
	// Note: this is an Address -- not a Workload.
	MarshaledAddress *anypb.Any
	// AsAddress contains a pre-created AddressInfo representation. This ensures we do not need repeated conversions on
	// the hotpath
	AsAddress AddressInfo
}

func (i WorkloadInfo) Equals(other WorkloadInfo) bool {
	return equalUsingPremarshaled(i.Workload, i.MarshaledAddress, other.Workload, other.MarshaledAddress) &&
		maps.Equal(i.Labels, other.Labels) &&
		i.Source == other.Source &&
		i.CreationTime == other.CreationTime
}

func workloadResourceName(w *workloadapi.Workload) string {
	return w.Uid
}

func (i *WorkloadInfo) Clone() *WorkloadInfo {
	return &WorkloadInfo{
		Workload:     protomarshal.Clone(i.Workload),
		Labels:       maps.Clone(i.Labels),
		Source:       i.Source,
		CreationTime: i.CreationTime,
	}
}

func (i WorkloadInfo) ResourceName() string {
	return workloadResourceName(i.Workload)
}

type WaypointPolicyStatus struct {
	Source     TypedObject
	Conditions []PolicyBindingStatus
}

const (
	WaypointPolicyReasonAccepted         = "Accepted"
	WaypointPolicyReasonInvalid          = "Invalid"
	WaypointPolicyReasonPartiallyInvalid = "PartiallyInvalid"
	WaypointPolicyReasonAncestorNotBound = "AncestorNotBound"
	WaypointPolicyReasonTargetNotFound   = "TargetNotFound"
)

// impl pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue/StatusWriter
func (i WaypointPolicyStatus) GetStatusTarget() TypedObject {
	return i.Source
}

func (i WaypointPolicyStatus) GetConditions() ConditionSet {
	set := make(ConditionSet, 1)

	set[WaypointAccepted] = flattenConditions(i.Conditions)

	return set
}

// end impl StatusWriter

// flattenConditions is a work around for the uncertain future of Ancestor in gtwapi which exists at the moment.
// It is intended to take many conditions which have ancestors and condense them into a single condition so we can
// retain detail in the codebase to be prepared when a canonical representation is accepted upstream.
func flattenConditions(conditions []PolicyBindingStatus) *Condition {
	status := false
	reason := WaypointPolicyReasonInvalid
	unboundAncestors := []string{}
	var message string

	// flatten causes a loss of some information and there is only 1 condition so no need to flatten
	if len(conditions) == 1 {
		c := conditions[0]
		return &Condition{
			ObservedGeneration: c.ObservedGeneration,
			Reason:             c.Status.Reason,
			Message:            c.Status.Message,
			Status:             c.Bound,
		}
	}

	var highestObservedGeneration int64
	for _, c := range conditions {
		if c.Bound {
			// if anything was true we consider the overall bind to be true
			status = true
		} else {
			unboundAncestors = append(unboundAncestors, c.Ancestor)
		}

		if c.ObservedGeneration > highestObservedGeneration {
			highestObservedGeneration = c.ObservedGeneration
		}
	}

	someUnbound := len(unboundAncestors) > 0

	if status {
		reason = WaypointPolicyReasonAccepted
	}

	if status && someUnbound {
		reason = WaypointPolicyReasonPartiallyInvalid
	}

	if someUnbound {
		message = fmt.Sprintf("Invalid targetRefs: %s", strings.Join(unboundAncestors, ", "))
	}

	return &Condition{
		highestObservedGeneration,
		reason,
		message,
		status,
	}
}

// impl pkg/kube/krt/ResourceNamer
func (i WaypointPolicyStatus) ResourceName() string {
	return i.Source.Namespace + "/" + i.Source.Name
}

// end impl ResourceNamer

type PolicyBindingStatus struct {
	ObservedGeneration int64
	Ancestor           string
	Status             *StatusMessage
	Bound              bool
}

func (i PolicyBindingStatus) Equals(other PolicyBindingStatus) bool {
	return ptr.Equal(i.Status, other.Status) &&
		i.Bound == other.Bound &&
		i.Ancestor == other.Ancestor &&
		i.ObservedGeneration == other.ObservedGeneration
}

type WorkloadAuthorization struct {
	// LabelSelectors for the workload. Note these are only used internally, not sent over XDS
	LabelSelector
	Authorization *security.Authorization

	Source  TypedObject
	Binding PolicyBindingStatus
}

// impl pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue/StatusWriter
func (i WorkloadAuthorization) GetStatusTarget() TypedObject {
	return i.Source
}

func (i WorkloadAuthorization) GetConditions() ConditionSet {
	set := make(ConditionSet, 1)

	if i.Binding.Status != nil {
		set[ZtunnelAccepted] = &Condition{
			ObservedGeneration: i.Binding.ObservedGeneration,
			Reason:             i.Binding.Status.Reason,
			Message:            i.Binding.Status.Message,
			Status:             i.Binding.Bound,
		}
	} else {
		message := "attached to ztunnel"
		set[ZtunnelAccepted] = &Condition{
			ObservedGeneration: i.Binding.ObservedGeneration,
			Reason:             "Accepted",
			Message:            message,
			Status:             i.Binding.Bound,
		}
	}

	return set
}

// end impl StatusWriter

func (i WorkloadAuthorization) Equals(other WorkloadAuthorization) bool {
	return protoconv.Equals(i.Authorization, other.Authorization) &&
		maps.Equal(i.Labels, other.Labels) &&
		i.Source == other.Source &&
		i.Binding.Equals(other.Binding)
}

func (i WorkloadAuthorization) ResourceName() string {
	return i.Authorization.GetNamespace() + "/" + i.Authorization.GetName()
}

type LabelSelector struct {
	Labels map[string]string
}

func NewSelector(l map[string]string) LabelSelector {
	return LabelSelector{l}
}

func (l LabelSelector) GetLabelSelector() map[string]string {
	return l.Labels
}

func ExtractWorkloadsFromAddresses(addrs []AddressInfo) []WorkloadInfo {
	return slices.MapFilter(addrs, func(a AddressInfo) *WorkloadInfo {
		switch addr := a.Type.(type) {
		case *workloadapi.Address_Workload:
			return &WorkloadInfo{Workload: addr.Workload}
		default:
			return nil
		}
	})
}

func SortWorkloadsByCreationTime(workloads []WorkloadInfo) []WorkloadInfo {
	sort.SliceStable(workloads, func(i, j int) bool {
		if workloads[i].CreationTime.Equal(workloads[j].CreationTime) {
			return workloads[i].Workload.Uid < workloads[j].Workload.Uid
		}
		return workloads[i].CreationTime.Before(workloads[j].CreationTime)
	})
	return workloads
}

type NamespaceInfo struct {
	Name               string
	IngressUseWaypoint bool
}

func (i NamespaceInfo) ResourceName() string {
	return i.Name
}

func (i NamespaceInfo) Equals(other NamespaceInfo) bool {
	return i == other
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

func (p *Port) Equals(other *Port) bool {
	if p == nil {
		return other == nil
	}
	if other == nil {
		return p == nil
	}
	return p.Name == other.Name && p.Port == other.Port && p.Protocol == other.Protocol
}

func (ports PortList) Equals(other PortList) bool {
	return slices.EqualFunc(ports, other, func(a, b *Port) bool {
		return a.Equals(b)
	})
}

func (ports PortList) String() string {
	sp := make([]string, 0, len(ports))
	for _, p := range ports {
		sp = append(sp, p.String())
	}
	return strings.Join(sp, ", ")
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

// BuildDNSCacheName generates a hostname specific DNS cache config name.
func BuildDNSCacheName(hostname host.Name) string {
	return hostname.String() + dnsCacheConfigNameSuffix
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

// ParseSubsetKeyHostname is an optimized specialization of ParseSubsetKey that only returns the hostname.
// This is created as this is used in some hot paths and is about 2x faster than ParseSubsetKey; for typical use ParseSubsetKey is sufficient (and zero-alloc).
func ParseSubsetKeyHostname(s string) (hostname string) {
	idx := strings.LastIndex(s, "|")
	if idx == -1 {
		// Could be DNS SRV format.
		// Do not do LastIndex("_."), as those are valid characters in the hostname (unlike |)
		// Fallback to the full parser.
		_, _, hostname, _ := ParseSubsetKey(s)
		return string(hostname)
	}
	return s[idx+1:]
}

// ParseSubsetKey is the inverse of the BuildSubsetKey method
func ParseSubsetKey(s string) (direction TrafficDirection, subsetName string, hostname host.Name, port int) {
	sep := "|"
	// This could be the DNS srv form of the cluster that uses outbound_.port_.subset_.hostname
	// Since we do not want every callsite to implement the logic to differentiate between the two forms
	// we add an alternate parser here.
	if strings.HasPrefix(s, trafficDirectionOutboundSrvPrefix) ||
		strings.HasPrefix(s, trafficDirectionInboundSrvPrefix) {
		sep = "_."
	}

	// Format: dir|port|subset|hostname
	dir, s, ok := strings.Cut(s, sep)
	if !ok {
		return direction, subsetName, hostname, port
	}
	direction = TrafficDirection(dir)

	p, s, ok := strings.Cut(s, sep)
	if !ok {
		return direction, subsetName, hostname, port
	}
	port, _ = strconv.Atoi(p)

	ss, s, ok := strings.Cut(s, sep)
	if !ok {
		return direction, subsetName, hostname, port
	}
	subsetName = ss

	// last part. No | remains -- verify this
	if strings.Contains(s, sep) {
		return direction, subsetName, hostname, port
	}
	hostname = host.Name(s)
	return direction, subsetName, hostname, port
}

// GetAddressForProxy returns a Service's address specific to the cluster where the node resides
func (s *Service) GetAddressForProxy(node *Proxy) string {
	if node.Metadata != nil {
		if node.Metadata.ClusterID != "" {
			addresses := s.ClusterVIPs.GetAddressesFor(node.Metadata.ClusterID)
			addresses = filterAddresses(addresses, node.SupportsIPv4(), node.SupportsIPv6())
			if len(addresses) > 0 {
				return addresses[0]
			}
		}

		if nodeUsesAutoallocatedIPs(node) && s.DefaultAddress == constants.UnspecifiedIP {
			if node.SupportsIPv4() && s.AutoAllocatedIPv4Address != "" {
				return s.AutoAllocatedIPv4Address
			}
			if node.SupportsIPv6() && s.AutoAllocatedIPv6Address != "" {
				return s.AutoAllocatedIPv6Address
			}
		}
	}

	// fallback to the default address
	// TODO: this maybe not right, as the default address may not be the right ip family. We need to come up with a good solution.
	return s.DefaultAddress
}

// GetExtraAddressesForProxy returns a k8s service's extra addresses to the cluster where the node resides.
// Especially for dual stack k8s service to get other IP family addresses.
func (s *Service) GetExtraAddressesForProxy(node *Proxy) []string {
	addresses := s.getAllAddressesForProxy(node)
	if len(addresses) > 1 {
		return addresses[1:]
	}
	return nil
}

// HasAddressOrAssigned returns whether the service has an IP address.
// This includes auto-allocated IP addresses. Note that not all proxies support auto-allocated IP addresses;
// typically GetAllAddressesForProxy should be used which automatically filters addresses to account for that.
func (s *Service) HasAddressOrAssigned(id cluster.ID) bool {
	if id != "" {
		if len(s.ClusterVIPs.GetAddressesFor(id)) > 0 {
			return true
		}
	}
	if s.DefaultAddress != constants.UnspecifiedIP {
		return true
	}
	if s.AutoAllocatedIPv4Address != "" {
		return true
	}
	if s.AutoAllocatedIPv6Address != "" {
		return true
	}
	return false
}

// GetAllAddressesForProxy returns a k8s service's all addresses to the cluster where the node resides.
// Especially for dual stack k8s service to get other IP family addresses.
func (s *Service) GetAllAddressesForProxy(node *Proxy) []string {
	return s.getAllAddressesForProxy(node)
}

// nodeUsesAutoallocatedIPs checks to see if this node is eligible to consume automatically allocated IPs
func nodeUsesAutoallocatedIPs(node *Proxy) bool {
	if node == nil {
		return false
	}
	var DNSAutoAllocate, DNSCapture bool
	if node.Metadata != nil {
		DNSAutoAllocate = bool(node.Metadata.DNSAutoAllocate)
		DNSCapture = bool(node.Metadata.DNSCapture)
	}
	// check whether either version of auto-allocation is enabled
	autoallocationEnabled := DNSAutoAllocate || features.EnableIPAutoallocate

	// check if this proxy is a type that always consumes or has consumption explicitly enabled
	nodeConsumesAutoIP := DNSCapture || node.Type == Waypoint

	return autoallocationEnabled && nodeConsumesAutoIP
}

func (s *Service) getAllAddressesForProxy(node *Proxy) []string {
	addresses := []string{}
	if node.Metadata != nil && node.Metadata.ClusterID != "" {
		addresses = s.ClusterVIPs.GetAddressesFor(node.Metadata.ClusterID)
	}
	if len(addresses) == 0 && nodeUsesAutoallocatedIPs(node) {
		// The criteria to use AutoAllocated addresses is met so we should go ahead and use them if they are populated
		if s.AutoAllocatedIPv4Address != "" {
			addresses = append(addresses, s.AutoAllocatedIPv4Address)
		}
		if s.AutoAllocatedIPv6Address != "" {
			addresses = append(addresses, s.AutoAllocatedIPv6Address)
		}
	}
	if (!features.EnableDualStack && !features.EnableAmbient) || node.GetIPMode() != Dual {
		addresses = filterAddresses(addresses, node.SupportsIPv4(), node.SupportsIPv6())
	}
	if len(addresses) > 0 {
		return addresses
	}

	// fallback to the default address
	if a := s.DefaultAddress; len(a) > 0 {
		return []string{a}
	}
	return nil
}

func filterAddresses(addresses []string, supportsV4, supportsV6 bool) []string {
	if len(addresses) == 0 {
		return nil
	}

	var ipv4Addresses []string
	var ipv6Addresses []string
	for _, addr := range addresses {
		// check if an address is a CIDR range
		if strings.Contains(addr, "/") {
			if prefix, err := netip.ParsePrefix(addr); err != nil {
				log.Warnf("failed to parse prefix address '%s': %s", addr, err)
				continue
			} else if supportsV4 && prefix.Addr().Is4() {
				ipv4Addresses = append(ipv4Addresses, addr)
			} else if supportsV6 && prefix.Addr().Is6() {
				ipv6Addresses = append(ipv6Addresses, addr)
			}
		} else {
			if ipAddr, err := netip.ParseAddr(addr); err != nil {
				log.Warnf("failed to parse address '%s': %s", addr, err)
				continue
			} else if supportsV4 && ipAddr.Is4() {
				ipv4Addresses = append(ipv4Addresses, addr)
			} else if supportsV6 && ipAddr.Is6() {
				ipv6Addresses = append(ipv6Addresses, addr)
			}
		}
	}

	if supportsV4 && supportsV6 {
		firstAddrFamily := ""
		if strings.Contains(addresses[0], "/") {
			if prefix, err := netip.ParsePrefix(addresses[0]); err == nil {
				if prefix.Addr().Is4() {
					firstAddrFamily = "v4"
				} else if prefix.Addr().Is6() {
					firstAddrFamily = "v6"
				}
			}
		} else {
			if ipAddr, err := netip.ParseAddr(addresses[0]); err == nil {
				if ipAddr.Is4() {
					firstAddrFamily = "v4"
				} else if ipAddr.Is6() {
					firstAddrFamily = "v6"
				}
			}
		}

		if firstAddrFamily == "v4" {
			return ipv4Addresses
		} else if firstAddrFamily == "v6" {
			return ipv6Addresses
		}
	}

	if len(ipv4Addresses) > 0 {
		return ipv4Addresses
	}
	return ipv6Addresses
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

// DeepCopy creates a clone of Service.
func (s *Service) DeepCopy() *Service {
	// nolint: govet
	out := *s
	out.Attributes = s.Attributes.DeepCopy()
	if s.Ports != nil {
		out.Ports = make(PortList, len(s.Ports))
		for i, port := range s.Ports {
			if port != nil {
				out.Ports[i] = &Port{
					Name:     port.Name,
					Port:     port.Port,
					Protocol: port.Protocol,
				}
			} else {
				out.Ports[i] = nil
			}
		}
	}

	out.ServiceAccounts = slices.Clone(s.ServiceAccounts)
	out.ClusterVIPs = *s.ClusterVIPs.DeepCopy()
	return &out
}

// Equals compares two service objects.
func (s *Service) Equals(other *Service) bool {
	if s == nil {
		return other == nil
	}
	if other == nil {
		return s == nil
	}

	if !s.Attributes.Equals(&other.Attributes) {
		return false
	}

	if !s.Ports.Equals(other.Ports) {
		return false
	}
	if !slices.Equal(s.ServiceAccounts, other.ServiceAccounts) {
		return false
	}

	if len(s.ClusterVIPs.Addresses) != len(other.ClusterVIPs.Addresses) {
		return false
	}
	for k, v1 := range s.ClusterVIPs.Addresses {
		if v2, ok := other.ClusterVIPs.Addresses[k]; !ok || !slices.Equal(v1, v2) {
			return false
		}
	}

	return s.DefaultAddress == other.DefaultAddress && s.AutoAllocatedIPv4Address == other.AutoAllocatedIPv4Address &&
		s.AutoAllocatedIPv6Address == other.AutoAllocatedIPv6Address && s.Hostname == other.Hostname &&
		s.Resolution == other.Resolution && s.MeshExternal == other.MeshExternal
}

// DeepCopy creates a clone of IstioEndpoint.
func (ep *IstioEndpoint) DeepCopy() *IstioEndpoint {
	if ep == nil {
		return nil
	}

	out := *ep
	out.Labels = maps.Clone(ep.Labels)
	out.Addresses = slices.Clone(ep.Addresses)

	return &out
}

// ShallowCopy creates a shallow clone of IstioEndpoint.
func (ep *IstioEndpoint) ShallowCopy() *IstioEndpoint {
	// nolint: govet
	cpy := *ep
	return &cpy
}

// Equals checks whether the attributes are equal from the passed in service.
func (ep *IstioEndpoint) Equals(other *IstioEndpoint) bool {
	if ep == nil {
		return other == nil
	}
	if other == nil {
		return ep == nil
	}

	// Check things we can directly compare...
	eq := ep.ServicePortName == other.ServicePortName &&
		ep.LegacyClusterPortKey == other.LegacyClusterPortKey &&
		ep.ServiceAccount == other.ServiceAccount &&
		ep.Network == other.Network &&
		ep.Locality == other.Locality &&
		ep.EndpointPort == other.EndpointPort &&
		ep.LbWeight == other.LbWeight &&
		ep.TLSMode == other.TLSMode &&
		ep.Namespace == other.Namespace &&
		ep.WorkloadName == other.WorkloadName &&
		ep.HostName == other.HostName &&
		ep.SubDomain == other.SubDomain &&
		ep.HealthStatus == other.HealthStatus &&
		ep.SendUnhealthyEndpoints == other.SendUnhealthyEndpoints &&
		ep.NodeName == other.NodeName
	if !eq {
		return false
	}

	// check everything else
	if !slices.EqualUnordered(ep.Addresses, other.Addresses) {
		return false
	}
	if !maps.Equal(ep.Labels, other.Labels) {
		return false
	}

	// Compare discoverability by name
	var epp string
	if ep.DiscoverabilityPolicy != nil {
		epp = ep.DiscoverabilityPolicy.String()
	}
	var op string
	if other.DiscoverabilityPolicy != nil {
		op = other.DiscoverabilityPolicy.String()
	}
	if epp != op {
		return false
	}

	return true
}

func equalUsingPremarshaled[T proto.Message](a T, am *anypb.Any, b T, bm *anypb.Any) bool {
	// If they are both pre-marshaled, use the marshaled representation. This is orders of magnitude faster
	if am != nil && bm != nil {
		return bytes.Equal(am.Value, bm.Value)
	}

	// Fallback to equals
	return protoconv.Equals(a, b)
}
