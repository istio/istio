package metadataexchange

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

// Plugin implements Istio Telemetry HTTP and TCP metadata exchange
type Plugin struct{}

// NewPlugin returns an instance of the metadataexchange plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters.
func (p Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}
	return buildFilter(in, mutable, false)
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain
// configuration, like FilterChainMatch and TLSContext.
func (p Plugin) OnInboundFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service.
// Can be used to add additional filters on the outbound path.
func (p Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return buildFilter(in, mutable, false)
}

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
// Can be used to add additional filters.
func (p Plugin) OnInboundPassthrough(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return nil
}

// OnInboundPassthroughFilterChains is called whenever a plugin needs to setup custom pass through filter chain.
func (p Plugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}

func buildFilter(in *plugin.InputParams, mutable *networking.MutableObjects, isPassthrough bool) error {
	for i := range mutable.FilterChains {
		if in.ListenerProtocol == networking.ListenerProtocolHTTP || mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolHTTP {
			// gateway only has http_filter what about wasm enabled in this case?
			if httpMxFilter := xdsfilters.HTTPMx; httpMxFilter != nil {
				mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, httpMxFilter)
			}
		}
		if features.EnableTCPMetadataExchange && (in.ListenerProtocol == networking.ListenerProtocolTCP || mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolTCP) {
			if tcpMxFilter := xdsfilters.TCPMx; tcpMxFilter != nil {
				mutable.FilterChains[i].TCP = append(mutable.FilterChains[i].TCP, tcpMxFilter)
			}
		}
	}
	return nil
}
