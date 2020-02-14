// Copyright 2019 Istio Authors
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

// Package authz converts Istio RBAC (role-based-access-control) policies (ServiceRole and ServiceRoleBinding)
// to the Envoy RBAC filter config to enforce access control to the service co-located with Envoy.
// The generation is controlled by ClusterRbacConfig (a singleton custom resource with cluster scope).
// User could disable this plugin by either deleting the ClusterRbacConfig or set the ClusterRbacConfig.mode
// to OFF.
// Note: ClusterRbacConfig is not created with istio installation which means this plugin doesn't
// generate any RBAC config by default.
package authz

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	authzBuilder "istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/spiffe"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

// Plugin implements Istio Authorization
type Plugin struct{}

// NewPlugin returns an instance of the authorization plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.Router {
		// Only care about router.
		return nil
	}

	buildFilter(in, mutable)
	return nil
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	buildFilter(in, mutable)
	return nil
}

func buildFilter(in *plugin.InputParams, mutable *plugin.MutableObjects) {
	// TODO: Get trust domain from MeshConfig instead.
	// https://github.com/istio/istio/issues/17873
	trustDomainBundle := trustdomain.NewTrustDomainBundle(spiffe.GetTrustDomain(), in.Push.Mesh.TrustDomainAliases)
	builder := authzBuilder.NewBuilder(trustDomainBundle, in.ServiceInstance,
		in.Node.WorkloadLabels, in.Node.ConfigNamespace, in.Push.AuthzPolicies)
	if builder == nil {
		return
	}

	switch in.ListenerProtocol {
	case plugin.ListenerProtocolTCP:
		rbacLog.Debugf("building filter for TCP listener protocol")
		tcpFilters := builder.BuildTCPFilters()
		if in.Node.Type == model.Router {
			// For gateways, due to TLS termination, a listener marked as TCP could very well
			// be using a HTTP connection manager. So check the filterChain.listenerProtocol
			// to decide the type of filter to attach
			httpFilters := builder.BuildHTTPFilters()
			for cnum := range mutable.FilterChains {
				if mutable.FilterChains[cnum].ListenerProtocol == plugin.ListenerProtocolHTTP {
					for _, httpFilter := range httpFilters {
						rbacLog.Debugf("added HTTP filter to gateway filter chain %d", cnum)
						mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
					}
				} else {
					for _, tcpFilter := range tcpFilters {
						rbacLog.Debugf("added TCP filter to gateway filter chain %d", cnum)
						mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
					}
				}
			}
		} else {
			for _, tcpFilter := range tcpFilters {
				for cnum := range mutable.FilterChains {
					rbacLog.Debugf("added TCP filter to filter chain %d", cnum)
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
				}
			}
		}
	case plugin.ListenerProtocolHTTP:
		rbacLog.Debugf("building filter for HTTP listener protocol")
		filters := builder.BuildHTTPFilters()
		for _, filter := range filters {
			for cnum := range mutable.FilterChains {
				rbacLog.Debugf("added HTTP filter to filter chain %d", cnum)
				mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
			}
		}
	case plugin.ListenerProtocolAuto:
		rbacLog.Debugf("building filter for AUTO listener protocol")
		httpFilters := builder.BuildHTTPFilters()
		tcpFilters := builder.BuildTCPFilters()

		for cnum := range mutable.FilterChains {
			switch mutable.FilterChains[cnum].ListenerProtocol {
			case plugin.ListenerProtocolTCP:
				for _, tcpFilter := range tcpFilters {
					rbacLog.Debugf("added TCP filter to filter chain %d", cnum)
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
				}
			case plugin.ListenerProtocolHTTP:
				for _, httpFilter := range httpFilters {
					rbacLog.Debugf("added HTTP filter to filter chain %d", cnum)
					mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
				}
			}
		}
	}
}

// OnVirtualListener implements the Plugin interface method.
func (Plugin) OnVirtualListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	buildFilter(in, mutable)
	return nil
}

// OnInboundPassthroughFilterChains is called for plugin to update the pass through filter chain.
func (Plugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}
