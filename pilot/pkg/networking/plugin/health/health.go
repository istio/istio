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

package health

import (
	"fmt"
	"reflect"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcfilter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/health_check/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/proto"
)

// Plugin implements Istio mTLS auth
type Plugin struct{}

// NewPlugin returns an instance of the health plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// BuildHealthCheckFilter returns a HealthCheck filter.
func buildHealthCheckFilter(probe *model.Probe, isXDSMarshalingToAnyEnabled bool) *http_conn.HttpFilter {
	config := &hcfilter.HealthCheck{
		PassThroughMode: proto.BoolTrue,
		Headers: []*envoy_api_v2_route.HeaderMatcher{
			{
				Name:                 ":path",
				HeaderMatchSpecifier: &envoy_api_v2_route.HeaderMatcher_ExactMatch{ExactMatch: probe.Path},
			},
		},
	}

	out := &http_conn.HttpFilter{
		Name: xdsutil.HealthCheck,
	}

	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(config)}
	} else {
		out.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(config)}
	}

	return out
}

func buildHealthCheckFilters(filterChain *plugin.FilterChain, probes model.ProbeList, endpoint *model.NetworkEndpoint, isXDSMarshalingToAnyEnabled bool) {
	for _, probe := range probes {
		// Check that the probe matches the listener port. If not, then the probe will be handled
		// as a management port and not traced. If the port does match, then we need to add a
		// health check filter for the probe path, to ensure that health checks are not traced.
		// If no probe port is defined, then port has not specifically been defined, so assume filter
		// needs to be applied.
		if probe.Port == nil || probe.Port.Port == endpoint.Port {
			filter := buildHealthCheckFilter(probe, isXDSMarshalingToAnyEnabled)
			if !containsHTTPFilter(filterChain.HTTP, filter) {
				filterChain.HTTP = append(filterChain.HTTP, filter)
			}
		}
	}
}

func containsHTTPFilter(array []*http_conn.HttpFilter, elem *http_conn.HttpFilter) bool {
	for _, item := range array {
		if reflect.DeepEqual(item, elem) {
			return true
		}
	}
	return false
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (Plugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	// TODO: implementation
	return nil
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (Plugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Node == nil {
		return nil
	}

	if in.Node.Type != model.SidecarProxy {
		// Only care about sidecar.
		return nil
	}

	if in.ServiceInstance == nil {
		return nil
	}

	if mutable.Listener == nil {
		return fmt.Errorf("listener not defined in mutable %v", mutable)
	}

	isXDSMarshalingToAnyEnabled := util.IsXDSMarshalingToAnyEnabled(in.Node)

	for i := range mutable.Listener.FilterChains {
		if in.ListenerProtocol == plugin.ListenerProtocolHTTP {
			for _, ip := range in.Node.IPAddresses {
				buildHealthCheckFilters(&mutable.FilterChains[i], in.Env.WorkloadHealthCheckInfo(ip),
					&in.ServiceInstance.Endpoint, isXDSMarshalingToAnyEnabled)
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

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (Plugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}
