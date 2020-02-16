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
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	mccpb "istio.io/istio/pilot/pkg/networking/plugin/mixer/client"
	mpb "istio.io/istio/pilot/pkg/networking/plugin/mixer/client"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/host"
)

type mixerplugin struct{}

type attribute = *mpb.Attributes_AttributeValue

type attributes map[string]attribute

const (
	// mixer filter name
	mixer = "mixer"

	// defaultConfig is the default service config (that does not correspond to an actual service)
	defaultConfig = "default"

	// force enable policy checks for both inbound and outbound calls
	policyCheckEnable = "enable"

	// force disable policy checks for both inbound and outbound calls
	policyCheckDisable = "disable"

	// force enable policy checks for both inbound and outbound calls, but fail open on errors
	policyCheckEnableAllow = "allow-on-error"

	// default number of retries for policy checks
	defaultRetries = 0
)

var (
	// default base retry wait time for policy checks
	defaultBaseRetryWaitTime = ptypes.DurationProto(80 * time.Millisecond)

	// default maximum wait time for policy checks
	defaultMaxRetryWaitTime = ptypes.DurationProto(1000 * time.Millisecond)
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

// proxyVersionToString converts IstioVersion to a semver format string.
func proxyVersionToString(v *model.IstioVersion) string {
	major := strconv.Itoa(v.Major)
	minor := strconv.Itoa(v.Minor)
	patch := strconv.Itoa(v.Patch)
	return strings.Join([]string{major, minor, patch}, ".")
}

func createOutboundListenerAttributes(in *plugin.InputParams) attributes {
	attrs := attributes{
		"source.uid":            attrUID(in.Node),
		"source.namespace":      attrNamespace(in.Node),
		"context.reporter.uid":  attrUID(in.Node),
		"context.reporter.kind": attrStringValue("outbound"),
	}
	if in.Node.IstioVersion != nil {
		vs := proxyVersionToString(in.Node.IstioVersion)
		attrs["context.proxy_version"] = attrStringValue(vs)
	}
	return attrs
}

func skipMixerHTTPFilter(dir direction, mesh *meshconfig.MeshConfig, node *model.Proxy) bool {
	return disablePolicyChecks(dir, mesh, node) && mesh.GetDisableMixerHttpReports()
}

// OnOutboundListener implements the Callbacks interface method.
func (mixerplugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Push.Mesh.MixerCheckServer == "" && in.Push.Mesh.MixerReportServer == "" {
		return nil
	}

	attrs := createOutboundListenerAttributes(in)

	skipHTTPFilter := skipMixerHTTPFilter(outbound, in.Push.Mesh, in.Node)

	switch in.ListenerProtocol {
	case plugin.ListenerProtocolHTTP:
		if skipHTTPFilter {
			return nil
		}
		httpFilter := buildOutboundHTTPFilter(in.Push.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
		}
		return nil
	case plugin.ListenerProtocolTCP:
		tcpFilter := buildOutboundTCPFilter(in.Push.Mesh, attrs, in.Node, in.Service)
		if in.Node.Type == model.Router {
			// For gateways, due to TLS termination, a listener marked as TCP could very well
			// be using a HTTP connection manager. So check the filterChain.listenerProtocol
			// to decide the type of filter to attach
			httpFilter := buildOutboundHTTPFilter(in.Push.Mesh, attrs, in.Node)
			for cnum := range mutable.FilterChains {
				if mutable.FilterChains[cnum].ListenerProtocol == plugin.ListenerProtocolHTTP && !skipHTTPFilter {
					mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
				} else {
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
				}
			}
		} else {
			for cnum := range mutable.FilterChains {
				if mutable.FilterChains[cnum].IsFallThrough {
					svc := util.FallThroughFilterChainBlackHoleService
					if util.IsAllowAnyOutbound(in.Node) {
						svc = util.FallThroughFilterChainPassthroughService
					}
					attrs := createOutboundListenerAttributes(in)
					fallThroughFilter := buildOutboundTCPFilter(in.Push.Mesh, attrs, in.Node, svc)
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, fallThroughFilter)
				} else {
					mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
				}
			}
		}
		return nil
	case plugin.ListenerProtocolAuto:
		tcpFilter := buildOutboundTCPFilter(in.Push.Mesh, attrs, in.Node, in.Service)
		httpFilter := buildOutboundHTTPFilter(in.Push.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			switch mutable.FilterChains[cnum].ListenerProtocol {
			case plugin.ListenerProtocolHTTP:
				if !skipHTTPFilter {
					mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
				}
			case plugin.ListenerProtocolTCP:
				mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
			}
		}
		return nil
	}

	return fmt.Errorf("unknown listener type %v in mixer.OnOutboundListener", in.ListenerProtocol)
}

