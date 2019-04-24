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
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Config interface for listing routing rules
	IstioConfigStore

	// Mesh is the mesh config (to be merged into the config store)
	Mesh *meshconfig.MeshConfig

	// Mixer subject alternate name for mutual TLS
	MixerSAN []string

	// PushContext holds informations during push generation. It is reset on config change, at the beginning
	// of the pushAll. It will hold all errors and stats and possibly caches needed during the entire cache computation.
	// DO NOT USE EXCEPT FOR TESTS AND HANDLING OF NEW CONNECTIONS.
	// ALL USE DURING A PUSH SHOULD USE THE ONE CREATED AT THE
	// START OF THE PUSH, THE GLOBAL ONE MAY CHANGE AND REFLECT A DIFFERENT
	// CONFIG AND PUSH
	// Deprecated - a local config for ads will be used instead
	PushContext *PushContext

	// MeshNetworks (loaded from a config map) provides information about the
	// set of networks inside a mesh and how to route to endpoints in each
	// network. Each network provides information about the endpoints in a
	// routable L3 network. A single routable L3 network can have one or more
	// service registries.
	MeshNetworks *meshconfig.MeshNetworks
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

	// Locality is the location of where Envoy proxy runs.
	Locality *core.Locality

	// DNSDomain defines the DNS domain suffix for short hostnames (e.g.
	// "default.svc.cluster.local")
	DNSDomain string

	// ConfigNamespace defines the namespace where this proxy resides
	// for the purposes of network scoping.
	// NOTE: DO NOT USE THIS FIELD TO CONSTRUCT DNS NAMES
	ConfigNamespace string

	// TrustDomain defines the trust domain of the certificate
	TrustDomain string

	// Metadata key-value pairs extending the Node identifier
	Metadata map[string]string

	// the sidecarScope associated with the proxy
	SidecarScope *SidecarScope

	// service instances associated with the proxy
	ServiceInstances []*ServiceInstance

	// labels associated with the workload
	WorkloadLabels LabelsCollection
}

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// SidecarProxy type is used for sidecar proxies in the application containers
	SidecarProxy NodeType = "sidecar"

	// Ingress type is used for cluster ingress proxies
	Ingress NodeType = "ingress"

	// Router type is used for standalone proxies acting as L7/L4 routers
	Router NodeType = "router"
)

