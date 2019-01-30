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

package mixer

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	e "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

type mixerplugin struct{}

type attribute = *mpb.Attributes_AttributeValue

type attributes map[string]attribute

const (
	// mixer filter name
	mixer = "mixer"

	// defaultConfig is the default service config (that does not correspond to an actual service)
	defaultConfig = "default"

	// workload-bound annotations for controlling mixerfilter
	policyCheckAnnotation = "policy.istio.io/check"

	// force enable policy checks for both inbound and outbound calls
	policyCheckEnable = "enable"

	// force disable policy checks for both inbound and outbound calls
	policyCheckDisable = "disable"

	// force enable policy checks for both inbound and outbound calls, but fail open on errors
	policyCheckEnableAllow = "allow-on-error"
)

type direction int

const (
	inbound direction = iota
	outbound
)

// NewPlugin returns an ptr to an initialized mixer.Plugin.
func NewPlugin() plugin.Plugin {
	return mixerplugin{}
}

// OnOutboundListener implements the Callbacks interface method.
func (mixerplugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Env.Mesh.MixerCheckServer == "" && in.Env.Mesh.MixerReportServer == "" {
		return nil
	}

	attrs := attributes{
		"source.uid":            attrUID(in.Node),
		"source.namespace":      attrNamespace(in.Node),
		"context.reporter.uid":  attrUID(in.Node),
		"context.reporter.kind": attrStringValue("outbound"),
	}

	switch in.ListenerProtocol {
	case plugin.ListenerProtocolHTTP:
		filter := buildOutboundHTTPFilter(in.Env.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
		}
		return nil
	case plugin.ListenerProtocolTCP:
		filter := buildOutboundTCPFilter(in.Env.Mesh, attrs, in.Node, in.Service, in.Push)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, filter)
		}
		return nil
	}

	return fmt.Errorf("unknown listener type %v in mixer.OnOutboundListener", in.ListenerProtocol)
}

// OnInboundListener implements the Callbacks interface method.
func (mixerplugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Env.Mesh.MixerCheckServer == "" && in.Env.Mesh.MixerReportServer == "" {
		return nil
	}

	attrs := attributes{
		"destination.uid":       attrUID(in.Node),
		"destination.namespace": attrNamespace(in.Node),
		"context.reporter.uid":  attrUID(in.Node),
		"context.reporter.kind": attrStringValue("inbound"),
	}

	switch address := mutable.Listener.Address.Address.(type) {
	case *core.Address_SocketAddress:
		if address != nil && address.SocketAddress != nil {
			attrs["destination.ip"] = attrIPValue(address.SocketAddress.Address)
			switch portSpec := address.SocketAddress.PortSpecifier.(type) {
			case *core.SocketAddress_PortValue:
				if portSpec != nil {
					attrs["destination.port"] = attrIntValue(int64(portSpec.PortValue))
				}
			}
		}
	}

	switch in.ListenerProtocol {
	case plugin.ListenerProtocolHTTP:
		filter := buildInboundHTTPFilter(in.Env.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
		}
		return nil
	case plugin.ListenerProtocolTCP:
		filter := buildInboundTCPFilter(in.Env.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, filter)
		}
		return nil
	}

	return fmt.Errorf("unknown listener type %v in mixer.OnOutboundListener", in.ListenerProtocol)
}

// OnOutboundCluster implements the Plugin interface method.
func (mixerplugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
	if !in.Env.Mesh.SidecarToTelemetrySessionAffinity {
		// if session affinity is not enabled, do nothing
		return
	}
	withoutPort := strings.Split(in.Env.Mesh.MixerReportServer, ":")
	if strings.Contains(cluster.Name, withoutPort[0]) {
		// config telemetry service discovery to be strict_dns for session affinity.
		// To enable session affinity, DNS needs to provide only one and the same telemetry instance IP
		// (e.g. in k8s, telemetry service spec needs to have SessionAffinity: ClientIP)
		cluster.Type = xdsapi.Cluster_STRICT_DNS
		addr := util.BuildAddress(in.Service.Address, uint32(in.Port.Port))
		cluster.LoadAssignment = &xdsapi.ClusterLoadAssignment{
			ClusterName: cluster.Name,
			Endpoints: []e.LocalityLbEndpoints{
				{
					LbEndpoints: []e.LbEndpoint{
						{
							HostIdentifier: &e.LbEndpoint_Endpoint{
								Endpoint: &e.Endpoint{Address: &addr},
							},
						},
					},
				},
			},
		}
		cluster.EdsClusterConfig = nil
	}
}

