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

package authz

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/spiffe"
)

var (
	authzLog = log.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

// Plugin implements Istio Authorization
type Plugin struct{}

// NewPlugin returns an instance of the authorization plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.Router {
		// Only care about router.
		return nil
	}

	buildFilter(in, mutable)
	return nil
}

func (Plugin) OnOutboundPassthroughFilterChain(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	buildFilter(in, mutable)
	return nil
}

func buildFilter(in *plugin.InputParams, mutable *networking.MutableObjects) {
	if in.Push == nil || in.Push.AuthzPolicies == nil {
		authzLog.Debugf("no authorization policy in push context")
		return
	}

	// TODO: Get trust domain from MeshConfig instead.
	// https://github.com/istio/istio/issues/17873
	tdBundle := trustdomain.NewBundle(spiffe.GetTrustDomain(), in.Push.Mesh.TrustDomainAliases)
	namespace := in.Node.ConfigNamespace
	workload := labels.Collection{in.Node.Metadata.Labels}
	b := builder.New(tdBundle, workload, namespace, in.Push.AuthzPolicies, util.IsIstioVersionGE15(in.Node))
	if b == nil {
		authzLog.Debugf("no authorization policy for workload %v in %s", workload, namespace)
		return
	}

	switch in.ListenerProtocol {
	case networking.ListenerProtocolTCP:
		authzLog.Debugf("building filter for TCP listener protocol")
		tcpFilters := b.BuildTCP()
		if in.Node.Type == model.Router {
			// For gateways, due to TLS termination, a listener marked as TCP could very well
			// be using a HTTP connection manager. So check the filterChain.listenerProtocol
			// to decide the type of filter to attach
			httpFilters := b.BuildHTTP()
			for cnum := range mutable.FilterChains {
				if mutable.FilterChains[cnum].ListenerProtocol == networking.ListenerProtocolHTTP {
					for _, httpFilter := range httpFilters {
						authzLog.Debugf("added HTTP filter to gateway filter chain %d", cnum)
						mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
					}
				} else {
					for _, tcpFilter := range tcpFilters {
						authzLog.Debugf("added TCP filter to gateway filter chain %d", cnum)
						mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
					}
				}
			}
		} else {
			for _, tcpFilter := range tcpFilters {
				for cnum := range mutable.FilterChains {
					authzLog.Debugf("added TCP filter to filter chain %d", cnum)
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
				}
			}
		}
	case networking.ListenerProtocolHTTP:
		authzLog.Debugf("building filter for HTTP listener protocol")
		httpFilters := b.BuildHTTP()
		for _, filter := range httpFilters {
			for cnum := range mutable.FilterChains {
				authzLog.Debugf("added HTTP filter to filter chain %d", cnum)
				mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
			}
		}
	case networking.ListenerProtocolAuto:
		authzLog.Debugf("building filter for AUTO listener protocol")
		httpFilters := b.BuildHTTP()
		tcpFilters := b.BuildTCP()

		for cnum := range mutable.FilterChains {
			switch mutable.FilterChains[cnum].ListenerProtocol {
			case networking.ListenerProtocolTCP:
				for _, tcpFilter := range tcpFilters {
					authzLog.Debugf("added TCP filter to filter chain %d", cnum)
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
				}
			case networking.ListenerProtocolHTTP:
				for _, httpFilter := range httpFilters {
					authzLog.Debugf("added HTTP filter to filter chain %d", cnum)
					mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
				}
			}
		}
	}
}

// OnVirtualListener implements the Plugin interface method.
func (Plugin) OnVirtualListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(in *plugin.InputParams, cluster *cluster.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *route.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *route.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(in *plugin.InputParams, cluster *cluster.Cluster) {
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	buildFilter(in, mutable)
	return nil
}

// OnInboundPassthroughFilterChains is called for plugin to update the pass through filter chain.
func (Plugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}
