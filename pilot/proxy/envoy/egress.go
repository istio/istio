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
	"crypto/sha1"
	"fmt"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
)

func buildEgressListeners(mesh *proxyconfig.MeshConfig, egress proxy.Node) Listeners {
	port := proxy.ParsePort(mesh.EgressProxyAddress)
	listener := buildHTTPListener(mesh, egress, nil, nil, WildcardAddress, port, fmt.Sprintf("%d", port),
		false, IngressTraceOperation)
	applyInboundAuth(listener, mesh)
	return Listeners{listener}
}

// buildEgressRoutes lists all HTTP route configs on the egress proxy
func buildEgressRoutes(mesh *proxyconfig.MeshConfig, services model.ServiceDiscovery) HTTPRouteConfigs {
	// Create a VirtualHost for each external service
	vhosts := make([]*VirtualHost, 0)
	for _, service := range services.Services() {
		if service.External() {
			if host := buildEgressHTTPRoute(mesh, service); host != nil {
				vhosts = append(vhosts, host)
			}
		}
	}
	port := proxy.ParsePort(mesh.EgressProxyAddress)
	configs := HTTPRouteConfigs{port: &HTTPRouteConfig{VirtualHosts: vhosts}}
	return configs.normalize()
}

// buildEgressRoute translates an egress rule to an Envoy route
func buildEgressHTTPRoute(mesh *proxyconfig.MeshConfig, svc *model.Service) *VirtualHost {
	var host *VirtualHost

	for _, servicePort := range svc.Ports {
		protocol := servicePort.Protocol
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolHTTPS:
			key := svc.Key(servicePort, nil)
			cluster := &Cluster{
				Name:   fmt.Sprintf("%x", sha1.Sum([]byte(key))),
				Type:   ClusterTypeStrictDNS,
				LbType: DefaultLbType,
				Hosts: []Host{{
					URL: fmt.Sprintf("tcp://%s:%d", svc.ExternalName, servicePort.Port),
				}},
			}

			if servicePort.Protocol == model.ProtocolGRPC || servicePort.Protocol == model.ProtocolHTTP2 {
				cluster.Features = ClusterFeatureHTTP2
			}

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

			// enable mixer check on the route
			if mesh.MixerAddress != "" {
				route.OpaqueConfig = buildMixerOpaqueConfig(!mesh.DisablePolicyChecks, false)
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
