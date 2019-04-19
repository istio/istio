// Copyright 2018 Istio Authors
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

package authn

import (
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authn/factory"
)

// Plugin implements Istio mTLS auth
type Plugin struct{}

// NewPlugin returns an instance of the authn plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnInboundFilterChains setups filter chains based on the authentication policy.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return factory.NewPolicyApplier(in.Env.IstioConfigStore,
		in.ServiceInstance).InboundFilterChain(in.Env.Mesh.SdsUdsPath,
		in.Env.Mesh.EnableSdsTokenMount,
		in.Env.Mesh.SdsUseK8SSaJwt,
		in.Node.Metadata)
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.ServiceInstance == nil {
		return nil
	}

	if in.Node.Type != model.Ingress && in.Node.Type != model.Router {
		// Only care about ingress and router.
		return nil
	}

	return buildFilter(in, mutable)
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	return buildFilter(in, mutable)
}

func buildFilter(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	applier := factory.NewPolicyApplier(in.Env.IstioConfigStore, in.ServiceInstance)
	if mutable.Listener == nil || (len(mutable.Listener.FilterChains) != len(mutable.FilterChains)) {
		return fmt.Errorf("expected same number of filter chains in listener (%d) and mutable (%d)", len(mutable.Listener.FilterChains), len(mutable.FilterChains))
	}
	for i := range mutable.Listener.FilterChains {
		if in.ListenerProtocol == plugin.ListenerProtocolHTTP || mutable.FilterChains[i].ListenerProtocol == plugin.ListenerProtocolHTTP {
			// Adding Jwt filter and authn filter, if needed.
			if filter := applier.JwtFilter(util.IsXDSMarshalingToAnyEnabled(in.Node)); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
			if filter := applier.AuthNFilter(in.Node.Type, util.IsXDSMarshalingToAnyEnabled(in.Node)); filter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, filter)
			}
		}
	}

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
