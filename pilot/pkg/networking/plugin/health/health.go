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
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
)

// Plugin implements Istio mTLS auth
type Plugin struct{}

// NewPlugin returns an instance of the health plugin
func NewPlugin() plugin.Plugin {
	return Plugin{}
}

// BuildHealthCheckFilter returns a HealthCheck filter.
func BuildHealthCheckFilter(probe model.Probe) *http_conn.HttpFilter {
	return &http_conn.HttpFilter{
		Name: xdsutil.HealthCheck,
		Config: util.MessageToStruct(&hcfilter.HealthCheck{
			PassThroughMode: &types.BoolValue{
				Value: true,
			},
			Headers: []*envoy_api_v2_route.HeaderMatcher{
				{
					Name:  ":path",
					Value: probe.Path,
				},
			},
		}),
	}
}

func buildHealthCheckFilters(filterChain *plugin.FilterChain, probes []model.Probe) {
	for _, probe := range probes {
		filter := BuildHealthCheckFilter(probe)
		if !containsHttpFilter(filterChain.HTTP, filter) {
			filterChain.HTTP = append(filterChain.HTTP, filter)
		}
	}
}

func containsHttpFilter(array []*http_conn.HttpFilter, elem *http_conn.HttpFilter) bool {
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
	if in.Node.Type != model.Sidecar {
		// Only care about sidecar.
		return nil
	}

	if in.ServiceInstance == nil {
		return nil
	}

	if mutable.Listener == nil {
		return fmt.Errorf("listener not defined in mutable %v", mutable)
	}

	if len(mutable.Listener.FilterChains) != len(mutable.FilterChains) {
		return fmt.Errorf("expected same number of filter chains in listener (%d) and mutable (%d)", len(mutable.Listener.FilterChains), len(mutable.FilterChains))
	}

	for i := range mutable.Listener.FilterChains {
		if in.ListenerType == plugin.ListenerTypeHTTP {
			// Build health check filters
			buildHealthCheckFilters(&mutable.FilterChains[i], in.ServiceInstance.ReadinessProbes)
			buildHealthCheckFilters(&mutable.FilterChains[i], in.ServiceInstance.LivenessProbes)
		}
	}

	return nil
}

// OnInboundCluster implements the Plugin interface method.
func (Plugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnOutboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (Plugin) OnInboundRouteConfiguration(in *plugin.InputParams, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster implements the Plugin interface method.
func (Plugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}
