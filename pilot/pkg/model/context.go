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

package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	gogojsonpb "github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/monitoring"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
)

var _ mesh.Holder = &Environment{}
var _ mesh.NetworksHolder = &Environment{}

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Config interface for listing routing rules
	IstioConfigStore

	// Watcher is the watcher for the mesh config (to be merged into the config store)
	mesh.Watcher

	// NetworksWatcher (loaded from a config map) provides information about the
	// set of networks inside a mesh and how to route to endpoints in each
	// network. Each network provides information about the endpoints in a
	// routable L3 network. A single routable L3 network can have one or more
	// service registries.
	mesh.NetworksWatcher

	// PushContext holds informations during push generation. It is reset on config change, at the beginning
	// of the pushAll. It will hold all errors and stats and possibly caches needed during the entire cache computation.
	// DO NOT USE EXCEPT FOR TESTS AND HANDLING OF NEW CONNECTIONS.
	// ALL USE DURING A PUSH SHOULD USE THE ONE CREATED AT THE
	// START OF THE PUSH, THE GLOBAL ONE MAY CHANGE AND REFLECT A DIFFERENT
	// CONFIG AND PUSH
	PushContext *PushContext

	// DomainSuffix provides a default domain for the Istio server.
	DomainSuffix string
}

func (e *Environment) GetDomainSuffix() string {
	if len(e.DomainSuffix) > 0 {
		return e.DomainSuffix
	}
	return constants.DefaultKubernetesDomain
}

func (e *Environment) Mesh() *meshconfig.MeshConfig {
	if e != nil && e.Watcher != nil {
		return e.Watcher.Mesh()
	}
	return nil
}

