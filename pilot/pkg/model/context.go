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
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/features/pilot"

	multierror "github.com/hashicorp/go-multierror"
	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Accounts interface for listing service accounts
	// Deprecated - use PushContext.ServiceAccounts
	ServiceAccounts

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

	// mutex control access to mutable fields in the Proxy. On-demand will modify the
	// list of services based on calls from envoy.
	mutex sync.RWMutex

	// serviceDependencies, if set, controls the list of outbound listeners and routes
	// for which the proxy will receive configurations. If nil, the proxy will get config
	// for all visible services.
	// The list will be populated either from explicit declarations or using 'on-demand'
	// feature, before generation takes place. Each node may have a different list, based on
	// the requests handled by envoy.
	serviceDependencies []*Service
}

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// Sidecar type is used for sidecar proxies in the application containers
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

// Isolated return true if the node should only return services from its own namespace
// or explicit imports.
func (node *Proxy) Isolated() bool {
	if node == nil {
		return false
	}

	// Gateways use explicit imports (or explicit bind to gateway)
	if node.Type != SidecarProxy {
		return false
	}

	// None interception mode requires strict isolation, since it depends on port uniqueness for TCP services.
	if node.Metadata[pilot.InterceptionMode] == pilot.InterceptionModeNone {
		return true
	}

	if node.Metadata[pilot.Isolation] != "" {
		return true
	}

	// Global enable flag, containing default namespaces
	if pilot.NetworkScopes != "" {
		return true
	}

	return false
}

// GetProxyVersion returns the proxy version string identifier, and whether it is present.
func (node *Proxy) GetProxyVersion() (string, bool) {
	version, found := node.Metadata["ISTIO_PROXY_VERSION"]
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
	if modestr, found := node.Metadata["ROUTER_MODE"]; found {
		switch RouterMode(modestr) {
		case SniDnatRouter:
			return SniDnatRouter
		}
	}
	return StandardRouter
}

// NoneIngressApplicationPort returns the port used by application for an inbound listener.
// Without iptables we use 3 ports for each inbound service: the original service port, used for connecting
// to other instances of the service, the 'endpoint port' where Envoy is listening, and a local application port.
// For iptable modes the last 2 are the same.
func (node *Proxy) NoneIngressApplicationPort(push *PushContext, instances []*ServiceInstance, orig int) (int, error) {
	// TODO: extract the port from Sidecar
	// TODO: sidecar should be cached in SidecarScope, no DB access here. This is temp until Shriram's PR is merged
	cfgs, err := push.Env.List(Sidecar.Type, node.ConfigNamespace)
	if err != nil {
		log.Error("XXX tmp code - no sidecars")
	}
	var matchingSc *v1alpha3.Sidecar
	for _, sc := range cfgs {
		sidecar := sc.Spec.(*v1alpha3.Sidecar)
		if sidecar.WorkloadSelector == nil || len(sidecar.WorkloadSelector.Labels) == 0 {
			continue // the default sidecar or sidecar without labels can't be used to customize ingress for the pod.
		}
		var workloadLabels LabelsCollection
		for _, w := range instances {
			workloadLabels = append(workloadLabels, w.Labels)
		}
		workloadSelector := Labels(sidecar.GetWorkloadSelector().GetLabels())
		if !workloadLabels.IsSupersetOf(workloadSelector) {
			continue
		}
		log.Infof("%v", sidecar)
		matchingSc = sidecar
		break
	}
	// END TEMP CODE

	if matchingSc != nil {
		for _, ing := range matchingSc.GetIngress() {
			if ing.Port == nil {
				continue
			}
			if ing.Port.Number == uint32(orig) {
				// Found matching port
				if ing.GetDefaultEndpoint() == "" {
					return 0, errors.New("missing defaultEndpoint")
				}
				host, port, err := net.SplitHostPort(ing.GetDefaultEndpoint())
				if err != nil {
					return 0, err
				}
				if host != "127.0.0.1" {
					return 0, errors.New("unexpected host in defaultEndpoint, must be 127.0.0.1")
				}
				portN, err := strconv.Atoi(port)
				if err != nil {
					return 0, err
				}
				return portN, nil
			}
		}
	}

	// TODO: allow envoy, via metadata, to provide a workload specific value. For example pilot-agent can
	// check UDS socket or communicate with the application (via scripts or other means) to determine the port.
	// Sidecar can provide a default value, but in some environments (raw VMs, etc) the port may be in use.

	// Hack/fallback for initial implementation to unblock testing: add or subtract 30000 to container ports.
	return 0, fmt.Errorf("no Sidecar or matching ingress port found %d", orig)
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
	if networks, found := node.Metadata["REQUESTED_NETWORK_VIEW"]; found {
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
	if ipstr, found := metadata["ISTIO_META_INSTANCE_IPS"]; found {
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

// GetProxyConfigNamespace extracts the namespace associated with the proxy
// from the proxy metadata or the proxy ID
func GetProxyConfigNamespace(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}

	// First look for ISTIO_META_CONFIG_NAMESPACE
	if configNamespace, found := proxy.Metadata[pilot.ConfigNamespace]; found {
		return configNamespace
	}

	parts := strings.Split(proxy.ID, ".")
	if len(parts) > 1 {
		return parts[1]
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

	// KeyFilename is mTLS private key
	KeyFilename = "key.pem"

	// RootCertFilename is mTLS root cert
	RootCertFilename = "root-cert.pem"

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
	DiscoveryPlainAddress = "istio-pilot:15007"

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
		ConfigPath:             ConfigPathDir,
		BinaryPath:             BinaryPathFilename,
		ServiceCluster:         ServiceClusterName,
		DrainDuration:          types.DurationProto(2 * time.Second),
		ParentShutdownDuration: types.DurationProto(3 * time.Second),
		DiscoveryAddress:       DiscoveryPlainAddress,
		ConnectTimeout:         types.DurationProto(1 * time.Second),
		StatsdUdpAddress:       "",
		ProxyAdminPort:         15000,
		ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_NONE,
		CustomConfigFile:       "",
		Concurrency:            0,
		StatNameLength:         189,
		Tracing:                nil,
	}
}

// DefaultMeshConfig configuration
func DefaultMeshConfig() meshconfig.MeshConfig {
	config := DefaultProxyConfig()
	return meshconfig.MeshConfig{
		MixerCheckServer:      "",
		MixerReportServer:     "",
		DisablePolicyChecks:   false,
		PolicyCheckFailOpen:   false,
		ProxyListenPort:       15001,
		ConnectTimeout:        types.DurationProto(1 * time.Second),
		IngressClass:          "istio",
		IngressControllerMode: meshconfig.MeshConfig_STRICT,
		EnableTracing:         true,
		AccessLogFile:         "/dev/stdout",
		AccessLogEncoding:     meshconfig.MeshConfig_TEXT,
		DefaultConfig:         &config,
		SdsUdsPath:            "",
		EnableSdsTokenMount:   false,
		TrustDomain:           "",
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
