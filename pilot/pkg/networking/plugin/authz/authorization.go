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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

var (
	authzLog = log.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

type ActionType int

const (
	// Local for action ALLOW, DENY and AUDIT and is enforced by Envoy RBAC filter.
	Local ActionType = iota
	// Custom action is enforced by Envoy ext_authz filter.
	Custom
)

// Plugin implements Istio Authorization
type Plugin struct {
	actionType ActionType
}

// NewPlugin returns an instance of the authorization plugin
func NewPlugin(actionType ActionType) plugin.Plugin {
	return Plugin{actionType: actionType}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (p Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.Router {
		// Only care about router.
		return nil
	}

	p.buildFilter(in, mutable, false)
	return nil
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (p Plugin) OnInboundFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters or add more stuff to the HTTP connection manager
// on the inbound path
func (p Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	p.buildFilter(in, mutable, false)
	return nil
}

func (p Plugin) buildFilter(in *plugin.InputParams, mutable *networking.MutableObjects, isOnInboundPassthrough bool) {
	if in.Push == nil || in.Push.AuthzPolicies == nil {
		authzLog.Debugf("no authorization policy in push context")
		return
	}

	// TODO: Get trust domain from MeshConfig instead.
	// https://github.com/istio/istio/issues/17873
	tdBundle := trustdomain.NewBundle(spiffe.GetTrustDomain(), in.Push.Mesh.TrustDomainAliases)
	option := builder.Option{
		IsIstioVersionGE15:     util.IsIstioVersionGE15(in.Node),
		IsOnInboundPassthrough: isOnInboundPassthrough,
		IsCustomBuilder:        p.actionType == Custom,
	}

	b := builder.New(tdBundle, in, option)
	if b == nil {
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

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (p Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	p.buildFilter(in, mutable, true)
	return nil
}

// OnInboundPassthroughFilterChains is called for plugin to update the pass through filter chain.
func (p Plugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}