// GetDiscoveryAddress parses the DiscoveryAddress specified via MeshConfig.
func (e *Environment) GetDiscoveryAddress() (host.Name, string, error) {
	proxyConfig := mesh.DefaultProxyConfig()
	if e.Mesh().DefaultConfig != nil {
		proxyConfig = *e.Mesh().DefaultConfig
	}
	hostname, port, err := net.SplitHostPort(proxyConfig.DiscoveryAddress)
	if err != nil {
		return "", "", fmt.Errorf("invalid Istiod Address: %s, %v", proxyConfig.DiscoveryAddress, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("invalid Istiod Port: %s, %s, %v", port, proxyConfig.DiscoveryAddress, err)
	}
	return host.Name(hostname), port, nil
}

func (e *Environment) AddMeshHandler(h func()) {
	if e != nil && e.Watcher != nil {
		e.Watcher.AddMeshHandler(h)
	}
}

func (e *Environment) Networks() *meshconfig.MeshNetworks {
	if e != nil && e.NetworksWatcher != nil {
		return e.NetworksWatcher.Networks()
	}
	return nil
}

func (e *Environment) AddNetworksHandler(h func()) {
	if e != nil && e.NetworksWatcher != nil {
		e.NetworksWatcher.AddNetworksHandler(h)
	}
}

func (e *Environment) AddMetric(metric monitoring.Metric, key string, proxy *Proxy, msg string) {
	if e != nil && e.PushContext != nil {
		e.PushContext.AddMetric(metric, key, proxy, msg)
	}
}

// Request is an alias for array of marshaled resources.
type Resources = []*any.Any

// XdsUpdates include information about the subset of updated resources.
// See for example EDS incremental updates.
type XdsUpdates = map[ConfigKey]struct{}

// XdsResourceGenerator creates the response for a typeURL DiscoveryRequest. If no generator is associated
// with a Proxy, the default (a networking.core.ConfigGenerator instance) will be used.
// The server may associate a different generator based on client metadata. Different
// WatchedResources may use same or different Generator.
type XdsResourceGenerator interface {
	Generate(proxy *Proxy, push *PushContext, w *WatchedResource, updates XdsUpdates) Resources
}

// Proxy contains information about an specific instance of a proxy (envoy sidecar, gateway,
// etc). The Proxy is initialized when a sidecar connects to Pilot, and populated from
// 'node' info in the protocol as well as data extracted from registries.
//
// In current Istio implementation nodes use a 4-parts '~' delimited ID.
// Type~IPAddress~ID~Domain
type Proxy struct {

	// Type specifies the node type. First part of the ID.
	Type NodeType

	// IPAddresses is the IP addresses of the proxy used to identify it and its
	// co-located service instances. Example: "10.60.1.6". In some cases, the host
	// where the poxy and service instances reside may have more than one IP address
	IPAddresses []string

	// ID is the unique platform-specific sidecar proxy ID. For k8s it is the pod ID and
	// namespace.
	ID string

	// Locality is the location of where Envoy proxy runs. This is extracted from
	// the registry where possible. If the registry doesn't provide a locality for the
	// proxy it will use the one sent via ADS that can be configured in the Envoy bootstrap
	Locality *core.Locality

	// DNSDomain defines the DNS domain suffix for short hostnames (e.g.
	// "default.svc.cluster.local")
	DNSDomain string

	// ConfigNamespace defines the namespace where this proxy resides
	// for the purposes of network scoping.
	// NOTE: DO NOT USE THIS FIELD TO CONSTRUCT DNS NAMES
	ConfigNamespace string

	// Metadata key-value pairs extending the Node identifier
	Metadata *NodeMetadata

	// the sidecarScope associated with the proxy
	SidecarScope *SidecarScope

	// the sidecarScope associated with the proxy previously
	PrevSidecarScope *SidecarScope

	// The merged gateways associated with the proxy if this is a Router
	MergedGateway *MergedGateway

	// service instances associated with the proxy
	ServiceInstances []*ServiceInstance

	// Istio version associated with the Proxy
	IstioVersion *IstioVersion

	// Indicates wheteher proxy supports IPv6 addresses
	ipv6Support bool

	// Indicates wheteher proxy supports IPv4 addresses
	ipv4Support bool

	// GlobalUnicastIP stores the globacl unicast IP if available, otherwise nil
	GlobalUnicastIP string

	// XdsResourceGenerator is used to generate resources for the node, based on the PushContext.
	// If nil, the default networking/core v2 generator is used. This field can be set
	// at connect time, based on node metadata, to trigger generation of a different style
	// of configuration.
	XdsResourceGenerator XdsResourceGenerator

	// Active contains the list of watched resources for the proxy, keyed by the DiscoveryRequest short type.
	Active map[string]*WatchedResource

	// ActiveExperimental contains the list of watched resources for the proxy, keyed by the canonical DiscoveryRequest type.
	// Note that the key may not be equal to the proper TypeUrl. For example, Envoy types like Cluster will share a single
	// key for multiple versions.
	ActiveExperimental map[string]*WatchedResource

	// Envoy may request different versions of configuration (XDS v2 vs v3). While internally Pilot will
	// only generate one version or the other, because the protos are wire compatible we can cast to the
	// requested version. This struct keeps track of the types requested for each resource type.
	// For example, if Envoy requests Clusters v3, we would track that here. Pilot would generate a v2
	// cluster response, but change the TypeUrl in the response to be v3.
	RequestedTypes struct {
		CDS string
		EDS string
		RDS string
		LDS string
	}
}

// WatchedResource tracks an active DiscoveryRequest subscription.
type WatchedResource struct {
	// TypeUrl is copied from the DiscoveryRequest.TypeUrl that initiated watching this resource.
	// nolint
	TypeUrl string

	// ResourceNames tracks the list of resources that are actively watched. If empty, all resources of the
	// TypeUrl type are watched.
	// For endpoints the resource names will have list of clusters and for clusters it is empty.
	ResourceNames []string

	// VersionSent is the version of the resource included in the last sent response.
	// It corresponds to the [Cluster/Route/Listener]VersionSent in the XDS package.
	VersionSent string

	// NonceSent is the nonce sent in the last sent response. If it is equal with NonceAcked, the
	// last message has been processed. If empty: we never sent a message of this type.
	NonceSent string

	// VersionAcked represents the version that was applied successfully. It can be different from
	// VersionSent: if NonceSent == NonceAcked and versions are different it means the client rejected
	// the last version, and VersionAcked is the last accepted and active config.
	// If empty it means the client has no accepted/valid version, and is not ready.
	VersionAcked string

	// NonceAcked is the last acked message.
	NonceAcked string

	// LastSent tracks the time of the generated push, to determine the time it takes the client to ack.
	LastSent time.Time

	// Updates count the number of generated updates for the resource
	Updates int

	// LastSize tracks the size of the last update
	LastSize int

	// Last request contains the last DiscoveryRequest received for
	// this type. Generators are called immediately after each request,
	// and may use the information in DiscoveryRequest.
	// Note that Envoy may send multiple requests for the same type, for
	// example to update the set of watched resources or to ACK/NACK.
	LastRequest *discovery.DiscoveryRequest
}

var (
	istioVersionRegexp = regexp.MustCompile(`^([1-9]+)\.([0-9]+)(\.([0-9]+))?`)
)

// StringList is a list that will be marshaled to a comma separate string in Json
type StringList []string

func (l StringList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return nil, nil
	}
	return []byte(`"` + strings.Join(l, ",") + `"`), nil
}