// OnInboundListener implements the Callbacks interface method.
func (mixerplugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Push.Mesh.MixerCheckServer == "" && in.Push.Mesh.MixerReportServer == "" {
		return nil
	}

	attrs := attributes{
		"destination.uid":       attrUID(in.Node),
		"destination.namespace": attrNamespace(in.Node),
		"context.reporter.uid":  attrUID(in.Node),
		"context.reporter.kind": attrStringValue("inbound"),
	}
	if in.Node.IstioVersion != nil {
		vs := proxyVersionToString(in.Node.IstioVersion)
		attrs["context.proxy_version"] = attrStringValue(vs)
	}

	if len(in.Node.Metadata.MeshID) > 0 {
		attrs["destination.mesh.id"] = attrStringValue(in.Node.Metadata.MeshID)
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

	skipHTTPFilter := skipMixerHTTPFilter(inbound, in.Push.Mesh, in.Node)

	switch in.ListenerProtocol {
	case plugin.ListenerProtocolHTTP:
		if skipHTTPFilter {
			return nil
		}
		filter := buildInboundHTTPFilter(in.Push.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, filter)
		}
		return nil
	case plugin.ListenerProtocolTCP:
		filter := buildInboundTCPFilter(in.Push.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, filter)
		}
		return nil
	case plugin.ListenerProtocolAuto:
		httpFilter := buildInboundHTTPFilter(in.Push.Mesh, attrs, in.Node)
		tcpFilter := buildInboundTCPFilter(in.Push.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			switch mutable.FilterChains[cnum].ListenerProtocol {
			case plugin.ListenerProtocolHTTP:
				if !skipHTTPFilter {
					mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, httpFilter)
				}
			case plugin.ListenerProtocolTCP:
				mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
			}
		}

		return nil
	}

	return fmt.Errorf("unknown listener type %v in mixer.OnOutboundListener", in.ListenerProtocol)
}

// OnVirtualListener implements the Plugin interface method.
func (mixerplugin) OnVirtualListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Push.Mesh.MixerCheckServer == "" && in.Push.Mesh.MixerReportServer == "" {
		return nil
	}
	if in.ListenerProtocol == plugin.ListenerProtocolTCP {
		attrs := createOutboundListenerAttributes(in)
		tcpFilter := buildOutboundTCPFilter(in.Push.Mesh, attrs, in.Node, in.Service)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, tcpFilter)
		}
	}
	return nil
}

