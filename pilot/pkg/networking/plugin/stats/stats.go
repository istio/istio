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

package stats

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	protobuf "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

// Plugin implements Istio Telemetry HTTP/TCP prometheus stats filter
type Plugin struct{}

// NewPlugin returns an instance of the HTTP/TCP prometheus stats plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// OnInboundListener is called whenever a new HTTP/TCP prometheus stats filter is added to the Listener filter chain.
func (p Plugin) OnInboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}
	return buildFilter("stats_inbound", in, mutable)
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain
// configuration, like FilterChainMatch and TLSContext.
func (p Plugin) OnInboundFilterChains(in *plugin.InputParams) []networking.FilterChain {
	return nil
}

// OnOutboundListener is called whenever a new HTTP/TCP prometheus stats filter is added to the Listener filter chain.
func (p Plugin) OnOutboundListener(in *plugin.InputParams, mutable *networking.MutableObjects) error {
	return buildFilter("stats_outbound", in, mutable)
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

// Build the HTTP and TCP prometheus stats telemetry filter based on the LisenterProtocol
func buildFilter(vmId string, in *plugin.InputParams, mutable *networking.MutableObjects) error {
	for i := range mutable.FilterChains {
		if in.ListenerProtocol == networking.ListenerProtocolHTTP || mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolHTTP {
			mutable.FilterChains[i].HTTP = append(mutable.FilterChains[i].HTTP, buildStatsHTTPFilter(vmId))
		}
		if in.ListenerProtocol == networking.ListenerProtocolTCP ||
			mutable.FilterChains[i].ListenerProtocol == networking.ListenerProtocolTCP {
			mutable.FilterChains[i].TCP = append(mutable.FilterChains[i].TCP, buildStatsTCPFilter(vmId))
		}
	}
	return nil
}

func buildStatsHTTPFilter(vmId string) *hcm.HttpFilter {
	var vmConfig *v3.PluginConfig_VmConfig
	// TODO: somewhere to put if stats enabled
	if features.EnableWasmTelemetry {
		vmConfig = &v3.PluginConfig_VmConfig{
			VmConfig: &v3.VmConfig{
				// TODO: vm_id should be put into an arg
				VmId:             vmId,
				Runtime:          "envoy.wasm.runtime.v8",
				AllowPrecompiled: true,
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: "/etc/istio/extensions/stats-filter.compiled.wasm",
						},
					},
				}},
			}}
	} else {
		vmConfig = &v3.PluginConfig_VmConfig{
			VmConfig: &v3.VmConfig{
				VmId:    vmId,
				Runtime: "envoy.wasm.runtime.null",
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: "envoy.wasm.stats",
						},
					},
				}},
			},
		}
	}

	httpMxConfigProto := &wasm.Wasm{
		Config: &v3.PluginConfig{
			Vm: vmConfig,
			// TODO: determine: .Values.telemetry.v2.prometheus.configOverride.inboundSidecar
			// cluster_id
			Configuration: util.MessageToAny(&protobuf.StringValue{Value: "{}\n"}),
		},
	}

	typed, err := ptypes.MarshalAny(httpMxConfigProto)
	if err != nil {
		return nil
	}

	return &hcm.HttpFilter{
		Name:       xdsfilters.StatsFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: typed},
	}
}

func buildStatsTCPFilter(vmId string) *listener.Filter {
	// TODO: to be implemented
	return nil
}