func (l *StringList) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == `""` {
		*l = []string{}
	} else {
		*l = strings.Split(string(data[1:len(data)-1]), ",")
	}
	return nil
}

// PodPort describes a mapping of port name to port number. Generally, this is just the definition of
// a port in Kubernetes, but without depending on Kubernetes api.
type PodPort struct {
	// If specified, this must be an IANA_SVC_NAME and unique within the pod. Each
	// named port in a pod must have a unique name. Name for the port that can be
	// referred to by services.
	// +optional
	Name string `json:"name,omitempty"`
	// Number of port to expose on the pod's IP address.
	// This must be a valid port number, 0 < x < 65536.
	ContainerPort int `json:"containerPort"`
	// Name of the protocol
	Protocol string `json:"protocol"`
}

// PodPortList defines a list of PodPort's that is serialized as a string
// This is for legacy reasons, where proper JSON was not supported and was written as a string
type PodPortList []PodPort

func (l PodPortList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return nil, nil
	}
	b, err := json.Marshal([]PodPort(l))
	if err != nil {
		return nil, err
	}
	b = bytes.ReplaceAll(b, []byte{'"'}, []byte{'\\', '"'})
	out := append([]byte{'"'}, b...)
	out = append(out, '"')
	return out, nil
}

func (l *PodPortList) UnmarshalJSON(data []byte) error {
	var pl []PodPort
	pls, err := strconv.Unquote(string(data))
	if err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(pls), &pl); err != nil {
		return err
	}
	*l = pl
	return nil
}

// StringBool defines a boolean that is serialized as a string for legacy reasons
type StringBool bool

func (s StringBool) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%t"`, s)), nil
}

func (s *StringBool) UnmarshalJSON(data []byte) error {
	pls, err := strconv.Unquote(string(data))
	if err != nil {
		return err
	}
	b, err := strconv.ParseBool(pls)
	if err != nil {
		return err
	}
	*s = StringBool(b)
	return nil
}

// ProxyConfig can only be marshaled using (gogo) jsonpb. However, the rest of node meta is not a proto
// To allow marshaling, we need to define a custom type that calls out to the gogo marshaller
type NodeMetaProxyConfig meshconfig.ProxyConfig

func (s NodeMetaProxyConfig) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	pc := meshconfig.ProxyConfig(s)
	if err := (&gogojsonpb.Marshaler{}).Marshal(&buf, &pc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *NodeMetaProxyConfig) UnmarshalJSON(data []byte) error {
	pc := (*meshconfig.ProxyConfig)(s)
	return gogojsonpb.Unmarshal(bytes.NewReader(data), pc)
}

