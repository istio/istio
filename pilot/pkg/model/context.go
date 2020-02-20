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

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/jsonpb"
	structpb "github.com/golang/protobuf/ptypes/struct"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/monitoring"

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
}

func (e *Environment) Mesh() *meshconfig.MeshConfig {
	if e != nil && e.Watcher != nil {
		return e.Watcher.Mesh()
	}
	return nil
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

// Proxy contains information about an specific instance of a proxy (envoy sidecar, gateway,
// etc). The Proxy is initialized when a sidecar connects to Pilot, and populated from
// 'node' info in the protocol as well as data extracted from registries.
//
// In current Istio implementation nodes use a 4-parts '~' delimited ID.
// Type~IPAddress~ID~Domain
type Proxy struct {
	// ClusterID specifies the cluster where the proxy resides.
	// TODO: clarify if this is needed in the new 'network' model, likely needs to
	// be renamed to 'network'
	ClusterID string

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

	// The merged gateways associated with the proxy if this is a Router
	MergedGateway *MergedGateway

	// service instances associated with the proxy
	ServiceInstances []*ServiceInstance

	// labels associated with the workload
	WorkloadLabels labels.Collection

	// Istio version associated with the Proxy
	IstioVersion *IstioVersion
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

// NodeMetadata defines the metadata associated with a proxy
// Fields should not be assumed to exist on the proxy, especially newly added fields which will not exist
// on older versions.
// The JSON field names should never change, as they are needed for backward compatibility with older proxies
// nolint: maligned
type NodeMetadata struct {
	// IstioVersion specifies the Istio version associated with the proxy
	IstioVersion string `json:"ISTIO_VERSION,omitempty"`

	// Labels specifies the set of workload instance (ex: k8s pod) labels associated with this node.
	Labels map[string]string `json:"LABELS,omitempty"`

	// InstanceIPs is the set of IPs attached to this proxy
	InstanceIPs StringList `json:"INSTANCE_IPS,omitempty"`

	// ConfigNamespace is the name of the metadata variable that carries info about
	// the config namespace associated with the proxy
	ConfigNamespace string `json:"CONFIG_NAMESPACE,omitempty"`

	// Namespace is the namespace in which the workload instance is running.
	Namespace string `json:"NAMESPACE,omitempty"` // replaces CONFIG_NAMESPACE

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

	// ExchangeKeys specifies a list of metadata keys that should be used for Node Metadata Exchange.
	ExchangeKeys StringList `json:"EXCHANGE_KEYS,omitempty"`

	// PlatformMetadata contains any platform specific metadata
	PlatformMetadata map[string]string `json:"PLATFORM_METADATA,omitempty"`

	// InstanceName is the short name for the workload instance (ex: pod name)
	// replaces POD_NAME
	InstanceName string `json:"NAME,omitempty"`

	// WorkloadName specifies the name of the workload represented by this node.
	WorkloadName string `json:"WORKLOAD_NAME,omitempty"`

	// Owner specifies the workload owner (opaque string). Typically, this is the owning controller of
	// of the workload instance (ex: k8s deployment for a k8s pod).
	Owner string `json:"OWNER,omitempty"`

	// PodPorts defines the ports on a pod. This is used to lookup named ports.
	PodPorts PodPortList `json:"POD_PORTS,omitempty"`

	// CanonicalTelemetryService specifies the service name to use for all node telemetry.
	CanonicalTelemetryService string `json:"CANONICAL_TELEMETRY_SERVICE,omitempty"`

	// LocalityLabel defines the locality specified for the pod
	LocalityLabel string `json:"istio-locality,omitempty"`

	PolicyCheck                  string `json:"policy.istio.io/check,omitempty"`
	PolicyCheckRetries           string `json:"policy.istio.io/checkRetries,omitempty"`
	PolicyCheckBaseRetryWaitTime string `json:"policy.istio.io/checkBaseRetryWaitTime,omitempty"`
	PolicyCheckMaxRetryWaitTime  string `json:"policy.istio.io/checkMaxRetryWaitTime,omitempty"`

	StatsInclusionPrefixes string `json:"sidecar.istio.io/statsInclusionPrefixes,omitempty"`
	StatsInclusionRegexps  string `json:"sidecar.istio.io/statsInclusionRegexps,omitempty"`
	StatsInclusionSuffixes string `json:"sidecar.istio.io/statsInclusionSuffixes,omitempty"`
	ExtraStatTags          string `json:"sidecar.istio.io/extraStatTags,omitempty"`

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

	// SdsTokenPath specifies the path of the SDS token used by the Envoy proxy.
	// If not set, Pilot uses the default SDS token path.
	SdsTokenPath string     `json:"SDS_TOKEN_PATH,omitempty"`
	UserSds      StringBool `json:"USER_SDS,omitempty"`
	SdsBase      string     `json:"BASE,omitempty"`
	// SdsEnabled indicates if SDS is enabled or not. This is are set to "1" if true
	SdsEnabled StringBool `json:"SDS,omitempty"`
	// SdsTrustJwt indicates if SDS trust jwt is enabled or not. This is are set to "1" if true
	SdsTrustJwt StringBool `json:"TRUSTJWT,omitempty"`

	// StsPort specifies the port of security token exchange server (STS).
	StsPort string `json:"STS_PORT,omitempty"`

	InsecurePath string `json:"istio.io/insecurepath,omitempty"`

	// IdleTimeout specifies the idle timeout for the proxy, in duration format (10s).
	// If not set, no timeout is set.
	IdleTimeout string `json:"IDLE_TIMEOUT,omitempty"`

	// HTTP10 indicates the application behind the sidecar is making outbound http requests with HTTP/1.0
	// protocol. It will enable the "AcceptHttp_10" option on the http options for outbound HTTP listeners.
	// Alpha in 1.1, based on feedback may be turned into an API or change. Set to "1" to enable.
	HTTP10 string `json:"HTTP10,omitempty"`

	// Contains a copy of the raw metadata. This is needed to lookup arbitrary values.
	// If a value is known ahead of time it should be added to the struct rather than reading from here,
	Raw map[string]interface{} `json:"-"`
}

func (m *NodeMetadata) UnmarshalJSON(data []byte) error {
	// Create a new type from the target type to avoid recursion.
	type NodeMetadata2 NodeMetadata

	t2 := &NodeMetadata2{}
	if err := json.Unmarshal(data, t2); err != nil {
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = NodeMetadata(*t2)
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

	// AllPortsLiteral is the string value indicating all ports
	AllPortsLiteral = "*"
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
	if node.Type == SidecarProxy {
		node.SidecarScope = ps.getSidecarScope(node, node.WorkloadLabels)
	} else {
		// Gateways should just have a default scope with egress: */*
		node.SidecarScope = DefaultSidecarScopeForNamespace(ps, node.ConfigNamespace)
	}

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

// SetWorkloadLabels will reset the proxy.WorkloadLabels if `force` = true,
// otherwise only set it when it is nil.
func (node *Proxy) SetWorkloadLabels(env *Environment) error {
	// The WorkloadLabels is already parsed from Node metadata["LABELS"]
	if node.WorkloadLabels != nil {
		return nil
	}

	// TODO: remove WorkloadLabels and use node.Metadata.Labels directly
	// First get the workload labels from node meta
	if len(node.Metadata.Labels) > 0 {
		node.WorkloadLabels = labels.Collection{node.Metadata.Labels}
		return nil
	}

	// Fallback to calling GetProxyWorkloadLabels
	l, err := env.GetProxyWorkloadLabels(node)
	if err != nil {
		log.Errorf("failed to get service proxy labels: %v", err)
		return err
	}

	node.WorkloadLabels = l
	return nil
}

// UnnamedNetwork is the default network that proxies in the mesh
// get when they don't request a specific network view.
const UnnamedNetwork = ""

// GetNetworkView returns the networks that the proxy requested.
// When sending EDS/CDS-with-dns-endpoints, Pilot will only send
// endpoints corresponding to the networks that the proxy wants to see.
// If not set, we assume that the proxy wants to see endpoints from the default
// unnamed network.
func GetNetworkView(node *Proxy) map[string]bool {
	if node == nil {
		return map[string]bool{UnnamedNetwork: true}
	}

	nmap := make(map[string]bool)
	for _, n := range node.Metadata.RequestedNetworkView {
		nmap[n] = true
	}

	if len(nmap) == 0 {
		// Proxy sees endpoints from the default unnamed network only
		nmap[UnnamedNetwork] = true
	}
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
	meta := &NodeMetadata{}
	if err := json.Unmarshal(buf.Bytes(), meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node metadata (%v): %v", buf.String(), err)
	}
	return meta, nil
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
	if len(metadata.Labels) > 0 {
		out.WorkloadLabels = labels.Collection{metadata.Labels}
	}
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
	if len(proxy.Metadata.ConfigNamespace) > 0 {
		return proxy.Metadata.ConfigNamespace
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

// Pile all node metadata constants here
const (
	// NodeMetadataTLSServerCertChain is the absolute path to server cert-chain file
	NodeMetadataTLSServerCertChain = "TLS_SERVER_CERT_CHAIN"

	// NodeMetadataTLSServerKey is the absolute path to server private key file
	NodeMetadataTLSServerKey = "TLS_SERVER_KEY"

	// NodeMetadataTLSServerRootCert is the absolute path to server root cert file
	NodeMetadataTLSServerRootCert = "TLS_SERVER_ROOT_CERT"

	// NodeMetadataTLSClientCertChain is the absolute path to client cert-chain file
	NodeMetadataTLSClientCertChain = "TLS_CLIENT_CERT_CHAIN"

	// NodeMetadataTLSClientKey is the absolute path to client private key file
	NodeMetadataTLSClientKey = "TLS_CLIENT_KEY"

	// NodeMetadataTLSClientRootCert is the absolute path to client root cert file
	NodeMetadataTLSClientRootCert = "TLS_CLIENT_ROOT_CERT"
)

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