// IsApplicationNodeType verifies that the NodeType is one of the declared constants in the model
func IsApplicationNodeType(nType NodeType) bool {
	switch nType {
	case SidecarProxy, Ingress, Router:
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

// GetProxyVersion returns the proxy version string identifier, and whether it is present.
func (node *Proxy) GetProxyVersion() (string, bool) {
	version, found := node.Metadata[NodeMetadataIstioProxyVersion]
	return version, found
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
	if modestr, found := node.Metadata[NodeMetadataRouterMode]; found {
		switch RouterMode(modestr) {
		case SniDnatRouter:
			return SniDnatRouter
		}
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
	labels := node.WorkloadLabels
	node.SidecarScope = ps.getSidecarScope(node, labels)
}

func (node *Proxy) SetServiceInstances(env *Environment) error {
	instances, err := env.GetProxyServiceInstances(node)
	if err != nil {
		log.Errorf("failed to get service proxy service instances: %v", err)
		return err
	}

	node.ServiceInstances = instances
	return nil
}

func (node *Proxy) SetWorkloadLabels(env *Environment) error {
	labels, err := env.GetProxyWorkloadLabels(node)
	if err != nil {
		log.Warnf("failed to get service proxy workload labels: %v, defaulting to proxy metadata", err)
		labels = LabelsCollection{node.Metadata}
	}

	node.WorkloadLabels = labels
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
	if networks, found := node.Metadata[NodeMetadataRequestedNetworkView]; found {
		for _, n := range strings.Split(networks, ",") {
			nmap[n] = true
		}
	} else {
		// Proxy sees endpoints from the default unnamed network only
		nmap[UnnamedNetwork] = true
	}
	return nmap
}

// ParseMetadata parses the opaque Metadata from an Envoy Node into string key-value pairs.
// Any non-string values are ignored.
func ParseMetadata(metadata *types.Struct) map[string]string {
	if metadata == nil {
		return nil
	}
	fields := metadata.GetFields()
	res := make(map[string]string, len(fields))
	for k, v := range fields {
		if s, ok := v.GetKind().(*types.Value_StringValue); ok {
			res[k] = s.StringValue
		}
	}
	if len(res) == 0 {
		res = nil
	}
	return res
}

// ParseServiceNodeWithMetadata parse the Envoy Node from the string generated by ServiceNode
// fuction and the metadata.
func ParseServiceNodeWithMetadata(s string, metadata map[string]string) (*Proxy, error) {
	parts := strings.Split(s, serviceNodeSeparator)
	out := &Proxy{
		Metadata: metadata,
	}

	if len(parts) != 4 {
		return out, fmt.Errorf("missing parts in the service node %q", s)
	}

	out.Type = NodeType(parts[0])

	switch out.Type {
	case SidecarProxy, Ingress, Router:
	default:
		return out, fmt.Errorf("invalid node type (valid types: ingress, sidecar, router in the service node %q", s)
	}

	// Get all IP Addresses from Metadata
	if ipstr, found := metadata[NodeMetadataInstanceIPs]; found {
		ipAddresses, err := parseIPAddresses(ipstr)
		if err == nil {
			out.IPAddresses = ipAddresses
		} else if isValidIPAddress(parts[1]) {
			//Fail back, use IP from node id
			out.IPAddresses = append(out.IPAddresses, parts[1])
		}
	} else if isValidIPAddress(parts[1]) {
		// Get IP from node id, it's only for backward-compatible, IP should come from metadata
		out.IPAddresses = append(out.IPAddresses, parts[1])
	}

	// Does query from ingress or router have to carry valid IP address?
	if len(out.IPAddresses) == 0 && out.Type == SidecarProxy {
		return out, fmt.Errorf("no valid IP address in the service node id or metadata")
	}

	out.ID = parts[2]
	out.DNSDomain = parts[3]
	return out, nil
}

// GetOrDefaultFromMap returns either the value found for key or the default value if the map is nil
// or does not contain the key. Useful when retrieving node metadata fields.
func GetOrDefaultFromMap(stringMap map[string]string, key, defaultVal string) string {
	if stringMap == nil {
		return defaultVal
	}
	if valFromMap, ok := stringMap[key]; ok {
		return valFromMap
	}
	return defaultVal
}

// GetProxyConfigNamespace extracts the namespace associated with the proxy
// from the proxy metadata or the proxy ID
func GetProxyConfigNamespace(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}

	// First look for ISTIO_META_CONFIG_NAMESPACE
	// All newer proxies (from Istio 1.1 onwards) are supposed to supply this
	if configNamespace, found := proxy.Metadata[NodeMetadataConfigNamespace]; found {
		return configNamespace
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

	// IngressCertsPath is the path location for ingress certificates
	IngressCertsPath = "/etc/istio/ingress-certs/"

	// AuthCertsPath is the path location for mTLS certificates
	AuthCertsPath = "/etc/certs/"

	// CertChainFilename is mTLS chain file
	CertChainFilename = "cert-chain.pem"

	// DefaultServerCertChain is the default path to the mTLS chain file
	DefaultCertChain = AuthCertsPath + CertChainFilename

	// KeyFilename is mTLS private key
	KeyFilename = "key.pem"

	// DefaultServerKey is the default path to the mTLS private key file
	DefaultKey = AuthCertsPath + KeyFilename

	// RootCertFilename is mTLS root cert
	RootCertFilename = "root-cert.pem"

	// DefaultRootCert is the default path to the mTLS root cert file
	DefaultRootCert = AuthCertsPath + RootCertFilename

	// IngressCertFilename is the ingress cert file name
	IngressCertFilename = "tls.crt"

	// IngressKeyFilename is the ingress private key file name
	IngressKeyFilename = "tls.key"

	// ConfigPathDir config directory for storing envoy json config files.
	ConfigPathDir = "/etc/istio/proxy"

	// BinaryPathFilename envoy binary location
	BinaryPathFilename = "/usr/local/bin/envoy"

	// ServiceClusterName service cluster name used in xDS calls
	ServiceClusterName = "istio-proxy"

	// DiscoveryPlainAddress discovery IP address:port with plain text
	DiscoveryPlainAddress = "istio-pilot:15010"

	// IstioIngressGatewayName is the internal gateway name assigned to ingress
	IstioIngressGatewayName = "istio-autogenerated-k8s-ingress"

	// IstioIngressNamespace is the namespace where Istio ingress controller is deployed
	IstioIngressNamespace = "istio-system"
)

// IstioIngressWorkloadLabels is the label assigned to Istio ingress pods
var IstioIngressWorkloadLabels = map[string]string{"istio": "ingress"}

// DefaultProxyConfig for individual proxies
func DefaultProxyConfig() meshconfig.ProxyConfig {
	return meshconfig.ProxyConfig{
		ConfigPath:                 ConfigPathDir,
		BinaryPath:                 BinaryPathFilename,
		ServiceCluster:             ServiceClusterName,
		DrainDuration:              types.DurationProto(45 * time.Second),
		ParentShutdownDuration:     types.DurationProto(60 * time.Second),
		DiscoveryAddress:           DiscoveryPlainAddress,
		ConnectTimeout:             types.DurationProto(1 * time.Second),
		StatsdUdpAddress:           "",
		EnvoyMetricsServiceAddress: "",
		ProxyAdminPort:             15000,
		ControlPlaneAuthPolicy:     meshconfig.AuthenticationPolicy_NONE,
		CustomConfigFile:           "",
		Concurrency:                0,
		StatNameLength:             189,
		Tracing:                    nil,
	}
}

// DefaultMeshConfig configuration
func DefaultMeshConfig() meshconfig.MeshConfig {
	config := DefaultProxyConfig()
	return meshconfig.MeshConfig{
		MixerCheckServer:                  "",
		MixerReportServer:                 "",
		DisablePolicyChecks:               true,
		PolicyCheckFailOpen:               false,
		SidecarToTelemetrySessionAffinity: false,
		RootNamespace:                     IstioSystemNamespace,
		ProxyListenPort:                   15001,
		ConnectTimeout:                    types.DurationProto(1 * time.Second),
		IngressService:                    "istio-ingressgateway",
		EnableTracing:                     true,
		AccessLogFile:                     "/dev/stdout",
		AccessLogEncoding:                 meshconfig.MeshConfig_TEXT,
		DefaultConfig:                     &config,
		SdsUdsPath:                        "",
		EnableSdsTokenMount:               false,
		TrustDomain:                       "",
		DefaultServiceExportTo:            []string{"*"},
		DefaultVirtualServiceExportTo:     []string{"*"},
		DefaultDestinationRuleExportTo:    []string{"*"},
		OutboundTrafficPolicy:             &meshconfig.MeshConfig_OutboundTrafficPolicy{Mode: meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY},
		DnsRefreshRate:                    types.DurationProto(5 * time.Second), // 5 seconds is the default refresh rate used in Envoy
	}
}

// ApplyMeshConfigDefaults returns a new MeshConfig decoded from the
// input YAML with defaults applied to omitted configuration values.
func ApplyMeshConfigDefaults(yaml string) (*meshconfig.MeshConfig, error) {
	out := DefaultMeshConfig()
	if err := ApplyYAML(yaml, &out, false); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	// Reset the default ProxyConfig as jsonpb.UnmarshalString doesn't
	// handled nested decode properly for our use case.
	prevDefaultConfig := out.DefaultConfig
	defaultProxyConfig := DefaultProxyConfig()
	out.DefaultConfig = &defaultProxyConfig

	// Re-apply defaults to ProxyConfig if they were defined in the
	// original input MeshConfig.ProxyConfig.
	if prevDefaultConfig != nil {
		origProxyConfigYAML, err := ToYAML(prevDefaultConfig)
		if err != nil {
			return nil, multierror.Prefix(err, "failed to re-encode default proxy config")
		}
		if err := ApplyYAML(origProxyConfigYAML, out.DefaultConfig, false); err != nil {
			return nil, multierror.Prefix(err, "failed to convert to proto.")
		}
	}

	if err := ValidateMeshConfig(&out); err != nil {
		return nil, err
	}

	return &out, nil
}

// EmptyMeshNetworks configuration with no networks
func EmptyMeshNetworks() meshconfig.MeshNetworks {
	return meshconfig.MeshNetworks{
		Networks: map[string]*meshconfig.Network{},
	}
}

// LoadMeshNetworksConfig returns a new MeshNetworks decoded from the
// input YAML.
func LoadMeshNetworksConfig(yaml string) (*meshconfig.MeshNetworks, error) {
	out := EmptyMeshNetworks()
	if err := ApplyYAML(yaml, &out, false); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	// TODO validate the loaded MeshNetworks
	// if err := ValidateMeshNetworks(&out); err != nil {
	// 	return nil, err
	// }
	return &out, nil
}

// ParsePort extracts port number from a valid proxy address
func ParsePort(addr string) int {
	port, err := strconv.Atoi(addr[strings.Index(addr, ":")+1:])
	if err != nil {
		log.Warna(err)
	}

	return port
}

// parseIPAddresses extracts IPs from a string
func parseIPAddresses(s string) ([]string, error) {
	ipAddresses := strings.Split(s, ",")
	if len(ipAddresses) == 0 {
		return ipAddresses, fmt.Errorf("no valid IP address")
	}
	for _, ipAddress := range ipAddresses {
		if !isValidIPAddress(ipAddress) {
			return ipAddresses, fmt.Errorf("invalid IP address %q", ipAddress)
		}
	}
	return ipAddresses, nil
}

// Tell whether the given IP address is valid or not
func isValidIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

// Pile all node metadata constants here
const (

	// NodeMetadataIstioProxyVersion specifies the Envoy version associated with the proxy
	NodeMetadataIstioProxyVersion = "ISTIO_PROXY_VERSION"

	// NodeMetadataNetwork defines the network the node belongs to. It is an optional metadata,
	// set at injection time. When set, the Endpoints returned to a note and not on same network
	// will be replaced with the gateway defined in the settings.
	NodeMetadataNetwork = "NETWORK"

	// NodeMetadataInterceptionMode is the name of the metadata variable that carries info about
	// traffic interception mode at the proxy
	NodeMetadataInterceptionMode = "INTERCEPTION_MODE"

	// NodeMetadataHTTP10 indicates the application behind the sidecar is making outbound http requests with HTTP/1.0
	// protocol. It will enable the "AcceptHttp_10" option on the http options for outbound HTTP listeners.
	// Alpha in 1.1, based on feedback may be turned into an API or change. Set to "1" to enable.
	NodeMetadataHTTP10 = "HTTP10"

	// NodeMetadataConfigNamespace is the name of the metadata variable that carries info about
	// the config namespace associated with the proxy
	NodeMetadataConfigNamespace = "CONFIG_NAMESPACE"

	// NodeMetadataSidecarUID is the user ID running envoy. Pilot can check if envoy runs as root, and may generate
	// different configuration. If not set, the default istio-proxy UID (1337) is assumed.
	NodeMetadataSidecarUID = "SIDECAR_UID"

	// NodeMetadataRequestedNetworkView specifies the networks that the proxy wants to see
	NodeMetadataRequestedNetworkView = "REQUESTED_NETWORK_VIEW"

	// NodeMetadataRouterMode indicates whether the proxy is functioning as a SNI-DNAT router
	// processing the AUTO_PASSTHROUGH gateway servers
	NodeMetadataRouterMode = "ROUTER_MODE"

	// NodeMetadataInstanceIPs is the set of IPs attached to this proxy
	NodeMetadataInstanceIPs = "INSTANCE_IPS"

	// NodeMetadataSdsTokenPath specifies the path of the SDS token used by the Enovy proxy.
	// If not set, Pilot uses the default SDS token path.
	NodeMetadataSdsTokenPath = "SDS_TOKEN_PATH"

	// NodeMetadataPolicyCheckRetries determines the policy for behavior when unable to connect to mixer
	// If not set, FAIL_CLOSE is set, rejecting requests.
	NodeMetadataPolicyCheck = "policy.istio.io/check"

	// NodeMetadataPolicyCheckRetries is the max number of retries on transport error to mixer
	// If not set, this will be 0, indicating no retries.
	NodeMetadataPolicyCheckRetries = "policy.istio.io/checkRetries"

	// NodeMetadataPolicyCheckBaseRetryWaitTime for base time to wait between retries, will be adjusted by backoff and jitter.
	// In duration format. If not set, this will be 80ms.
	NodeMetadataPolicyCheckBaseRetryWaitTime = "policy.istio.io/checkBaseRetryWaitTime"

	// NodeMetadataPolicyCheckMaxRetryWaitTime for max time to wait between retries
	// In duration format. If not set, this will be 1000ms.
	NodeMetadataPolicyCheckMaxRetryWaitTime = "policy.istio.io/checkMaxRetryWaitTime"

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

	// NodeMetadataIdleTimeout specifies the idle timeout for the proxy, in duration format (10s).
	// If not set, no timeout is set.
	NodeMetadataIdleTimeout = "IDLE_TIMEOUT"
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

	switch node.Metadata[NodeMetadataInterceptionMode] {
	case "TPROXY":
		return InterceptionTproxy
	case "REDIRECT":
		return InterceptionRedirect
	case "NONE":
		return InterceptionNone
	}

	return InterceptionRedirect
}
