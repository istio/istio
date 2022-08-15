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
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/jsonpb"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/trustbundle"
	networkutil "istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/identifier"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/ledger"
	"istio.io/pkg/monitoring"
)

var _ mesh.Holder = &Environment{}

func NewEnvironment() *Environment {
	return &Environment{
		PushContext:   NewPushContext(),
		EndpointIndex: NewEndpointIndex(),
	}
}

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Config interface for listing routing rules
	ConfigStore

	// Watcher is the watcher for the mesh config (to be merged into the config store)
	mesh.Watcher

	// NetworksWatcher (loaded from a config map) provides information about the
	// set of networks inside a mesh and how to route to endpoints in each
	// network. Each network provides information about the endpoints in a
	// routable L3 network. A single routable L3 network can have one or more
	// service registries.
	NetworksWatcher mesh.NetworksWatcher

	NetworkManager *NetworkManager

	// PushContext holds information during push generation. It is reset on config change, at the beginning
	// of the pushAll. It will hold all errors and stats and possibly caches needed during the entire cache computation.
	// DO NOT USE EXCEPT FOR TESTS AND HANDLING OF NEW CONNECTIONS.
	// ALL USE DURING A PUSH SHOULD USE THE ONE CREATED AT THE
	// START OF THE PUSH, THE GLOBAL ONE MAY CHANGE AND REFLECT A DIFFERENT
	// CONFIG AND PUSH
	PushContext *PushContext

	// DomainSuffix provides a default domain for the Istio server.
	DomainSuffix string

	ledger ledger.Ledger

	// TrustBundle: List of Mesh TrustAnchors
	TrustBundle *trustbundle.TrustBundle

	clusterLocalServices ClusterLocalProvider

	GatewayAPIController GatewayController

	// EndpointShards for a service. This is a global (per-server) list, built from
	// incremental updates. This is keyed by service and namespace
	EndpointIndex *EndpointIndex
}

func (e *Environment) Mesh() *meshconfig.MeshConfig {
	if e != nil && e.Watcher != nil {
		return e.Watcher.Mesh()
	}
	return nil
}

func (e *Environment) MeshNetworks() *meshconfig.MeshNetworks {
	if e != nil && e.NetworksWatcher != nil {
		return e.NetworksWatcher.Networks()
	}
	return nil
}