// BootstrapNodeMetadata is a superset of NodeMetadata, intended to model the entirety of the node metadata
// we configure in the Envoy bootstrap. This is split out from NodeMetadata to explicitly segment the parameters
// that are consumed by Pilot from the parameters used only as part of the bootstrap. Fields used by bootstrap only
// are consumed by Envoy itself, such as the telemetry filters.
type BootstrapNodeMetadata struct {
	NodeMetadata

	// ExchangeKeys specifies a list of metadata keys that should be used for Node Metadata Exchange.
	ExchangeKeys StringList `json:"EXCHANGE_KEYS,omitempty"`

	// InstanceName is the short name for the workload instance (ex: pod name)
	// replaces POD_NAME
	InstanceName string `json:"NAME,omitempty"`

	// WorkloadName specifies the name of the workload represented by this node.
	WorkloadName string `json:"WORKLOAD_NAME,omitempty"`

	// Owner specifies the workload owner (opaque string). Typically, this is the owning controller of
	// of the workload instance (ex: k8s deployment for a k8s pod).
	Owner string `json:"OWNER,omitempty"`

	// PlatformMetadata contains any platform specific metadata
	PlatformMetadata map[string]string `json:"PLATFORM_METADATA,omitempty"`

	StatsInclusionPrefixes string `json:"sidecar.istio.io/statsInclusionPrefixes,omitempty"`
	StatsInclusionRegexps  string `json:"sidecar.istio.io/statsInclusionRegexps,omitempty"`
	StatsInclusionSuffixes string `json:"sidecar.istio.io/statsInclusionSuffixes,omitempty"`
	ExtraStatTags          string `json:"sidecar.istio.io/extraStatTags,omitempty"`
}

// NodeMetadata defines the metadata associated with a proxy
// Fields should not be assumed to exist on the proxy, especially newly added fields which will not exist
// on older versions.
// The JSON field names should never change, as they are needed for backward compatibility with older proxies
// nolint: maligned
type NodeMetadata struct {
	// ProxyConfig defines the proxy config specified for a proxy.
	// Note that this setting may be configured different for each proxy, due user overrides
	// or from different versions of proxies connecting. While Pilot has access to the meshConfig.defaultConfig,
	// this field should be preferred if it is present.
	ProxyConfig *NodeMetaProxyConfig `json:"PROXY_CONFIG,omitempty"`

	// IstioVersion specifies the Istio version associated with the proxy
	IstioVersion string `json:"ISTIO_VERSION,omitempty"`

	// Labels specifies the set of workload instance (ex: k8s pod) labels associated with this node.
	Labels map[string]string `json:"LABELS,omitempty"`

	// InstanceIPs is the set of IPs attached to this proxy
	InstanceIPs StringList `json:"INSTANCE_IPS,omitempty"`

	// Namespace is the namespace in which the workload instance is running.
	Namespace string `json:"NAMESPACE,omitempty"`

	// InterceptionMode is the name of the metadata variable that carries info about
	// traffic interception mode at the proxy
	InterceptionMode TrafficInterceptionMode `json:"INTERCEPTION_MODE,omitempty"`

	// ServiceAccount specifies the service account which is running the workload.
	ServiceAccount string `json:"SERVICE_ACCOUNT,omitempty"`

	// RouterMode indicates whether the proxy is functioning as a SNI-DNAT router
	// processing the AUTO_PASSTHROUGH gateway servers
	RouterMode string `json:"ROUTER_MODE,omitempty"`

	// MeshID specifies the mesh ID environment variable.
	MeshID string `json:"MESH_ID,omitempty"`

	// ClusterID defines the cluster the node belongs to.
	ClusterID string `json:"CLUSTER_ID,omitempty"`

	// Network defines the network the node belongs to. It is an optional metadata,
	// set at injection time. When set, the Endpoints returned to a note and not on same network
	// will be replaced with the gateway defined in the settings.
	Network string `json:"NETWORK,omitempty"`

	// RequestedNetworkView specifies the networks that the proxy wants to see
	RequestedNetworkView StringList `json:"REQUESTED_NETWORK_VIEW,omitempty"`

	// PodPorts defines the ports on a pod. This is used to lookup named ports.
	PodPorts PodPortList `json:"POD_PORTS,omitempty"`

	PolicyCheck                  string `json:"policy.istio.io/check,omitempty"`
	PolicyCheckRetries           string `json:"policy.istio.io/checkRetries,omitempty"`
	PolicyCheckBaseRetryWaitTime string `json:"policy.istio.io/checkBaseRetryWaitTime,omitempty"`
	PolicyCheckMaxRetryWaitTime  string `json:"policy.istio.io/checkMaxRetryWaitTime,omitempty"`

	// TLSServerCertChain is the absolute path to server cert-chain file
	TLSServerCertChain string `json:"TLS_SERVER_CERT_CHAIN,omitempty"`
	// TLSServerKey is the absolute path to server private key file
	TLSServerKey string `json:"TLS_SERVER_KEY,omitempty"`
	// TLSServerRootCert is the absolute path to server root cert file
	TLSServerRootCert string `json:"TLS_SERVER_ROOT_CERT,omitempty"`
	// TLSClientCertChain is the absolute path to client cert-chain file
	TLSClientCertChain string `json:"TLS_CLIENT_CERT_CHAIN,omitempty"`
	// TLSClientKey is the absolute path to client private key file
	TLSClientKey string `json:"TLS_CLIENT_KEY,omitempty"`
	// TLSClientRootCert is the absolute path to client root cert file
	TLSClientRootCert string `json:"TLS_CLIENT_ROOT_CERT,omitempty"`

	CertBaseDir string `json:"BASE,omitempty"`
	// SdsEnabled indicates if SDS is enabled or not. This is are set to "1" if true
	SdsEnabled StringBool `json:"SDS,omitempty"`

	// StsPort specifies the port of security token exchange server (STS).
	// Used by envoy filters
	StsPort string `json:"STS_PORT,omitempty"`

	// IdleTimeout specifies the idle timeout for the proxy, in duration format (10s).
	// If not set, no timeout is set.
	IdleTimeout string `json:"IDLE_TIMEOUT,omitempty"`

	// HTTP10 indicates the application behind the sidecar is making outbound http requests with HTTP/1.0
	// protocol. It will enable the "AcceptHttp_10" option on the http options for outbound HTTP listeners.
	// Alpha in 1.1, based on feedback may be turned into an API or change. Set to "1" to enable.
	HTTP10 string `json:"HTTP10,omitempty"`

	// Generator indicates the client wants to use a custom Generator plugin.
	Generator string `json:"GENERATOR,omitempty"`

	// DNSCapture indicates whether the workload has enabled dns capture
	DNSCapture string `json:"DNS_CAPTURE,omitempty"`

	// Contains a copy of the raw metadata. This is needed to lookup arbitrary values.
	// If a value is known ahead of time it should be added to the struct rather than reading from here,
	Raw map[string]interface{} `json:"-"`
}

