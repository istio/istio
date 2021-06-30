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

package metadataexchange

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

// Plugin implements Istio Telemetry HTTP/TCP metadata exchange
type Plugin struct{}

// NewPlugin returns an instance of the metadata exchange plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnInboundListener is called whenever a new HTTP/TCP metadata exchange filter is added to the Listener filter chain.
func (p Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}
	return buildFilter(mutable)
}

// OnOutboundListener is called whenever a new HTTP/TCP metadata exchange filter is added to the Listener filter chain.
func (p Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return buildFilter(mutable)
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
// Can be used to add additional filters.
func (p Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

func (p Plugin) InboundMTLSConfiguration(in *plugin.InputParams, passthrough bool) []plugin.MTLSSettings {
	return nil
}

// Build the HTTP or TCP metadata exchange telemetry filter based on the LisenterProtocol
func buildFilter(mutable *networking.MutableObjects) error {
	for i := range mutable.FilterChains {
		if mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolHTTP {
			mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, xdsfilters.HTTPMx)
		}
		if mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolTCP {
			mutable.FilterChains[i].TCP = append(mutable.FilterChains[i].TCP, xdsfilters.TCPMx)
		}
	}
	return nil
}
