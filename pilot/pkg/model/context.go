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

	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Accounts interface for listing service accounts
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
}

// Proxy defines the proxy attributes used by xDS identification
type Proxy struct {
	// ClusterID specifies the cluster where the proxy resides
	ClusterID string

	// Type specifies the node type
	Type NodeType

	// IPAddress is the IP address of the proxy used to identify it and its
	// co-located service instances. Example: "10.60.1.6"
	IPAddress string

	// ID is the unique platform-specific sidecar proxy ID
	ID string

	// Domain defines the DNS domain suffix for short hostnames (e.g.
	// "default.svc.cluster.local")
	Domain string

	// Metadata key-value pairs extending the Node identifier
	Metadata map[string]string
}

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// Sidecar type is used for sidecar proxies in the application containers
	Sidecar NodeType = "sidecar"

	// Ingress type is used for cluster ingress proxies
	Ingress NodeType = "ingress"

	// Router type is used for standalone proxies acting as L7/L4 routers
	Router NodeType = "router"
)

// IsApplicationNodeType verifies that the NodeType is one of the declared constants in the model
func IsApplicationNodeType(nType NodeType) bool {
	switch nType {
	case Sidecar, Ingress, Router:
		return true
	default:
		return false
	}
}

// ServiceNode encodes the proxy node attributes into a URI-acceptable string
func (node *Proxy) ServiceNode() string {
	return strings.Join([]string{
		string(node.Type), node.IPAddress, node.ID, node.Domain,
	}, serviceNodeSeparator)

}

// GetProxyVersion returns the proxy version string identifier, and whether it is present.
func (node *Proxy) GetProxyVersion() (string, bool) {
	version, found := node.Metadata["ISTIO_PROXY_VERSION"]
	return version, found
}

// GetProxyMTLSInclusionSet returns list of services for which Istio mTLS should be enabled
// if there is a global destination rule specifying ISTIO_MUTUAL as the tls mode for all
// services in the system. The value is obtained from ISTIO_META_ISTIO_MTLS_INCLUDE_ONLY
// metadata variable provided by the sidecar proxy.
//
// We need this because DestinationRules are typically specified by the service owner but
// applied at all clients. One or more clients may not be able to to mTLS. So these clients
// need an escape hatch. On the server side, a permissive mTLS mode will allow both TLS and
// non-TLS traffic.
func (node *Proxy) GetProxyMTLSInclusionSet() map[string]bool {
	mTLSInclusionMap := make(map[string]bool)
	if list, found := node.Metadata["ISTIO_MTLS_INCLUDE_ONLY"]; found {
		for _, s := range strings.Split(list, ",") {
			mTLSInclusionMap[s] = true
		}
		// NOTE: if the metadata is set, but has no entries i.e. "",
		// it implies do not do mTLS for anything
		return mTLSInclusionMap
	}

	// If the metadata is not set, then do whatever is dictated by the destination rule.
	return nil
}

// HasSnowFlakes returns true if this proxy has any configuration that could cause it to
// receive xDS output that is different from other proxies.
// Currently, we check for things like mtls inclusion list which is used only by the
// multi-cluster gateway.
func (node *Proxy) HasSnowFlakes() bool {
	if _, found := node.Metadata["ISTIO_MTLS_INCLUDE_ONLY"]; found {
		return true
	}
	return false
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

// ParseServiceNode is the inverse of service node function
func ParseServiceNode(s string) (Proxy, error) {
	parts := strings.Split(s, serviceNodeSeparator)
	out := Proxy{}

	if len(parts) != 4 {
		return out, fmt.Errorf("missing parts in the service node %q", s)
	}

	out.Type = NodeType(parts[0])

	switch out.Type {
	case Sidecar, Ingress, Router:
	default:
		return out, fmt.Errorf("invalid node type (valid types: ingress, sidecar, router in the service node %q", s)
	}
	out.IPAddress = parts[1]

	// Does query from ingress or router have to carry valid IP address?
	if net.ParseIP(out.IPAddress) == nil && out.Type == Sidecar {
		return out, fmt.Errorf("invalid IP address %q in the service node %q", out.IPAddress, s)
	}

	out.ID = parts[2]
	out.Domain = parts[3]
	return out, nil
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
		ZipkinAddress:          "",
		ConnectTimeout:         types.DurationProto(1 * time.Second),
		StatsdUdpAddress:       "",
		ProxyAdminPort:         15000,
		ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_NONE,
		CustomConfigFile:       "",
		Concurrency:            0,
		StatNameLength:         189,
	}
}

// DefaultMeshConfig configuration
func DefaultMeshConfig() meshconfig.MeshConfig {
	config := DefaultProxyConfig()
	return meshconfig.MeshConfig{
		MixerCheckServer:      "",
		MixerReportServer:     "",
		DisablePolicyChecks:   false,
		ProxyListenPort:       15001,
		ConnectTimeout:        types.DurationProto(1 * time.Second),
		IngressClass:          "istio",
		IngressControllerMode: meshconfig.MeshConfig_STRICT,
		EnableTracing:         true,
		AccessLogFile:         "/dev/stdout",
		DefaultConfig:         &config,
		SdsUdsPath:            "",
	}
}

// ApplyMeshConfigDefaults returns a new MeshConfig decoded from the
// input YAML with defaults applied to omitted configuration values.
func ApplyMeshConfigDefaults(yaml string) (*meshconfig.MeshConfig, error) {
	out := DefaultMeshConfig()
	if err := ApplyYAML(yaml, &out); err != nil {
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
		if err := ApplyYAML(origProxyConfigYAML, out.DefaultConfig); err != nil {
			return nil, multierror.Prefix(err, "failed to convert to proto.")
		}
	}

	if err := ValidateMeshConfig(&out); err != nil {
		return nil, err
	}
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
