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

package envoy

import (
	"fmt"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
)

// buildEgressRoutes lists all HTTP route configs on the egress proxy
func buildEgressRoutes(services model.ServiceDiscovery, mesh *proxyconfig.ProxyMeshConfig) HTTPRouteConfigs {
	// Create a VirtualHost for each external service
	vhosts := make([]*VirtualHost, 0)
	for _, service := range services.Services() {
		if service.External() {
			if host := buildEgressHTTPRoute(service); host != nil {
				vhosts = append(vhosts, host)
			}
		}
	}
	port := proxy.ParsePort(mesh.EgressProxyAddress)
	configs := HTTPRouteConfigs{port: &HTTPRouteConfig{VirtualHosts: vhosts}}
	configs.normalize()
	return configs
}

// buildEgressRoute translates an egress rule to an Envoy route
func buildEgressHTTPRoute(svc *model.Service) *VirtualHost {
	var host *VirtualHost

	for _, servicePort := range svc.Ports {
		protocol := servicePort.Protocol
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolHTTPS:
			cluster := buildOutboundCluster(svc.Hostname, servicePort, nil)

			// overwrite cluster hosts and types
			cluster.Type = ClusterTypeStrictDNS
			cluster.Hosts = []Host{{
				URL: fmt.Sprintf("tcp://%s:%d", svc.ExternalName, servicePort.Port),
			}}

			if protocol == model.ProtocolHTTPS {
				// TODO add root CA for public TLS
				cluster.SSLContext = &SSLContextExternal{}
			}

			route := &HTTPRoute{
				Prefix:          "/",
				Cluster:         cluster.Name,
				AutoHostRewrite: true,
				clusters:        []*Cluster{cluster},
			}

			host = &VirtualHost{
				Name:    svc.Hostname,
				Domains: []string{svc.Hostname},
				Routes:  []*HTTPRoute{route},
			}

		default:
			glog.Warningf("Unsupported outbound protocol %v for port %#v", protocol, servicePort)
		}
	}

	return host
}
