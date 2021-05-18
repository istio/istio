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
	tcppb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/pkg/log"
)

var authzLog = log.RegisterScope("authorization", "Istio Authorization Policy", 0)

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

	p.buildFilter(in, mutable)
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

	p.buildFilter(in, mutable)
	return nil
}

func (p Plugin) buildFilter(in *plugin.InputParams, mutable *networking.MutableObjects) {
	if in.Push == nil || in.Push.AuthzPolicies == nil {
		authzLog.Debugf("No authorization policy for %s", in.Node.ID)
		return
	}

	meshConfig := in.Push.Mesh
	tdBundle := trustdomain.NewBundle(meshConfig.TrustDomain, meshConfig.TrustDomainAliases)
	option := builder.Option{
		IsCustomBuilder: p.actionType == Custom,
		Logger:          &builder.AuthzLogger{},
	}
	defer option.Logger.Report(in)
	b := builder.New(tdBundle, in, option)
	if b == nil {
		return
	}

	// We will lazily build filters for tcp/http as needed
	httpBuilt := false
	tcpBuilt := false
	var httpFilters []*httppb.HttpFilter
	var tcpFilters []*tcppb.Filter

	for cnum := range mutable.FilterChains {
		switch mutable.FilterChains[cnum].ListenerProtocol {
		case networking.ListenerProtocolTCP:
			if !tcpBuilt {
				tcpFilters = b.BuildTCP()
				tcpBuilt = true
			}
			option.Logger.AppendDebugf("added %d TCP filters to filter chain %d", len(tcpFilters), cnum)
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilters...)
		case networking.ListenerProtocolHTTP:
			if !httpBuilt {
				httpFilters = b.BuildHTTP()
				httpBuilt = true
			}
			option.Logger.AppendDebugf("added %d HTTP filters to filter chain %d", len(httpFilters), cnum)
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilters...)
		}
	}
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (p Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	p.buildFilter(in, mutable)
	return nil
}

func (p Plugin) InboundMTLSConfiguration(in *plugin.InputParams, passthrough bool) []plugin.MTLSSettings {
	return nil
}