// ProxyConfigOrDefault is a helper function to get the ProxyConfig from metadata, or fallback to a default
// This is useful as the logic should check for proxy config from proxy first and then defer to mesh wide defaults
// if not present.
func (m NodeMetadata) ProxyConfigOrDefault(def *meshconfig.ProxyConfig) *meshconfig.ProxyConfig {
	if m.ProxyConfig != nil {
		return (*meshconfig.ProxyConfig)(m.ProxyConfig)
	}
	return def
}

func (m *BootstrapNodeMetadata) UnmarshalJSON(data []byte) error {
	// Create a new type from the target type to avoid recursion.
	type BootstrapNodeMetadata2 BootstrapNodeMetadata

	t2 := &BootstrapNodeMetadata2{}
	if err := json.Unmarshal(data, t2); err != nil {
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = BootstrapNodeMetadata(*t2)
	m.Raw = raw

	return nil
}

// Converts this to a protobuf structure. This should be used only for debugging - performance is bad.
func (m NodeMetadata) ToStruct() *structpb.Struct {
	j, err := json.Marshal(m)
	if err != nil {
		return nil
	}

	pbs := &structpb.Struct{}
	if err := jsonpb.Unmarshal(bytes.NewBuffer(j), pbs); err != nil {
		return nil
	}

	return pbs
}

// IstioVersion encodes the Istio version of the proxy. This is a low key way to
// do semver style comparisons and generate the appropriate envoy config
type IstioVersion struct {
	Major int
	Minor int
	Patch int
}

var (
	MaxIstioVersion = &IstioVersion{Major: 65535, Minor: 65535, Patch: 65535}
)

// Compare returns -1/0/1 if version is less than, equal or greater than inv
// To compare only on major, call this function with { X, -1, -1}.
// to compare only on major & minor, call this function with {X, Y, -1}.
func (pversion *IstioVersion) Compare(inv *IstioVersion) int {
	// check major
	if r := compareVersion(pversion.Major, inv.Major); r != 0 {
		return r
	}

	// check minor
	if inv.Minor > -1 {
		if r := compareVersion(pversion.Minor, inv.Minor); r != 0 {
			return r
		}

		// check patch
		if inv.Patch > -1 {
			if r := compareVersion(pversion.Patch, inv.Patch); r != 0 {
				return r
			}
		}
	}
	return 0
}

func compareVersion(ov, nv int) int {
	if ov == nv {
		return 0
	}
	if ov < nv {
		return -1
	}
	return 1
}

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// SidecarProxy type is used for sidecar proxies in the application containers
	SidecarProxy NodeType = "sidecar"

	// Router type is used for standalone proxies acting as L7/L4 routers
	Router NodeType = "router"
)

