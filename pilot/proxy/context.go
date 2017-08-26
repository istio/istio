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

	// Access to TLS secrets from ingress proxies
	model.SecretRegistry

	// Mesh is the mesh config (to be merged into the config store)
	Mesh *proxyconfig.ProxyMeshConfig
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
)

// DefaultMeshConfig configuration
func DefaultMeshConfig() proxyconfig.ProxyMeshConfig {
	return proxyconfig.ProxyMeshConfig{
		DiscoveryAddress:   "istio-pilot:8080",
		EgressProxyAddress: "istio-egress:80",

		ProxyListenPort:        15001,
		ProxyAdminPort:         15000,
		DrainDuration:          ptypes.DurationProto(2 * time.Second),
		ParentShutdownDuration: ptypes.DurationProto(3 * time.Second),
		DiscoveryRefreshDelay:  ptypes.DurationProto(1 * time.Second),
		ConnectTimeout:         ptypes.DurationProto(1 * time.Second),
		IstioServiceCluster:    "istio-proxy",

		IngressClass:          "istio",
		IngressControllerMode: proxyconfig.ProxyMeshConfig_STRICT,

		AuthPolicy:    proxyconfig.ProxyMeshConfig_NONE,
		AuthCertsPath: "/etc/certs",
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