// GetDiscoveryAddress parses the DiscoveryAddress specified via MeshConfig.
func (e *Environment) GetDiscoveryAddress() (host.Name, string, error) {
	proxyConfig := mesh.DefaultProxyConfig()
	if e.Mesh().DefaultConfig != nil {
		proxyConfig = e.Mesh().DefaultConfig
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

func (e *Environment) AddNetworksHandler(h func()) {
	if e != nil && e.NetworksWatcher != nil {
		e.NetworksWatcher.AddNetworksHandler(h)
	}
}

func (e *Environment) AddMetric(metric monitoring.Metric, key string, proxyID, msg string) {
	if e != nil && e.PushContext != nil {
		e.PushContext.AddMetric(metric, key, proxyID, msg)
	}
}

func (e *Environment) Version() string {
	if x := e.GetLedger(); x != nil {
		return x.RootHash()
	}
	return ""
}

// Init initializes the Environment for use.
func (e *Environment) Init() {
	// Use a default DomainSuffix, if none was provided.
	if len(e.DomainSuffix) == 0 {
		e.DomainSuffix = constants.DefaultClusterLocalDomain
	}

	// Create the cluster-local service registry.
	e.clusterLocalServices = NewClusterLocalProvider(e)
}

func (e *Environment) InitNetworksManager(updater XDSUpdater) (err error) {
	e.NetworkManager, err = NewNetworkManager(e, updater)
	return
}

func (e *Environment) ClusterLocal() ClusterLocalProvider {
	return e.clusterLocalServices
}

func (e *Environment) GetLedger() ledger.Ledger {
	return e.ledger
}

func (e *Environment) SetLedger(l ledger.Ledger) {
	e.ledger = l
}

// Resources is an alias for array of marshaled resources.
type Resources = []*discovery.Resource

// DeletedResources is an alias for array of strings that represent removed resources in delta.
type DeletedResources = []string

func AnyToUnnamedResources(r []*anypb.Any) Resources {
	a := make(Resources, 0, len(r))
	for _, rr := range r {
		a = append(a, &discovery.Resource{Resource: rr})
	}
	return a
}

func ResourcesToAny(r Resources) []*anypb.Any {
	a := make([]*anypb.Any, 0, len(r))
	for _, rr := range r {
		a = append(a, rr.Resource)
	}
	return a
}

// XdsUpdates include information about the subset of updated resources.
// See for example EDS incremental updates.
type XdsUpdates = map[ConfigKey]struct{}

// XdsLogDetails contains additional metadata that is captured by Generators and used by xds processors
// like Ads and Delta to uniformly log.
type XdsLogDetails struct {
	Incremental    bool
	AdditionalInfo string
}

var DefaultXdsLogDetails = XdsLogDetails{}

// XdsResourceGenerator creates the response for a typeURL DiscoveryRequest or DeltaDiscoveryRequest. If no generator
// is associated with a Proxy, the default (a networking.core.ConfigGenerator instance) will be used.
// The server may associate a different generator based on client metadata. Different
// WatchedResources may use same or different Generator.
// Note: any errors returned will completely close the XDS stream. Use with caution; typically and empty
// or no response is preferred.
type XdsResourceGenerator interface {
	// Generate generates the Sotw resources for Xds.
	Generate(proxy *Proxy, w *WatchedResource, req *PushRequest) (Resources, XdsLogDetails, error)
}

// XdsDeltaResourceGenerator generates Sotw and delta resources.
type XdsDeltaResourceGenerator interface {
	XdsResourceGenerator
	// GenerateDeltas returns the changed and removed resources, along with whether or not delta was actually used.
	GenerateDeltas(proxy *Proxy, req *PushRequest, w *WatchedResource) (Resources, DeletedResources, XdsLogDetails, bool, error)
}

// Proxy contains information about an specific instance of a proxy (envoy sidecar, gateway,
// etc). The Proxy is initialized when a sidecar connects to Pilot, and populated from
// 'node' info in the protocol as well as data extracted from registries.
//
// In current Istio implementation nodes use a 4-parts '~' delimited ID.
// Type~IPAddress~ID~Domain
type Proxy struct {
	sync.RWMutex

	// Type specifies the node type. First part of the ID.
	Type NodeType

	// IPAddresses is the IP addresses of the proxy used to identify it and its
	// co-located service instances. Example: "10.60.1.6". In some cases, the host
	// where the proxy and service instances reside may have more than one IP address
	IPAddresses []string

	// ID is the unique platform-specific sidecar proxy ID. For k8s it is the pod ID and
	// namespace <podName.namespace>.
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

	// Labels specifies the set of workload instance (ex: k8s pod) labels associated with this node.
	// Labels can be different from that in Metadata because of pod labels update after startup,
	// while NodeMetadata.Labels are set during bootstrap.
	Labels map[string]string

	// IstioMetaLabels contains the labels specified by ISTIO_METAJSON_LABELS and platform instance,
	// so we can support pod labels update by plus the fundamental labels.
	IstioMetaLabels map[string]string

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

	// VerifiedIdentity determines whether a proxy had its identity verified. This
	// generally occurs by JWT or mTLS authentication. This can be false when
	// connecting over plaintext. If this is set to true, we can verify the proxy has
	// access to ConfigNamespace namespace. However, other options such as node type
	// are not part of an Istio identity and thus are not verified.
	VerifiedIdentity *spiffe.Identity

	// IPMode of proxy.
	ipMode IPMode

	// GlobalUnicastIP stores the global unicast IP if available, otherwise nil
	GlobalUnicastIP string

	// XdsResourceGenerator is used to generate resources for the node, based on the PushContext.
	// If nil, the default networking/core v2 generator is used. This field can be set
	// at connect time, based on node metadata, to trigger generation of a different style
	// of configuration.
	XdsResourceGenerator XdsResourceGenerator

	// WatchedResources contains the list of watched resources for the proxy, keyed by the DiscoveryRequest TypeUrl.
	WatchedResources map[string]*WatchedResource

	// XdsNode is the xDS node identifier
	XdsNode *core.Node

	AutoregisteredWorkloadEntryName string

	// LastPushContext stores the most recent push context for this proxy. This will be monotonically
	// increasing in version. Requests should send config based on this context; not the global latest.
	// Historically, the latest was used which can cause problems when computing whether a push is
	// required, as the computed sidecar scope version would not monotonically increase.
	LastPushContext *PushContext
	// LastPushTime records the time of the last push. This is used in conjunction with
	// LastPushContext; the XDS cache depends on knowing the time of the PushContext to determine if a
	// key is stale or not.
	LastPushTime time.Time
}

// WatchedResource tracks an active DiscoveryRequest subscription.
type WatchedResource struct {
	// TypeUrl is copied from the DiscoveryRequest.TypeUrl that initiated watching this resource.
	// nolint
	TypeUrl string

	// ResourceNames tracks the list of resources that are actively watched.
	// For LDS and CDS, all resources of the TypeUrl type are watched if it is empty.
	// For endpoints the resource names will have list of clusters and for clusters it is empty.
	// For Delta Xds, all resources of the TypeUrl that a client has subscribed to.
	ResourceNames []string

	// NonceSent is the nonce sent in the last sent response. If it is equal with NonceAcked, the
	// last message has been processed. If empty: we never sent a message of this type.
	NonceSent string

	// NonceAcked is the last acked message.
	NonceAcked string

	// AlwaysRespond, if true, will ensure that even when a request would otherwise be treated as an
	// ACK, it will be responded to. This typically happens when a proxy reconnects to another instance of
	// Istiod. In that case, Envoy expects us to respond to EDS/RDS/SDS requests to finish warming of
	// clusters/listeners.
	// Typically, this should be set to 'false' after response; keeping it true would likely result in an endless loop.
	AlwaysRespond bool

	// LastResources tracks the contents of the last push.
	// This field is extremely expensive to maintain and is typically disabled
	LastResources Resources
}

var istioVersionRegexp = regexp.MustCompile(`^([1-9]+)\.([0-9]+)(\.([0-9]+))?`)

// StringList is a list that will be marshaled to a comma separate string in Json
type StringList []string

func (l StringList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return nil, nil
	}
	return []byte(`"` + strings.Join(l, ",") + `"`), nil
}

func (l *StringList) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || string(data) == `""` {
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
	var buf bytes.Buffer
	pc := (*meshconfig.ProxyConfig)(s)
	if err := (&jsonpb.Marshaler{}).Marshal(&buf, pc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *NodeMetaProxyConfig) UnmarshalJSON(data []byte) error {
	pc := (*meshconfig.ProxyConfig)(s)
	return jsonpb.Unmarshal(bytes.NewReader(data), pc)
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

	// WorkloadName specifies the name of the workload represented by this node.
	WorkloadName string `json:"WORKLOAD_NAME,omitempty"`

	// Owner specifies the workload owner (opaque string). Typically, this is the owning controller of
	// of the workload instance (ex: k8s deployment for a k8s pod).
	Owner string `json:"OWNER,omitempty"`

	// PilotSAN is the list of subject alternate names for the xDS server.
	PilotSubjectAltName []string `json:"PILOT_SAN,omitempty"`

	// XDSRootCert defines the root cert to use for XDS connections
	XDSRootCert string `json:"-"`

	// OutlierLogPath is the cluster manager outlier event log path.
	OutlierLogPath string `json:"OUTLIER_LOG_PATH,omitempty"`

	// ProvCertDir is the directory containing pre-provisioned certs.
	ProvCert string `json:"PROV_CERT,omitempty"`

	// AppContainers is the list of containers in the pod.
	AppContainers string `json:"APP_CONTAINERS,omitempty"`

	// IstioProxySHA is the SHA of the proxy version.
	IstioProxySHA string `json:"ISTIO_PROXY_SHA,omitempty"`
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

	// IstioRevision specifies the Istio revision associated with the proxy.
	// Mostly used when istiod requests the upstream.
	IstioRevision string `json:"ISTIO_REVISION,omitempty"`

	// Labels specifies the set of workload instance (ex: k8s pod) labels associated with this node. Labels is a
	// superset of IstioMetaLabels.
	Labels map[string]string `json:"LABELS,omitempty"`

	// Annotations specifies the set of workload instance (ex: k8s pod) annotations associated with this node.
	Annotations map[string]string `json:"ANNOTATIONS,omitempty"`

	// InstanceIPs is the set of IPs attached to this proxy
	InstanceIPs StringList `json:"INSTANCE_IPS,omitempty"`

	// Namespace is the namespace in which the workload instance is running.
	Namespace string `json:"NAMESPACE,omitempty"`

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

	// AutoRegister will enable auto registration of the connected endpoint to the service registry using the given WorkloadGroup name
	AutoRegisterGroup string `json:"AUTO_REGISTER_GROUP,omitempty"`

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

// GetView returns a restricted view of the mesh for this proxy. The view can be
// restricted by network (via ISTIO_META_REQUESTED_NETWORK_VIEW).
// If not set, we assume that the proxy wants to see endpoints in any network.
func (node *Proxy) GetView() ProxyView {
	return newProxyView(node)
}

// InNetwork returns true if the proxy is on the given network, or if either
// the proxy's network or the given network is unspecified ("").
func (node *Proxy) InNetwork(network network.ID) bool {
	return node == nil || identifier.IsSameOrEmpty(network.String(), node.Metadata.Network.String())
}

// InCluster returns true if the proxy is in the given cluster, or if either
// the proxy's cluster id or the given cluster id is unspecified ("").
func (node *Proxy) InCluster(cluster cluster.ID) bool {
	return node == nil || identifier.IsSameOrEmpty(cluster.String(), node.Metadata.ClusterID.String())
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

// IstioVersion encodes the Istio version of the proxy. This is a low key way to
// do semver style comparisons and generate the appropriate envoy config
type IstioVersion struct {
	Major int
	Minor int
	Patch int
}

var MaxIstioVersion = &IstioVersion{Major: 65535, Minor: 65535, Patch: 65535}

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

var NodeTypes = [...]NodeType{SidecarProxy, Router}

// IPMode represents the IP mode of proxy.
type IPMode int

// IPMode constants starting with index 1.
const (
	IPv4 IPMode = iota + 1
	IPv6
	Dual
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
		node.SidecarScope = ps.getSidecarScope(node, node.Labels)
	} else {
		// Gateways should just have a default scope with egress: */*
		node.SidecarScope = ps.getSidecarScope(node, nil)
	}
	node.PrevSidecarScope = sidecarScope
}

// SetGatewaysForProxy merges the Gateway objects associated with this
// proxy and caches the merged object in the proxy Node. This is a convenience hack so that
// callers can simply call push.MergedGateways(node) instead of having to
// fetch all the gateways and invoke the merge call in multiple places (lds/rds).
// Must be called after ServiceInstances are set
func (node *Proxy) SetGatewaysForProxy(ps *PushContext) {
	if node.Type != Router {
		return
	}
	node.MergedGateway = ps.mergeGateways(node)
}

func (node *Proxy) SetServiceInstances(serviceDiscovery ServiceDiscovery) {
	instances := serviceDiscovery.GetProxyServiceInstances(node)

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
}

// SetWorkloadLabels will set the node.Labels.
// It merges both node meta labels and workload labels and give preference to workload labels.
func (node *Proxy) SetWorkloadLabels(env *Environment) {
	labels := env.GetProxyWorkloadLabels(node)
	// when IstioMetaLabels unset, calculate the IstioMetaLabels. It is only set once.
	if node.IstioMetaLabels == nil {
		IstioMetaLabels := make(map[string]string, len(node.Metadata.Labels))
		for k, v := range node.Metadata.Labels {
			IstioMetaLabels[k] = v
		}
		for k := range labels {
			delete(IstioMetaLabels, k)
		}
		node.IstioMetaLabels = IstioMetaLabels
	}

	node.Labels = make(map[string]string, len(labels)+len(node.IstioMetaLabels))
	// we can't just equate proxy workload labels to node meta labels as it may be customized by user
	// with `ISTIO_METAJSON_LABELS` env (pkg/bootstrap/config.go extractAttributesMetadata).
	// so, we fill the `ISTIO_METAJSON_LABELS` as well.
	for k, v := range node.IstioMetaLabels {
		node.Labels[k] = v
	}
	for k, v := range labels {
		node.Labels[k] = v
	}
}

// DiscoverIPMode discovers the IP Versions supported by Proxy based on its IP addresses.
func (node *Proxy) DiscoverIPMode() {
	if networkutil.AllIPv4(node.IPAddresses) {
		node.ipMode = IPv4
	} else if networkutil.AllIPv6(node.IPAddresses) {
		node.ipMode = IPv6
	} else {
		node.ipMode = Dual
	}
	node.GlobalUnicastIP = networkutil.GlobalUnicastIP(node.IPAddresses)
}

// SupportsIPv4 returns true if proxy supports IPv4 addresses.
func (node *Proxy) SupportsIPv4() bool {
	return node.ipMode == IPv4 || node.ipMode == Dual
}

// SupportsIPv6 returns true if proxy supports IPv6 addresses.
func (node *Proxy) SupportsIPv6() bool {
	return node.ipMode == IPv6 || node.ipMode == Dual
}

// IsIPv6 returns true if proxy only supports IPv6 addresses.
func (node *Proxy) IsIPv6() bool {
	return node.ipMode == IPv6
}

// ParseMetadata parses the opaque Metadata from an Envoy Node into string key-value pairs.
// Any non-string values are ignored.
func ParseMetadata(metadata *structpb.Struct) (*NodeMetadata, error) {
	if metadata == nil {
		return &NodeMetadata{}, nil
	}

	boostrapNodeMeta, err := ParseBootstrapNodeMetadata(metadata)
	if err != nil {
		return nil, err
	}
	return &boostrapNodeMeta.NodeMetadata, nil
}

// ParseBootstrapNodeMetadata parses the opaque Metadata from an Envoy Node into string key-value pairs.
func ParseBootstrapNodeMetadata(metadata *structpb.Struct) (*BootstrapNodeMetadata, error) {
	if metadata == nil {
		return &BootstrapNodeMetadata{}, nil
	}

	b, err := protomarshal.MarshalProtoNames(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to read node metadata %v: %v", metadata, err)
	}
	meta := &BootstrapNodeMetadata{}
	if err := json.Unmarshal(b, meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node metadata (%v): %v", string(b), err)
	}
	return meta, nil
}

// ParseServiceNodeWithMetadata parse the Envoy Node from the string generated by ServiceNode
// function and the metadata.
func ParseServiceNodeWithMetadata(nodeID string, metadata *NodeMetadata) (*Proxy, error) {
	parts := strings.Split(nodeID, serviceNodeSeparator)
	out := &Proxy{
		Metadata: metadata,
	}

	if len(parts) != 4 {
		return out, fmt.Errorf("missing parts in the service node %q", nodeID)
	}

	if !IsApplicationNodeType(NodeType(parts[0])) {
		return out, fmt.Errorf("invalid node type (valid types: sidecar, router in the service node %q", nodeID)
	}
	out.Type = NodeType(parts[0])

	// Get all IP Addresses from Metadata
	if hasValidIPAddresses(metadata.InstanceIPs) {
		out.IPAddresses = metadata.InstanceIPs
	} else if isValidIPAddress(parts[1]) {
		// Fall back, use IP from node id, it's only for backward-compatibility, IP should come from metadata
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
	// Assume very large patch release if not set
	patch := 65535
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
	_, sPort, err := net.SplitHostPort(addr)
	if sPort == "" {
		return 0
	}
	if err != nil {
		log.Warn(err)
	}
	port, pErr := strconv.Atoi(sPort)
	if pErr != nil {
		log.Warn(pErr)
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

// IsUnprivileged returns true if the proxy has explicitly indicated that it is
// unprivileged, i.e. it cannot bind to the privileged ports 1-1023.
func (node *Proxy) IsUnprivileged() bool {
	if node == nil || node.Metadata == nil {
		return false
	}
	// expect explicit "true" value
	unprivileged, _ := strconv.ParseBool(node.Metadata.UnprivilegedPod)
	return unprivileged
}

// CanBindToPort returns true if the proxy can bind to a given port.
func (node *Proxy) CanBindToPort(bindTo bool, port uint32) bool {
	if bindTo && IsPrivilegedPort(port) && node.IsUnprivileged() {
		return false
	}
	return true
}

// IsPrivilegedPort returns true if a given port is in the range 1-1023.
func IsPrivilegedPort(port uint32) bool {
	// check for 0 is important because:
	// 1) technically, 0 is not a privileged port; any process can ask to bind to 0
	// 2) this function will be receiving 0 on input in the case of UDS listeners
	return 0 < port && port < 1024
}

func (node *Proxy) IsVM() bool {
	// TODO use node metadata to indicate that this is a VM intstead of the TestVMLabel
	return node.Labels[constants.TestVMLabel] != ""
}

func (node *Proxy) IsProxylessGrpc() bool {
	return node.Metadata != nil && node.Metadata.Generator == "grpc"
}

func (node *Proxy) FuzzValidate() bool {
	if node.Metadata == nil {
		return false
	}
	return len(node.IPAddresses) != 0
}

type GatewayController interface {
	ConfigStoreController
	// Reconcile updates the internal state of the gateway controller for a given input. This should be
	// called before any List/Get calls if the state has changed
	Reconcile(ctx *PushContext) error
	// SecretAllowed determines if a SDS credential is accessible to a given namespace.
	// For example, for resourceName of `kubernetes-gateway://ns-name/secret-name` and namespace of `ingress-ns`,
	// this would return true only if there was a policy allowing `ingress-ns` to access Secrets in the `ns-name` namespace.
	SecretAllowed(resourceName string, namespace string) bool
}

// OutboundListenerClass is a helper to turn a NodeType for outbound to a ListenerClass.
func OutboundListenerClass(t NodeType) istionetworking.ListenerClass {
	if t == Router {
		return istionetworking.ListenerClassGateway
	}
	return istionetworking.ListenerClassSidecarOutbound
}