// OnInboundCluster implements the Plugin interface method.
func (mixerplugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
	// do nothing
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (mixerplugin) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
	if in.Env.Mesh.MixerCheckServer == "" && in.Env.Mesh.MixerReportServer == "" {
		return
	}
	for i := 0; i < len(routeConfiguration.VirtualHosts); i++ {
		host := routeConfiguration.VirtualHosts[i]
		for j := 0; j < len(host.Routes); j++ {
			host.Routes[j] = modifyOutboundRouteConfig(in.Push, in, host.Routes[j])
		}
		routeConfiguration.VirtualHosts[i] = host
	}
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (mixerplugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
	if in.Env.Mesh.MixerCheckServer == "" && in.Env.Mesh.MixerReportServer == "" {
		return
	}
	switch in.ListenerProtocol {
	case plugin.ListenerProtocolHTTP:
		// copy structs in place
		for i := 0; i < len(routeConfiguration.VirtualHosts); i++ {
			host := routeConfiguration.VirtualHosts[i]
			for j := 0; j < len(host.Routes); j++ {
				route := host.Routes[j]
				route.TypedPerFilterConfig = addServiceConfig(route.TypedPerFilterConfig, buildInboundRouteConfig(in.Push, in, in.ServiceInstance))
				host.Routes[j] = route
			}
			routeConfiguration.VirtualHosts[i] = host
		}

	case plugin.ListenerProtocolTCP:
	default:
		log.Warn("Unknown listener type in mixer#OnOutboundRouteConfiguration")
	}
}

// OnInboundFilterChains is called whenever a plugin needs to setup the filter chains, including relevant filter chain configuration.
func (mixerplugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}

func buildUpstreamName(address string) string {
	// effectively disable the upstream
	if address == "" {
		return ""
	}

	host, port, _ := net.SplitHostPort(address)
	v, _ := strconv.Atoi(port)
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(host), v)
}

func buildTransport(mesh *meshconfig.MeshConfig, node *model.Proxy) *mccpb.TransportConfig {
	// default to mesh
	networkFailPolicy := mccpb.FAIL_CLOSE
	if mesh.PolicyCheckFailOpen {
		networkFailPolicy = mccpb.FAIL_OPEN
	}

	// apply proxy-level overrides
	if policy, ok := node.Metadata[policyCheckAnnotation]; ok {
		switch policy {
		case policyCheckEnable:
			networkFailPolicy = mccpb.FAIL_CLOSE
		case policyCheckEnableAllow, policyCheckDisable:
			networkFailPolicy = mccpb.FAIL_OPEN
		}
	}

	res := &mccpb.TransportConfig{
		CheckCluster:      buildUpstreamName(mesh.MixerCheckServer),
		ReportCluster:     buildUpstreamName(mesh.MixerReportServer),
		NetworkFailPolicy: &mccpb.NetworkFailPolicy{Policy: networkFailPolicy},
	}

	return res
}

func buildOutboundHTTPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) *http_conn.HttpFilter {
	return &http_conn.HttpFilter{
		Name: mixer,
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&mccpb.HttpClientConfig{
				DefaultDestinationService: defaultConfig,
				ServiceConfigs: map[string]*mccpb.ServiceConfig{
					defaultConfig: {
						DisableCheckCalls: disablePolicyChecks(outbound, mesh, node),
					},
				},
				MixerAttributes: &mpb.Attributes{Attributes: attrs},
				ForwardAttributes: &mpb.Attributes{Attributes: attributes{
					"source.uid": attrUID(node),
				}},
				Transport: buildTransport(mesh, node),
			}),
		},
	}
}

func buildInboundHTTPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) *http_conn.HttpFilter {
	return &http_conn.HttpFilter{
		Name: mixer,
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&mccpb.HttpClientConfig{
				DefaultDestinationService: defaultConfig,
				ServiceConfigs: map[string]*mccpb.ServiceConfig{
					defaultConfig: {
						DisableCheckCalls: disablePolicyChecks(inbound, mesh, node),
					},
				},
				MixerAttributes: &mpb.Attributes{Attributes: attrs},
				Transport:       buildTransport(mesh, node),
			}),
		},
	}
}

func modifyOutboundRouteConfig(push *model.PushContext, in *plugin.InputParams, httpRoute route.Route) route.Route {
	// default config, to be overridden by per-weighted cluster
	httpRoute.TypedPerFilterConfig = addServiceConfig(httpRoute.TypedPerFilterConfig, &mccpb.ServiceConfig{
		DisableCheckCalls: disablePolicyChecks(outbound, in.Env.Mesh, in.Node),
	})
	switch action := httpRoute.Action.(type) {
	case *route.Route_Route:
		switch upstreams := action.Route.ClusterSpecifier.(type) {
		case *route.RouteAction_Cluster:
			_, _, hostname, _ := model.ParseSubsetKey(upstreams.Cluster)
			attrs := addDestinationServiceAttributes(make(attributes), push, hostname)
			httpRoute.TypedPerFilterConfig = addServiceConfig(httpRoute.TypedPerFilterConfig, &mccpb.ServiceConfig{
				DisableCheckCalls: disablePolicyChecks(outbound, in.Env.Mesh, in.Node),
				MixerAttributes:   &mpb.Attributes{Attributes: attrs},
				ForwardAttributes: &mpb.Attributes{Attributes: attrs},
			})
		case *route.RouteAction_WeightedClusters:
			for _, weighted := range upstreams.WeightedClusters.Clusters {
				_, _, hostname, _ := model.ParseSubsetKey(weighted.Name)
				attrs := addDestinationServiceAttributes(make(attributes), push, hostname)
				weighted.TypedPerFilterConfig = addServiceConfig(weighted.TypedPerFilterConfig, &mccpb.ServiceConfig{
					DisableCheckCalls: disablePolicyChecks(outbound, in.Env.Mesh, in.Node),
					MixerAttributes:   &mpb.Attributes{Attributes: attrs},
					ForwardAttributes: &mpb.Attributes{Attributes: attrs},
				})
			}
		case *route.RouteAction_ClusterHeader:
		default:
			log.Warn("Unknown cluster type in mixer#OnOutboundRouteConfiguration")
		}
	case *route.Route_Redirect, *route.Route_DirectResponse:
	default:
		log.Warn("Unknown route type in mixer#OnOutboundRouteConfiguration")
	}
	return httpRoute
}

func buildInboundRouteConfig(push *model.PushContext, in *plugin.InputParams, instance *model.ServiceInstance) *mccpb.ServiceConfig {
	config := in.Env.IstioConfigStore

	attrs := addDestinationServiceAttributes(make(attributes), push, instance.Service.Hostname)
	out := &mccpb.ServiceConfig{
		DisableCheckCalls: disablePolicyChecks(inbound, in.Env.Mesh, in.Node),
		MixerAttributes:   &mpb.Attributes{Attributes: attrs},
	}

	if config != nil {
		apiSpecs := config.HTTPAPISpecByDestination(instance)
		model.SortHTTPAPISpec(apiSpecs)
		for _, config := range apiSpecs {
			out.HttpApiSpec = append(out.HttpApiSpec, config.Spec.(*mccpb.HTTPAPISpec))
		}

		quotaSpecs := config.QuotaSpecByDestination(instance)
		model.SortQuotaSpec(quotaSpecs)
		for _, config := range quotaSpecs {
			out.QuotaSpec = append(out.QuotaSpec, config.Spec.(*mccpb.QuotaSpec))
		}
	}

	return out
}