// IsApplicationNodeType verifies that the NodeType is one of the declared constants in the model
func IsApplicationNodeType(nType NodeType) bool {
	switch nType {
	case SidecarProxy, Router:
		return true
	default:
		return false
	}
}

// ServiceNode encodes the proxy node attributes into a URI-acceptable string
func (node *Proxy) ServiceNode() string {
	ip := ""
	if len(node.IPAddresses) > 0 {
		ip = node.IPAddresses[0]
	}
	return strings.Join([]string{
		string(node.Type), ip, node.ID, node.DNSDomain,
	}, serviceNodeSeparator)

}

// RouterMode decides the behavior of Istio Gateway (normal or sni-dnat)
type RouterMode string

const (
	// StandardRouter is the normal gateway mode
	StandardRouter RouterMode = "standard"

	// SniDnatRouter is used for bridging two networks
	SniDnatRouter RouterMode = "sni-dnat"
)

// GetRouterMode returns the operating mode associated with the router.
// Assumes that the proxy is of type Router
func (node *Proxy) GetRouterMode() RouterMode {
	if RouterMode(node.Metadata.RouterMode) == SniDnatRouter {
		return SniDnatRouter
	}
	return StandardRouter
}

// SetSidecarScope identifies the sidecar scope object associated with this
// proxy and updates the proxy Node. This is a convenience hack so that
// callers can simply call push.Services(node) while the implementation of
// push.Services can return the set of services from the proxyNode's
// sidecar scope or from the push context's set of global services. Similar
// logic applies to push.VirtualServices and push.DestinationRule. The
// short cut here is useful only for CDS and parts of RDS generation code.
//
// Listener generation code will still use the SidecarScope object directly
// as it needs the set of services for each listener port.
func (node *Proxy) SetSidecarScope(ps *PushContext) {
	sidecarScope := node.SidecarScope

	if node.Type == SidecarProxy {
		workloadLabels := labels.Collection{node.Metadata.Labels}
		node.SidecarScope = ps.getSidecarScope(node, workloadLabels)
	} else {
		// Gateways should just have a default scope with egress: */*
		node.SidecarScope = DefaultSidecarScopeForNamespace(ps, node.ConfigNamespace)
	}
	node.PrevSidecarScope = sidecarScope
}

// SetGatewaysForProxy merges the Gateway objects associated with this
// proxy and caches the merged object in the proxy Node. This is a convenience hack so that
// callers can simply call push.MergedGateways(node) instead of having to
// fetch all the gateways and invoke the merge call in multiple places (lds/rds).
func (node *Proxy) SetGatewaysForProxy(ps *PushContext) {
	if node.Type != Router {
		return
	}
	node.MergedGateway = ps.mergeGateways(node)
}

func (node *Proxy) SetServiceInstances(serviceDiscovery ServiceDiscovery) error {
	instances, err := serviceDiscovery.GetProxyServiceInstances(node)
	if err != nil {
		log.Errorf("failed to get service proxy service instances: %v", err)
		return err
	}

	// Keep service instances in order of creation/hostname.
	sort.SliceStable(instances, func(i, j int) bool {
		if instances[i].Service != nil && instances[j].Service != nil {
			if !instances[i].Service.CreationTime.Equal(instances[j].Service.CreationTime) {
				return instances[i].Service.CreationTime.Before(instances[j].Service.CreationTime)
			}
			// Additionally, sort by hostname just in case services created automatically at the same second.
			return instances[i].Service.Hostname < instances[j].Service.Hostname
		}
		return true
	})

	node.ServiceInstances = instances
	return nil
}

