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

package proxy

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
)

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances
	model.ServiceDiscovery

	// Accounts interface for listing service accounts
	model.ServiceAccounts

	// Config interface for listing routing rules
	model.IstioConfigStore

	// Mesh is the mesh config (to be merged into the config store)
	Mesh *proxyconfig.MeshConfig
}

// Node defines the proxy attributes used by xDS identification
type Node struct {
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
}

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// Sidecar type is used for sidecar proxies in the application containers
	Sidecar NodeType = "sidecar"

	// Ingress type is used for cluster ingress proxies
	Ingress NodeType = "ingress"

	// Egress type is used for cluster egress proxies
	Egress NodeType = "egress"
)

// ServiceNode encodes the proxy node attributes into a URI-acceptable string
func (node Node) ServiceNode() string {
	return strings.Join([]string{
		string(node.Type), node.IPAddress, node.ID, node.Domain,
	}, serviceNodeSeparator)

}

// ParseServiceNode is the inverse of service node function
func ParseServiceNode(s string) (Node, error) {
	parts := strings.Split(s, serviceNodeSeparator)
	out := Node{}

	if len(parts) != 4 {
		return out, errors.New("missing parts in the service node")
	}

	out.Type = NodeType(parts[0])
	out.IPAddress = parts[1]
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
)

// DefaultProxyConfig for individual proxies
func DefaultProxyConfig() proxyconfig.ProxyConfig {
	return proxyconfig.ProxyConfig{
		ConfigPath:             "/etc/istio/proxy",
		BinaryPath:             "/usr/local/bin/envoy",
		ServiceCluster:         "istio-proxy",
		DrainDuration:          ptypes.DurationProto(2 * time.Second),
		ParentShutdownDuration: ptypes.DurationProto(3 * time.Second),
		DiscoveryAddress:       "istio-pilot:8080",
		DiscoveryRefreshDelay:  ptypes.DurationProto(1 * time.Second),
		ZipkinAddress:          "",
		ConnectTimeout:         ptypes.DurationProto(1 * time.Second),
		StatsdUdpAddress:       "",
		ProxyAdminPort:         15000,
	}
}

// DefaultMeshConfig configuration
func DefaultMeshConfig() proxyconfig.MeshConfig {
	config := DefaultProxyConfig()
	return proxyconfig.MeshConfig{
		EgressProxyAddress:    "istio-egress:80",
		MixerAddress:          "",
		DisablePolicyChecks:   false,
		ProxyListenPort:       15001,
		ConnectTimeout:        ptypes.DurationProto(1 * time.Second),
		IngressClass:          "istio",
		IngressControllerMode: proxyconfig.MeshConfig_STRICT,
		AuthPolicy:            proxyconfig.MeshConfig_NONE,
		RdsRefreshDelay:       ptypes.DurationProto(1 * time.Second),
		EnableTracing:         true,
		AccessLogFile:         "/dev/stdout",
		DefaultConfig:         &config,
	}
}

// ParsePort extracts port number from a valid proxy address
func ParsePort(addr string) int {
	port, err := strconv.Atoi(addr[strings.Index(addr, ":")+1:])
	if err != nil {
		glog.Warning(err)
	}

	return port
}
