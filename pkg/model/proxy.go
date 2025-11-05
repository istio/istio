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
	"strconv"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/types/known/structpb"

	apilabel "istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry/util/label"
	networkutil "istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/util/protomarshal"
)

// StringList is a list that will be marshaled to a comma separate string in Json
type StringList []string

func (l StringList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return nil, nil
	}
	return json.Marshal(strings.Join(l, ","))
}

func (l *StringList) UnmarshalJSON(data []byte) error {
	var inner string
	err := json.Unmarshal(data, &inner)
	if err != nil {
		return err
	}
	if len(inner) == 0 {
		*l = []string{}
	} else {
		*l = strings.Split(inner, ",")
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
		return err
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

func (s *NodeMetaProxyConfig) MarshalJSON() ([]byte, error) {
	pc := (*meshconfig.ProxyConfig)(s)
	return protomarshal.Marshal(pc)
}

func (s *NodeMetaProxyConfig) UnmarshalJSON(data []byte) error {
	pc := (*meshconfig.ProxyConfig)(s)
	return protomarshal.UnmarshalAllowUnknown(data, pc)
}

// Node is a typed version of Envoy node with metadata.
type Node struct {
	// ID of the Envoy node
	ID string
	// Metadata is the typed node metadata
	Metadata *BootstrapNodeMetadata
	// RawMetadata is the untyped node metadata
	RawMetadata map[string]any
	// Locality from Envoy bootstrap
	Locality *core.Locality
}

// BootstrapNodeMetadata is a superset of NodeMetadata, intended to model the entirety of the node metadata
// we configure in the Envoy bootstrap. This is split out from NodeMetadata to explicitly segment the parameters
// that are consumed by Pilot from the parameters used only as part of the bootstrap. Fields used by bootstrap only
// are consumed by Envoy itself, such as the telemetry filters.
type BootstrapNodeMetadata struct {
	NodeMetadata

	// InstanceName is the short name for the workload instance (ex: pod name)
	// replaces POD_NAME
	InstanceName string `json:"NAME,omitempty"`

	// Owner specifies the workload owner (opaque string). Typically, this is the owning controller of
	// of the workload instance (ex: k8s deployment for a k8s pod).
	Owner string `json:"OWNER,omitempty"`

	// PilotSAN is the list of subject alternate names for the xDS server.
	PilotSubjectAltName []string `json:"PILOT_SAN,omitempty"`

	// XDSRootCert defines the root cert to use for XDS connections
	XDSRootCert string `json:"-"`

	// OutlierLogPath is the cluster manager outlier event log path.
	OutlierLogPath string `json:"OUTLIER_LOG_PATH,omitempty"`

	// AppContainers is the list of containers in the pod.
	AppContainers string `json:"APP_CONTAINERS,omitempty"`

	// IstioProxySHA is the SHA of the proxy version.
	IstioProxySHA string `json:"ISTIO_PROXY_SHA,omitempty"`
}

// TrafficInterceptionMode indicates how traffic to/from the workload is captured and
// sent to Envoy. This should not be confused with the CaptureMode in the API that indicates
// how the user wants traffic to be intercepted for the listener. TrafficInterceptionMode is
// always derived from the Proxy metadata
type TrafficInterceptionMode string

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

	// IstioRevision specifies the Istio revision associated with the proxy.
	// Mostly used when istiod requests the upstream.
	IstioRevision string `json:"ISTIO_REVISION,omitempty"`

	// Labels specifies the set of workload instance (ex: k8s pod) labels associated with this node.
	// It contains both StaticLabels and pod labels if any, it is a superset of StaticLabels.
	// Note: it is not meant to be used during xds generation.
	Labels map[string]string `json:"LABELS,omitempty"`

	// StaticLabels specifies the set of labels from `ISTIO_METAJSON_LABELS`.
	StaticLabels map[string]string `json:"STATIC_LABELS,omitempty"`

	// Annotations specifies the set of workload instance (ex: k8s pod) annotations associated with this node.
	Annotations map[string]string `json:"ANNOTATIONS,omitempty"`

	// InstanceIPs is the set of IPs attached to this proxy
	InstanceIPs StringList `json:"INSTANCE_IPS,omitempty"`

	// Namespace is the namespace in which the workload instance is running.
	Namespace string `json:"NAMESPACE,omitempty"`

	// NodeName is the name of the kubernetes node on which the workload instance is running.
	NodeName string `json:"NODE_NAME,omitempty"`

	// WorkloadName specifies the name of the workload represented by this node.
	WorkloadName string `json:"WORKLOAD_NAME,omitempty"`

	// InterceptionMode is the name of the metadata variable that carries info about
	// traffic interception mode at the proxy
	InterceptionMode TrafficInterceptionMode `json:"INTERCEPTION_MODE,omitempty"`

	// ServiceAccount specifies the service account which is running the workload.
	ServiceAccount string `json:"SERVICE_ACCOUNT,omitempty"`

	// HTTPProxyPort enables http proxy on the port for the current sidecar.
	// Same as MeshConfig.HttpProxyPort, but with per/sidecar scope.
	HTTPProxyPort string `json:"HTTP_PROXY_PORT,omitempty"`

	// MeshID specifies the mesh ID environment variable.
	MeshID string `json:"MESH_ID,omitempty"`

	// ClusterID defines the cluster the node belongs to.
	ClusterID cluster.ID `json:"CLUSTER_ID,omitempty"`

	// Network defines the network the node belongs to. It is an optional metadata,
	// set at injection time. When set, the Endpoints returned to a node and not on same network
	// will be replaced with the gateway defined in the settings.
	Network network.ID `json:"NETWORK,omitempty"`

	// RequestedNetworkView specifies the networks that the proxy wants to see
	RequestedNetworkView StringList `json:"REQUESTED_NETWORK_VIEW,omitempty"`

	// PodPorts defines the ports on a pod. This is used to lookup named ports.
	PodPorts PodPortList `json:"POD_PORTS,omitempty"`

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

	// IdleTimeout specifies the idle timeout for the proxy, in duration format (10s).
	// If not set, default timeout is 1 hour.
	IdleTimeout string `json:"IDLE_TIMEOUT,omitempty"`

	// HTTP10 indicates the application behind the sidecar is making outbound http requests with HTTP/1.0
	// protocol. It will enable the "AcceptHttp_10" option on the http options for outbound HTTP listeners.
	// Alpha in 1.1, based on feedback may be turned into an API or change. Set to "1" to enable.
	HTTP10 string `json:"HTTP10,omitempty"`

	// Generator indicates the client wants to use a custom Generator plugin.
	Generator string `json:"GENERATOR,omitempty"`

	// DNSCapture indicates whether the workload has enabled dns capture
	DNSCapture StringBool `json:"DNS_CAPTURE,omitempty"`

	// DNSAutoAllocate indicates whether the workload should have auto allocated addresses for ServiceEntry
	// This allows resolving ServiceEntries, which is especially useful for distinguishing TCP traffic
	// This depends on DNSCapture.
	DNSAutoAllocate StringBool `json:"DNS_AUTO_ALLOCATE,omitempty"`

	// EnableHBONE, if set, will enable generation of HBONE listener config.
	// Note: this only impacts sidecars and gateways; ztunnel and waypoint proxy unconditionally use HBONE.
	EnableHBONE StringBool `json:"ENABLE_HBONE,omitempty"`

	// DisableHBONESend, will disable sending HBONE.
	// Warning: If this is enabled, ambient may break; use with caution.
	DisableHBONESend StringBool `json:"DISABLE_HBONE_SEND,omitempty"`

	// AutoRegister will enable auto registration of the connected endpoint to the service registry using the given WorkloadGroup name
	AutoRegisterGroup string `json:"AUTO_REGISTER_GROUP,omitempty"`

	// WorkloadEntry specifies the name of the WorkloadEntry this proxy corresponds to.
	//
	// This field is intended for use in those scenarios where a user needs to
	// onboard a workload from a VM without relying on auto-registration.
	//
	// At runtime, when a proxy establishes an ADS connection to the istiod,
	// istiod will treat a non-empty value of this field as an indicator
	// that proxy corresponds to a VM and must be represented by a WorkloadEntry
	// with a given name.
	WorkloadEntry string `json:"WORKLOAD_ENTRY,omitempty"`

	// UnprivilegedPod is used to determine whether a Gateway Pod can open ports < 1024
	UnprivilegedPod string `json:"UNPRIVILEGED_POD,omitempty"`

	// PlatformMetadata contains any platform specific metadata
	PlatformMetadata map[string]string `json:"PLATFORM_METADATA,omitempty"`

	// StsPort specifies the port of security token exchange server (STS).
	// Used by envoy filters
	StsPort string `json:"STS_PORT,omitempty"`

	// Envoy status port redirecting to agent status port.
	EnvoyStatusPort int `json:"ENVOY_STATUS_PORT,omitempty"`

	// Envoy prometheus port redirecting to admin port prometheus endpoint.
	EnvoyPrometheusPort int `json:"ENVOY_PROMETHEUS_PORT,omitempty"`

	// ExitOnZeroActiveConnections terminates Envoy if there are no active connections if set.
	ExitOnZeroActiveConnections StringBool `json:"EXIT_ON_ZERO_ACTIVE_CONNECTIONS,omitempty"`

	// InboundListenerExactBalance sets connection balance config to use exact_balance for virtualInbound,
	// as long as QUIC, since it uses UDP, isn't also used.
	InboundListenerExactBalance StringBool `json:"INBOUND_LISTENER_EXACT_BALANCE,omitempty"`

	// OutboundListenerExactBalance sets connection balance config to use exact_balance for outbound
	// redirected tcp listeners. This does not change the virtualOutbound listener.
	OutboundListenerExactBalance StringBool `json:"OUTBOUND_LISTENER_EXACT_BALANCE,omitempty"`

	// The istiod address when running ASM Managed Control Plane.
	CloudrunAddr string `json:"CLOUDRUN_ADDR,omitempty"`

	// Metadata discovery service enablement
	MetadataDiscovery *StringBool `json:"METADATA_DISCOVERY,omitempty"`

	// Envoy command line to option to control deprecates=d logs behavior.
	EnvoySkipDeprecatedLogs StringBool `json:"ENVOY_SKIP_DEPRECATED_LOGS,omitempty"`

	// Name of the socket file which will be used for workload SDS.
	WorkloadIdentitySocketFile string `json:"WORKLOAD_IDENTITY_SOCKET_FILE,omitempty"`

	// Contains a copy of the raw metadata. This is needed to lookup arbitrary values.
	// If a value is known ahead of time it should be added to the struct rather than reading from here,
	Raw map[string]any `json:"-"`
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
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = BootstrapNodeMetadata(*t2)
	m.Raw = raw

	return nil
}

// ToStruct converts NodeMetadata to a protobuf structure. This should be used only for debugging - performance is bad.
func (m NodeMetadata) ToStruct() *structpb.Struct {
	j, err := json.Marshal(m)
	if err != nil {
		return nil
	}

	pbs := &structpb.Struct{}
	if err := protomarshal.Unmarshal(j, pbs); err != nil {
		return nil
	}

	return pbs
}

// GetOrDefault returns either the value, or the default if the value is empty. Useful when retrieving node metadata fields.
func GetOrDefault(s string, def string) string {
	if len(s) > 0 {
		return s
	}
	return def
}

const (
	// LocalityLabel indicates the region/zone/subzone of an instance. It is used to override the native
	// registry's value.
	//
	// Note: because k8s labels does not support `/`, so we use `.` instead in k8s.
	LocalityLabel = "istio-locality"
	// k8s istio-locality label separator
	k8sSeparator = "."
)

func GetLocalityLabel(labels map[string]string) string {
	// "topology.istio.io/locality" take first
	if loc, ok := labels[apilabel.TopologyLocality.Name]; ok {
		return loc
	}

	// check the old label(istio-locality)
	if loc, ok := labels[LocalityLabel]; ok {
		return loc
	}

	return ""
}

// SanitizeLocalityLabel returns the locality from the supplied label. Because Kubernetes
// labels don't support `/`, we replace "." with "/" in the supplied label as a workaround.
func SanitizeLocalityLabel(label string) string {
	if len(label) > 0 {
		// if there are /'s present we don't need to replace
		if strings.Contains(label, "/") {
			return label
		}
		// replace "." with "/"
		return strings.Replace(label, k8sSeparator, "/", -1)
	}
	return ""
}

// ConvertLocality converts '/' separated locality string to Locality struct.
func ConvertLocality(locality string) *core.Locality {
	if locality == "" {
		return &core.Locality{}
	}

	region, zone, subzone := label.SplitLocalityLabel(locality)
	return &core.Locality{
		Region:  region,
		Zone:    zone,
		SubZone: subzone,
	}
}

// ALPNH2Only advertises that Proxy is going to use HTTP/2 when talking to the cluster.
var ALPNH2Only = []string{"h2"}

// ALPNInMeshH2 advertises that Proxy is going to use HTTP/2 when talking to the in-mesh cluster.
// The custom "istio" value indicates in-mesh traffic and it's going to be used for routing decisions.
// Once Envoy supports client-side ALPN negotiation, this should be {"istio", "h2", "http/1.1"}.
var ALPNInMeshH2 = []string{"istio", "h2"}

func StringToExactMatch(in []string) []*matcher.StringMatcher {
	if len(in) == 0 {
		return nil
	}
	res := make([]*matcher.StringMatcher, 0, len(in))
	for _, s := range in {
		res = append(res, &matcher.StringMatcher{
			MatchPattern: &matcher.StringMatcher_Exact{Exact: s},
		})
	}
	return res
}

const (
	// IstioCanonicalServiceLabelName is the name of label for the Istio Canonical Service for a workload instance.
	IstioCanonicalServiceLabelName = "service.istio.io/canonical-name"

	// IstioCanonicalServiceRevisionLabelName is the name of label for the Istio Canonical Service revision for a workload instance.
	IstioCanonicalServiceRevisionLabelName = "service.istio.io/canonical-revision"
)

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// SidecarProxy type is used for sidecar proxies in the application containers
	SidecarProxy NodeType = "sidecar"

	// Router type is used for standalone proxies acting as L7/L4 routers
	Router NodeType = "router"

	// Waypoint type is used for waypoint proxies
	Waypoint NodeType = "waypoint"

	// Ztunnel type is used for node proxies (ztunnel)
	Ztunnel NodeType = "ztunnel"
)

// IsApplicationNodeType verifies that the NodeType is one of the declared constants in the model
func IsApplicationNodeType(nType NodeType) bool {
	switch nType {
	case SidecarProxy, Router, Waypoint, Ztunnel:
		return true
	default:
		return false
	}
}

// IPMode represents the IP mode of proxy.
type IPMode int

// IPMode constants starting with index 1.
const (
	IPv4 IPMode = iota + 1
	IPv6
	Dual
)

func DiscoverIPMode(addrs []string) IPMode {
	if networkutil.AllIPv4(addrs) {
		return IPv4
	} else if networkutil.AllIPv6(addrs) {
		return IPv6
	}
	return Dual
}