// SetWorkloadLabels will set the node.Metadata.Labels only when it is nil.
func (node *Proxy) SetWorkloadLabels(env *Environment) error {
	// First get the workload labels from node meta
	if len(node.Metadata.Labels) > 0 {
		return nil
	}

	// Fallback to calling GetProxyWorkloadLabels
	l, err := env.GetProxyWorkloadLabels(node)
	if err != nil {
		log.Errorf("failed to get service proxy labels: %v", err)
		return err
	}
	if len(l) > 0 {
		node.Metadata.Labels = l[0]
	}
	return nil
}

// DiscoverIPVersions discovers the IP Versions supported by Proxy based on its IP addresses.
func (node *Proxy) DiscoverIPVersions() {
	for i := 0; i < len(node.IPAddresses); i++ {
		addr := net.ParseIP(node.IPAddresses[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.IsGlobalUnicast() {
			node.GlobalUnicastIP = addr.String()
		}
		if addr.To4() != nil {
			node.ipv4Support = true
		} else {
			node.ipv6Support = true
		}
	}
}

// SupportsIPv4 returns true if proxy supports IPv4 addresses.
func (node *Proxy) SupportsIPv4() bool {
	return node.ipv4Support
}

// SupportsIPv6 returns true if proxy supports IPv6 addresses.
func (node *Proxy) SupportsIPv6() bool {
	return node.ipv6Support
}

// UnnamedNetwork is the default network that proxies in the mesh
// get when they don't request a specific network view.
const UnnamedNetwork = ""

// GetNetworkView returns the networks that the proxy requested.
// When sending EDS/CDS-with-dns-endpoints, Pilot will only send
// endpoints corresponding to the networks that the proxy wants to see.
// If not set, we assume that the proxy wants to see endpoints in any network.
func GetNetworkView(node *Proxy) map[string]bool {
	if node == nil || len(node.Metadata.RequestedNetworkView) == 0 {
		return nil
	}

	nmap := make(map[string]bool)
	for _, n := range node.Metadata.RequestedNetworkView {
		nmap[n] = true
	}
	nmap[UnnamedNetwork] = true

	return nmap
}

// ParseMetadata parses the opaque Metadata from an Envoy Node into string key-value pairs.
// Any non-string values are ignored.
func ParseMetadata(metadata *structpb.Struct) (*NodeMetadata, error) {
	if metadata == nil {
		return &NodeMetadata{}, nil
	}

	buf := &bytes.Buffer{}
	if err := (&jsonpb.Marshaler{OrigName: true}).Marshal(buf, metadata); err != nil {
		return nil, fmt.Errorf("failed to read node metadata %v: %v", metadata, err)
	}
	meta := &BootstrapNodeMetadata{}
	if err := json.Unmarshal(buf.Bytes(), meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node metadata (%v): %v", buf.String(), err)
	}
	return &meta.NodeMetadata, nil
}

// ParseServiceNodeWithMetadata parse the Envoy Node from the string generated by ServiceNode
// function and the metadata.
func ParseServiceNodeWithMetadata(s string, metadata *NodeMetadata) (*Proxy, error) {
	parts := strings.Split(s, serviceNodeSeparator)
	out := &Proxy{
		Metadata: metadata,
	}

	if len(parts) != 4 {
		return out, fmt.Errorf("missing parts in the service node %q", s)
	}

	if !IsApplicationNodeType(NodeType(parts[0])) {
		return out, fmt.Errorf("invalid node type (valid types: sidecar, router in the service node %q", s)
	}
	out.Type = NodeType(parts[0])

	// Get all IP Addresses from Metadata
	if hasValidIPAddresses(metadata.InstanceIPs) {
		out.IPAddresses = metadata.InstanceIPs
	} else if isValidIPAddress(parts[1]) {
		//Fall back, use IP from node id, it's only for backward-compatibility, IP should come from metadata
		out.IPAddresses = append(out.IPAddresses, parts[1])
	}

	// Does query from ingress or router have to carry valid IP address?
	if len(out.IPAddresses) == 0 {
		return out, fmt.Errorf("no valid IP address in the service node id or metadata")
	}

	out.ID = parts[2]
	out.DNSDomain = parts[3]
	if len(metadata.IstioVersion) == 0 {
		log.Warnf("Istio Version is not found in metadata for %v, which may have undesirable side effects", out.ID)
	}
	out.IstioVersion = ParseIstioVersion(metadata.IstioVersion)
	return out, nil
}

// ParseIstioVersion parses a version string and returns IstioVersion struct
func ParseIstioVersion(ver string) *IstioVersion {
	// strip the release- prefix if any and extract the version string
	ver = istioVersionRegexp.FindString(strings.TrimPrefix(ver, "release-"))

	if ver == "" {
		// return very large values assuming latest version
		return MaxIstioVersion
	}

	parts := strings.Split(ver, ".")
	// we are guaranteed to have atleast major and minor based on the regex
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch := 0
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return &IstioVersion{Major: major, Minor: minor, Patch: patch}
}

// GetOrDefault returns either the value, or the default if the value is empty. Useful when retrieving node metadata fields.
func GetOrDefault(s string, def string) string {
	if len(s) > 0 {
		return s
	}
	return def
}

// GetProxyConfigNamespace extracts the namespace associated with the proxy
// from the proxy metadata or the proxy ID
func GetProxyConfigNamespace(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}

	// First look for ISTIO_META_CONFIG_NAMESPACE
	// All newer proxies (from Istio 1.1 onwards) are supposed to supply this
	if len(proxy.Metadata.Namespace) > 0 {
		return proxy.Metadata.Namespace
	}

	// if not found, for backward compatibility, extract the namespace from
	// the proxy domain. this is a k8s specific hack and should be enabled
	parts := strings.Split(proxy.DNSDomain, ".")
	if len(parts) > 1 { // k8s will have namespace.<domain>
		return parts[0]
	}

	return ""
}

const (
	serviceNodeSeparator = "~"
)

// ParsePort extracts port number from a valid proxy address
func ParsePort(addr string) int {
	port, err := strconv.Atoi(addr[strings.Index(addr, ":")+1:])
	if err != nil {
		log.Warna(err)
	}

	return port
}

// hasValidIPAddresses returns true if the input ips are all valid, otherwise returns false.
func hasValidIPAddresses(ipAddresses []string) bool {
	if len(ipAddresses) == 0 {
		return false
	}
	for _, ipAddress := range ipAddresses {
		if !isValidIPAddress(ipAddress) {
			return false
		}
	}
	return true
}

// Tell whether the given IP address is valid or not
func isValidIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

// TrafficInterceptionMode indicates how traffic to/from the workload is captured and
// sent to Envoy. This should not be confused with the CaptureMode in the API that indicates
// how the user wants traffic to be intercepted for the listener. TrafficInterceptionMode is
// always derived from the Proxy metadata
type TrafficInterceptionMode string

const (
	// InterceptionNone indicates that the workload is not using IPtables for traffic interception
	InterceptionNone TrafficInterceptionMode = "NONE"

	// InterceptionTproxy implies traffic intercepted by IPtables with TPROXY mode
	InterceptionTproxy TrafficInterceptionMode = "TPROXY"

	// InterceptionRedirect implies traffic intercepted by IPtables with REDIRECT mode
	// This is our default mode
	InterceptionRedirect TrafficInterceptionMode = "REDIRECT"
)

// GetInterceptionMode extracts the interception mode associated with the proxy
// from the proxy metadata
func (node *Proxy) GetInterceptionMode() TrafficInterceptionMode {
	if node == nil {
		return InterceptionRedirect
	}

	switch node.Metadata.InterceptionMode {
	case "TPROXY":
		return InterceptionTproxy
	case "REDIRECT":
		return InterceptionRedirect
	case "NONE":
		return InterceptionNone
	}

	return InterceptionRedirect
}

// SidecarDNSListenerPort specifes the port at which the sidecar hosts a DNS resolver listener.
// TODO: customize me. tools/istio-iptables package also has this hardcoded.
const SidecarDNSListenerPort = 15013