// OnOutboundCluster implements the Plugin interface method.
func (mixerplugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
	if !in.Push.Mesh.SidecarToTelemetrySessionAffinity {
		// if session affinity is not enabled, do nothing
		return
	}
	withoutPort := strings.Split(in.Push.Mesh.MixerReportServer, ":")
	if strings.Contains(cluster.Name, withoutPort[0]) {
		// config telemetry service discovery to be strict_dns for session affinity.
		// To enable session affinity, DNS needs to provide only one and the same telemetry instance IP
		// (e.g. in k8s, telemetry service spec needs to have SessionAffinity: ClientIP)
		cluster.ClusterDiscoveryType = &xdsapi.Cluster_Type{Type: xdsapi.Cluster_STRICT_DNS}
		addr := util.BuildAddress(in.Service.Address, uint32(in.Port.Port))
		cluster.LoadAssignment = &xdsapi.ClusterLoadAssignment{
			ClusterName: cluster.Name,
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{Address: addr},
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
	if in.Push.Mesh.MixerCheckServer == "" && in.Push.Mesh.MixerReportServer == "" {
		return
	}
	for i := 0; i < len(routeConfiguration.VirtualHosts); i++ {
		virtualHost := routeConfiguration.VirtualHosts[i]
		for j := 0; j < len(virtualHost.Routes); j++ {
			virtualHost.Routes[j] = modifyOutboundRouteConfig(in.Push, in, virtualHost.Name, virtualHost.Routes[j])
		}
		routeConfiguration.VirtualHosts[i] = virtualHost
	}
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (mixerplugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
	if in.Push.Mesh.MixerCheckServer == "" && in.Push.Mesh.MixerReportServer == "" {
		return
	}
	switch in.ListenerProtocol {
	case plugin.ListenerProtocolHTTP:
		// copy structs in place
		for i := 0; i < len(routeConfiguration.VirtualHosts); i++ {
			virtualHost := routeConfiguration.VirtualHosts[i]
			for j := 0; j < len(virtualHost.Routes); j++ {
				r := virtualHost.Routes[j]
				r.TypedPerFilterConfig = addTypedServiceConfig(r.TypedPerFilterConfig, buildInboundRouteConfig(in, in.ServiceInstance))
				virtualHost.Routes[j] = r
			}
			routeConfiguration.VirtualHosts[i] = virtualHost
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

// OnInboundPassthrough is called whenever a new passthrough filter chain is added to the LDS output.
func (mixerplugin) OnInboundPassthrough(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

// OnInboundPassthroughFilterChains is called for plugin to update the pass through filter chain.
func (mixerplugin) OnInboundPassthroughFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}

func buildUpstreamName(address string) string {
	// effectively disable the upstream
	if address == "" {
		return ""
	}

	hostname, port, _ := net.SplitHostPort(address)
	v, _ := strconv.Atoi(port)
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, "", host.Name(hostname), v)
}

func buildTransport(mesh *meshconfig.MeshConfig, node *model.Proxy) *mccpb.TransportConfig {
	// default to mesh
	policy := mccpb.NetworkFailPolicy_FAIL_CLOSE
	if mesh.PolicyCheckFailOpen {
		policy = mccpb.NetworkFailPolicy_FAIL_OPEN
	}

	// apply proxy-level overrides
	switch node.Metadata.PolicyCheck {
	case policyCheckEnable:
		policy = mccpb.NetworkFailPolicy_FAIL_CLOSE
	case policyCheckEnableAllow, policyCheckDisable:
		policy = mccpb.NetworkFailPolicy_FAIL_OPEN
	}

	networkFailPolicy := &mccpb.NetworkFailPolicy{Policy: policy}

	networkFailPolicy.MaxRetry = defaultRetries
	if len(node.Metadata.PolicyCheckRetries) > 0 {
		retries, err := strconv.Atoi(node.Metadata.PolicyCheckRetries)
		if err != nil {
			log.Warnf("unable to parse retry limit %q.", node.Metadata.PolicyCheckRetries)
		} else {
			networkFailPolicy.MaxRetry = uint32(retries)
		}
	}

	networkFailPolicy.BaseRetryWait = defaultBaseRetryWaitTime
	if len(node.Metadata.PolicyCheckBaseRetryWaitTime) > 0 {
		dur, err := time.ParseDuration(node.Metadata.PolicyCheckBaseRetryWaitTime)
		if err != nil {
			log.Warnf("unable to parse base retry wait time %q.", node.Metadata.PolicyCheckBaseRetryWaitTime)
		} else {
			networkFailPolicy.BaseRetryWait = ptypes.DurationProto(dur)
		}
	}

	networkFailPolicy.MaxRetryWait = defaultMaxRetryWaitTime
	if len(node.Metadata.PolicyCheckMaxRetryWaitTime) > 0 {
		dur, err := time.ParseDuration(node.Metadata.PolicyCheckMaxRetryWaitTime)
		if err != nil {
			log.Warnf("unable to parse max retry wait time %q.", node.Metadata.PolicyCheckMaxRetryWaitTime)
		} else {
			networkFailPolicy.MaxRetryWait = ptypes.DurationProto(dur)
		}
	}

	res := &mccpb.TransportConfig{
		CheckCluster:          buildUpstreamName(mesh.MixerCheckServer),
		ReportCluster:         buildUpstreamName(mesh.MixerReportServer),
		NetworkFailPolicy:     networkFailPolicy,
		ReportBatchMaxEntries: mesh.ReportBatchMaxEntries,
		ReportBatchMaxTime:    util.GogoDurationToDuration(mesh.ReportBatchMaxTime),
	}

	return res
}

func buildOutboundHTTPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) *http_conn.HttpFilter {
	cfg := &mccpb.HttpClientConfig{
		DefaultDestinationService: defaultConfig,
		ServiceConfigs: map[string]*mccpb.ServiceConfig{
			defaultConfig: {
				DisableCheckCalls:  disablePolicyChecks(outbound, mesh, node),
				DisableReportCalls: mesh.GetDisableMixerHttpReports(),
			},
		},
		MixerAttributes: &mpb.Attributes{Attributes: attrs},
		ForwardAttributes: &mpb.Attributes{Attributes: attributes{
			"source.uid": attrUID(node),
		}},
		Transport:                 buildTransport(mesh, node),
		IgnoreForwardedAttributes: node.Type == model.Router,
	}

	out := &http_conn.HttpFilter{
		Name:       mixer,
		ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(cfg)},
	}

	return out
}

func buildInboundHTTPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) *http_conn.HttpFilter {
	cfg := &mccpb.HttpClientConfig{
		DefaultDestinationService: defaultConfig,
		ServiceConfigs: map[string]*mccpb.ServiceConfig{
			defaultConfig: {
				DisableCheckCalls:  disablePolicyChecks(inbound, mesh, node),
				DisableReportCalls: mesh.GetDisableMixerHttpReports(),
			},
		},
		MixerAttributes:           &mpb.Attributes{Attributes: attrs},
		Transport:                 buildTransport(mesh, node),
		IgnoreForwardedAttributes: node.Type == model.Router,
	}
	out := &http_conn.HttpFilter{
		Name:       mixer,
		ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(cfg)},
	}

	return out
}

func addFilterConfigToRoute(in *plugin.InputParams, httpRoute *route.Route, attrs attributes) {
	httpRoute.TypedPerFilterConfig = addTypedServiceConfig(httpRoute.TypedPerFilterConfig, &mccpb.ServiceConfig{
		DisableCheckCalls:  disablePolicyChecks(outbound, in.Push.Mesh, in.Node),
		DisableReportCalls: in.Push.Mesh.GetDisableMixerHttpReports(),
		MixerAttributes:    &mpb.Attributes{Attributes: attrs},
		ForwardAttributes:  &mpb.Attributes{Attributes: attrs},
	})
}

func modifyOutboundRouteConfig(push *model.PushContext, in *plugin.InputParams, virtualHostname string, httpRoute *route.Route) *route.Route {
	// default config, to be overridden by per-weighted cluster
	httpRoute.TypedPerFilterConfig = addTypedServiceConfig(httpRoute.TypedPerFilterConfig, &mccpb.ServiceConfig{
		DisableCheckCalls:  disablePolicyChecks(outbound, in.Push.Mesh, in.Node),
		DisableReportCalls: in.Push.Mesh.GetDisableMixerHttpReports(),
	})
	switch action := httpRoute.Action.(type) {
	case *route.Route_Route:
		switch upstreams := action.Route.ClusterSpecifier.(type) {
		case *route.RouteAction_Cluster:
			_, _, hostname, _ := model.ParseSubsetKey(upstreams.Cluster)
			var attrs attributes
			if hostname == "" && upstreams.Cluster == util.PassthroughCluster {
				attrs = addVirtualDestinationServiceAttributes(make(attributes), util.PassthroughCluster)
			} else {
				svc := in.Node.SidecarScope.ServiceForHostname(hostname, push.ServiceByHostnameAndNamespace)
				attrs = addDestinationServiceAttributes(make(attributes), svc)
			}
			addFilterConfigToRoute(in, httpRoute, attrs)

		case *route.RouteAction_WeightedClusters:
			for _, weighted := range upstreams.WeightedClusters.Clusters {
				_, _, hostname, _ := model.ParseSubsetKey(weighted.Name)
				svc := in.Node.SidecarScope.ServiceForHostname(hostname, push.ServiceByHostnameAndNamespace)
				attrs := addDestinationServiceAttributes(make(attributes), svc)
				weighted.TypedPerFilterConfig = addTypedServiceConfig(weighted.TypedPerFilterConfig, &mccpb.ServiceConfig{
					DisableCheckCalls:  disablePolicyChecks(outbound, in.Push.Mesh, in.Node),
					DisableReportCalls: in.Push.Mesh.GetDisableMixerHttpReports(),
					MixerAttributes:    &mpb.Attributes{Attributes: attrs},
					ForwardAttributes:  &mpb.Attributes{Attributes: attrs},
				})
			}
		case *route.RouteAction_ClusterHeader:
		default:
			log.Warn("Unknown cluster type in mixer#OnOutboundRouteConfiguration")
		}
	// route.Route_DirectResponse is used for the BlackHole cluster configuration,
	// hence adding the attributes for the mixer filter
	case *route.Route_DirectResponse:
		if virtualHostname == util.BlackHoleRouteName {
			hostname := host.Name(util.BlackHoleCluster)
			attrs := addVirtualDestinationServiceAttributes(make(attributes), hostname)
			addFilterConfigToRoute(in, httpRoute, attrs)
		}
	// route.Route_Redirect is not used currently, so no attributes are added here
	case *route.Route_Redirect:
	default:
		log.Warn("Unknown route type in mixer#OnOutboundRouteConfiguration")
	}
	return httpRoute
}

func buildInboundRouteConfig(in *plugin.InputParams, instance *model.ServiceInstance) *mccpb.ServiceConfig {
	configStore := in.Push.IstioConfigStore

	attrs := addDestinationServiceAttributes(make(attributes), instance.Service)
	out := &mccpb.ServiceConfig{
		DisableCheckCalls:  disablePolicyChecks(inbound, in.Push.Mesh, in.Node),
		DisableReportCalls: in.Push.Mesh.GetDisableMixerHttpReports(),
		MixerAttributes:    &mpb.Attributes{Attributes: attrs},
	}

	if configStore != nil {
		quotaSpecs := configStore.QuotaSpecByDestination(instance)
		model.SortQuotaSpec(quotaSpecs)
		for _, quotaSpec := range quotaSpecs {
			bytes, _ := gogoproto.Marshal(quotaSpec.Spec)
			converted := &mccpb.QuotaSpec{}
			if err := proto.Unmarshal(bytes, converted); err != nil {
				log.Warnf("failing to convert from gogo to golang: %v", err)
				continue
			}
			out.QuotaSpec = append(out.QuotaSpec, converted)
		}
	}

	return out
}

func buildOutboundTCPFilter(mesh *meshconfig.MeshConfig, attrsIn attributes, node *model.Proxy, destination *model.Service) *listener.Filter {
	attrs := attrsCopy(attrsIn)
	if destination != nil {
		attrs = addDestinationServiceAttributes(attrs, destination)
	}

	cfg := &mccpb.TcpClientConfig{
		DisableCheckCalls: disablePolicyChecks(outbound, mesh, node),
		MixerAttributes:   &mpb.Attributes{Attributes: attrs},
		Transport:         buildTransport(mesh, node),
	}

	out := &listener.Filter{
		Name:       mixer,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(cfg)},
	}

	return out
}

func buildInboundTCPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) *listener.Filter {
	cfg := &mccpb.TcpClientConfig{
		DisableCheckCalls: disablePolicyChecks(inbound, mesh, node),
		MixerAttributes:   &mpb.Attributes{Attributes: attrs},
		Transport:         buildTransport(mesh, node),
	}
	out := &listener.Filter{
		Name:       mixer,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(cfg)},
	}

	return out
}

func addTypedServiceConfig(filterConfigs map[string]*any.Any, config *mccpb.ServiceConfig) map[string]*any.Any {
	if filterConfigs == nil {
		filterConfigs = make(map[string]*any.Any)
	}
	filterConfigs[mixer] = util.MessageToAny(config)
	return filterConfigs
}

func addVirtualDestinationServiceAttributes(attrs attributes, destinationServiceName host.Name) attributes {
	if destinationServiceName == util.PassthroughCluster || destinationServiceName == util.BlackHoleCluster {
		// Add destination service name for passthrough and blackhole cluster.
		attrs["destination.service.name"] = attrStringValue(string(destinationServiceName))
	}

	return attrs
}

func addDestinationServiceAttributes(attrs attributes, svc *model.Service) attributes {
	if svc == nil {
		return attrs
	}
	attrs["destination.service.host"] = attrStringValue(string(svc.Hostname))

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
	switch node.Metadata.PolicyCheck {
	case policyCheckDisable:
		disable = true
	case policyCheckEnable, policyCheckEnableAllow:
		disable = false
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
		return attrStringValue(parts[len(parts)-1])
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