func buildOutboundTCPFilter(mesh *meshconfig.MeshConfig, attrsIn attributes, node *model.Proxy, destination *model.Service,
	push *model.PushContext) listener.Filter {
	attrs := attrsCopy(attrsIn)
	if destination != nil {
		attrs = addDestinationServiceAttributes(attrs, push, destination.Hostname)
	}
	return listener.Filter{
		Name: mixer,
		ConfigType: &listener.Filter_TypedConfig{
			TypedConfig: util.MessageToAny(&mccpb.TcpClientConfig{
				DisableCheckCalls: disablePolicyChecks(outbound, mesh, node),
				MixerAttributes:   &mpb.Attributes{Attributes: attrs},
				Transport:         buildTransport(mesh, node),
			}),
		},
	}
}

func buildInboundTCPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) listener.Filter {
	return listener.Filter{
		Name: mixer,
		ConfigType: &listener.Filter_TypedConfig{
			TypedConfig: util.MessageToAny(&mccpb.TcpClientConfig{
				DisableCheckCalls: disablePolicyChecks(outbound, mesh, node),
				MixerAttributes:   &mpb.Attributes{Attributes: attrs},
				Transport:         buildTransport(mesh, node),
			}),
		},
	}
}

func addServiceConfig(filterConfigs map[string]*types.Any, config *mccpb.ServiceConfig) map[string]*types.Any {
	if filterConfigs == nil {
		filterConfigs = make(map[string]*types.Any)
	}
	filterConfigs[mixer] = util.MessageToAny(config)
	return filterConfigs
}

func addDestinationServiceAttributes(attrs attributes, push *model.PushContext, destinationHostname model.Hostname) attributes {
	// TODO: pass Service directly.
	if destinationHostname == "" {
		return attrs
	}
	attrs["destination.service.host"] = attrStringValue(string(destinationHostname))

	svc := push.ServiceByHostname[destinationHostname]
	if svc == nil {
		return attrs
	}
	serviceAttributes := svc.Attributes
	if serviceAttributes.Name != "" {
		attrs["destination.service.name"] = attrStringValue(serviceAttributes.Name)
	}
	if serviceAttributes.Namespace != "" {
		attrs["destination.service.namespace"] = attrStringValue(serviceAttributes.Namespace)
	}
	if serviceAttributes.UID != "" {
		attrs["destination.service.uid"] = attrStringValue(serviceAttributes.UID)
	}
	return attrs
}

func disableClientPolicyChecks(mesh *meshconfig.MeshConfig, node *model.Proxy) bool {
	if mesh.DisablePolicyChecks {
		return true
	}
	if node.Type == model.Router {
		return false
	}
	if mesh.EnableClientSidePolicyCheck {
		return false
	}
	return true
}

func disablePolicyChecks(dir direction, mesh *meshconfig.MeshConfig, node *model.Proxy) (disable bool) {
	// default to mesh settings
	switch dir {
	case inbound:
		disable = mesh.DisablePolicyChecks
	case outbound:
		disable = disableClientPolicyChecks(mesh, node)
	}

	// override with proxy settings
	if policy, ok := node.Metadata[policyCheckAnnotation]; ok {
		switch policy {
		case policyCheckDisable:
			disable = true
		case policyCheckEnable, policyCheckEnableAllow:
			disable = false
		}
	}
	return
}

func attrStringValue(value string) attribute {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: value}}
}

func attrUID(node *model.Proxy) attribute {
	return attrStringValue("kubernetes://" + node.ID)
}

func attrNamespace(node *model.Proxy) attribute {
	parts := strings.Split(node.ID, ".")
	if len(parts) >= 2 {
		return attrStringValue(parts[1])
	}
	return attrStringValue("")
}

func attrIntValue(value int64) attribute {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_Int64Value{Int64Value: value}}
}

func attrIPValue(ip string) attribute {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_BytesValue{BytesValue: net.ParseIP(ip)}}
}

func attrsCopy(attrs attributes) attributes {
	out := make(attributes)
	for k, v := range attrs {
		out[k] = v
	}
	return out
}
