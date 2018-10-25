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

package envoyfilter

import (
	"net"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pkg/log"
)

type envoyfilterplugin struct{}

const (
	// InboundDirection indicates onInboundListener callback
	InboundDirection = "inbound"
	// OutboundDirection indicates onOutboundListener callback
	OutboundDirection = "outbound"
)

// NewPlugin returns an ptr to an initialized envoyfilter.Plugin.
func NewPlugin() plugin.Plugin {
	return envoyfilterplugin{}
}

// OnOutboundListener implements the Callbacks interface method.
func (envoyfilterplugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return insertUserSpecifiedFilters(in, mutable, OutboundDirection)
}

// OnInboundListener implements the Callbacks interface method.
func (envoyfilterplugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return insertUserSpecifiedFilters(in, mutable, InboundDirection)
}

// OnOutboundCluster implements the Plugin interface method.
func (envoyfilterplugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
	// do nothing
}

// OnInboundCluster implements the Plugin interface method.
func (envoyfilterplugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
	// do nothing
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (envoyfilterplugin) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
	// do nothing
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (envoyfilterplugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
	// do nothing
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (envoyfilterplugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}

func insertUserSpecifiedFilters(in *plugin.InputParams, mutable *plugin.MutableObjects, direction string) error {
	filterCRD := getFilterForWorkload(in)
	if filterCRD == nil {
		return nil
	}

	listenerIPAddress := getListenerIPAddress(&mutable.Listener.Address)
	if listenerIPAddress == nil {
		log.Warnf("Failed to parse IP Address from plugin listener")
	}

	for _, f := range filterCRD.Filters {
		if !listenerMatch(in, direction, listenerIPAddress, f.ListenerMatch) {
			continue
		}
		if in.ListenerProtocol == plugin.ListenerProtocolHTTP && f.FilterType == networking.EnvoyFilter_Filter_HTTP {
			// Insert into http connection manager
			for cnum := range mutable.FilterChains {
				insertHTTPFilter(&mutable.FilterChains[cnum], f)
			}
		} else {
			// tcp listener with some opaque filter or http listener with some opaque filter
			// We treat both as insert network filter X into network filter chain.
			// We cannot insert a HTTP in filter in network filter chain. Even HTTP connection manager is a network filter
			if f.FilterType == networking.EnvoyFilter_Filter_HTTP {
				log.Warnf("Ignoring filter %s. Cannot insert HTTP filter in network filter chain", f.FilterName)
				continue
			}
			for cnum := range mutable.FilterChains {
				insertNetworkFilter(&mutable.FilterChains[cnum], f)
			}
		}
	}
	return nil
}

// NOTE: There can be only one filter for a workload. If multiple filters are defined, the behavior
// is undefined.
func getFilterForWorkload(in *plugin.InputParams) *networking.EnvoyFilter {
	env := in.Env
	// collect workload labels
	workloadInstances := in.ProxyInstances

	var workloadLabels model.LabelsCollection
	for _, w := range workloadInstances {
		workloadLabels = append(workloadLabels, w.Labels)
	}

	f := env.EnvoyFilter(workloadLabels)
	if f != nil {
		return f.Spec.(*networking.EnvoyFilter)
	}
	return nil
}

func getListenerIPAddress(address *core.Address) net.IP {
	if address != nil && address.Address != nil {
		switch t := address.Address.(type) {
		case *core.Address_SocketAddress:
			if t.SocketAddress != nil {
				ip := "0.0.0.0"
				if t.SocketAddress.Address != "::" {
					ip = t.SocketAddress.Address
				}
				return net.ParseIP(ip)
			}
		}
	}
	return nil
}

func listenerMatch(in *plugin.InputParams, direction string, listenerIP net.IP,
	matchCondition *networking.EnvoyFilter_ListenerMatch) bool {
	if matchCondition == nil {
		return true
	}

	// match by port first
	if matchCondition.PortNumber > 0 && in.Port.Port != int(matchCondition.PortNumber) {
		return false
	}

	// match by port name prefix
	if matchCondition.PortNamePrefix != "" {
		if !strings.HasPrefix(strings.ToLower(in.Port.Name), strings.ToLower(matchCondition.PortNamePrefix)) {
			return false
		}
	}

	// Listener type
	switch matchCondition.ListenerType {
	case networking.EnvoyFilter_ListenerMatch_GATEWAY:
		if in.Node.Type != model.Router {
			return false // We don't care about direction for gateways
		}
	case networking.EnvoyFilter_ListenerMatch_SIDECAR_INBOUND:
		if in.Node.Type != model.Sidecar || direction != InboundDirection {
			return false
		}
	case networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND:
		if in.Node.Type != model.Sidecar || direction != OutboundDirection {
			return false
		}
		// case ANY implies do not care about proxy type or direction
	}

	// Listener protocol
	switch matchCondition.ListenerProtocol {
	case networking.EnvoyFilter_ListenerMatch_HTTP:
		if in.ListenerProtocol != plugin.ListenerProtocolHTTP {
			return false
		}
	case networking.EnvoyFilter_ListenerMatch_TCP:
		if in.ListenerProtocol != plugin.ListenerProtocolTCP {
			return false
		}
	}

	if len(matchCondition.Address) > 0 {
		if listenerIP == nil {
			// We failed to parse the listener IP address.
			// It could be a unix domain socket or something else.
			// Since we have some addresses to match against, this nil IP is considered as a mismatch
			return false
		}
		matched := false
		// if any of the addresses here match, return true
		for _, address := range matchCondition.Address {
			if strings.Contains(address, "/") {
				var ipNet *net.IPNet
				var err error
				if _, ipNet, err = net.ParseCIDR(address); err != nil {
					log.Warnf("Failed to parse address %s in EnvoyFilter: %v", address, err)
					continue
				}
				if ipNet.Contains(listenerIP) {
					matched = true
					break
				}
			} else if net.ParseIP(address).Equal(listenerIP) {
				matched = true
				break
			}
		}
		return matched
	}

	return true
}

func insertHTTPFilter(filterChain *plugin.FilterChain, envoyFilter *networking.EnvoyFilter_Filter) {
	filter := &http_conn.HttpFilter{
		Name:   envoyFilter.FilterName,
		Config: envoyFilter.FilterConfig,
	}

	position := networking.EnvoyFilter_InsertPosition_FIRST
	if envoyFilter.InsertPosition != nil {
		position = envoyFilter.InsertPosition.Index
	}

	switch position {
	case networking.EnvoyFilter_InsertPosition_FIRST, networking.EnvoyFilter_InsertPosition_BEFORE:
		filterChain.HTTP = append([]*http_conn.HttpFilter{filter}, filterChain.HTTP...)
		if position == networking.EnvoyFilter_InsertPosition_BEFORE {
			// bubble the filter to the right position scanning from beginning
			for i := 1; i < len(filterChain.HTTP); i++ {
				if filterChain.HTTP[i].Name != envoyFilter.InsertPosition.RelativeTo {
					filterChain.HTTP[i-1], filterChain.HTTP[i] = filterChain.HTTP[i], filterChain.HTTP[i-1]
				} else {
					break
				}
			}
		}
	case networking.EnvoyFilter_InsertPosition_LAST, networking.EnvoyFilter_InsertPosition_AFTER:
		filterChain.HTTP = append(filterChain.HTTP, filter)
		if position == networking.EnvoyFilter_InsertPosition_AFTER {
			// bubble the filter to the right position scanning from end
			for i := len(filterChain.HTTP) - 2; i >= 0; i-- {
				if filterChain.HTTP[i].Name != envoyFilter.InsertPosition.RelativeTo {
					filterChain.HTTP[i+1], filterChain.HTTP[i] = filterChain.HTTP[i], filterChain.HTTP[i+1]
				} else {
					break
				}
			}
		}
	}
}

func insertNetworkFilter(filterChain *plugin.FilterChain, envoyFilter *networking.EnvoyFilter_Filter) {
	filter := &listener.Filter{
		Name:   envoyFilter.FilterName,
		Config: envoyFilter.FilterConfig,
	}

	position := networking.EnvoyFilter_InsertPosition_FIRST
	if envoyFilter.InsertPosition != nil {
		position = envoyFilter.InsertPosition.Index
	}

	switch position {
	case networking.EnvoyFilter_InsertPosition_FIRST, networking.EnvoyFilter_InsertPosition_BEFORE:
		filterChain.TCP = append([]listener.Filter{*filter}, filterChain.TCP...)
		if position == networking.EnvoyFilter_InsertPosition_BEFORE {
			// bubble the filter to the right position scanning from beginning
			for i := 1; i < len(filterChain.TCP); i++ {
				if filterChain.TCP[i].Name != envoyFilter.InsertPosition.RelativeTo {
					filterChain.TCP[i-1], filterChain.TCP[i] = filterChain.TCP[i], filterChain.TCP[i-1]
				}
			}
		}
	case networking.EnvoyFilter_InsertPosition_LAST, networking.EnvoyFilter_InsertPosition_AFTER:
		filterChain.TCP = append(filterChain.TCP, *filter)
		if position == networking.EnvoyFilter_InsertPosition_AFTER {
			// bubble the filter to the right position scanning from end
			for i := len(filterChain.TCP) - 2; i >= 0; i-- {
				if filterChain.TCP[i].Name != envoyFilter.InsertPosition.RelativeTo {
					filterChain.TCP[i+1], filterChain.TCP[i] = filterChain.TCP[i], filterChain.TCP[i+1]
				}
			}
		}
	}

}
