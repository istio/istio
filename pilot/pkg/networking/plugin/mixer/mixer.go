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
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
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
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pkg/log"
)

type mixerplugin struct{}

type attributes map[string]*mpb.Attributes_AttributeValue

const (
	// UnknownDestination is set for cases when the destination service cannot be determined.
	// This is necessary since Mixer scopes config by the destination service.
	UnknownDestination = "unknown"

	//mixerPortName       = "grpc-mixer"
	// defined in install/kubernetes/helm/istio/charts/mixer/templates/service.yaml
	mixerPortNumber = 9091

	//mixerMTLSPortName   = "grpc-mixer-mtls"
	mixerMTLSPortNumber = 15004

	// mixer filter name
	mixer = "mixer"
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

	env := in.Env
	node := in.Node
	proxyInstances := in.ProxyInstances

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		for cnum := range mutable.FilterChains {
			m := buildMixerHTTPFilter(env, node, proxyInstances, true)
			if m != nil {
				mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, m)
			}
		}
		return nil
	case plugin.ListenerTypeTCP:
		for cnum := range mutable.FilterChains {
			m := buildOutboundTCPFilter(in.Env.Mesh, in.Node)
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, m)
		}
		return nil
	}

	return fmt.Errorf("unknown listener type %v in mixer.OnOutboundListener", in.ListenerType)
}

// OnInboundListener implements the Callbacks interface method.
func (mixerplugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	if in.Env.Mesh.MixerCheckServer == "" && in.Env.Mesh.MixerReportServer == "" {
		return nil
	}

	ip := ""
	port := uint32(0)
	if mutable.Listener != nil {
		switch address := mutable.Listener.Address.Address.(type) {
		case *core.Address_SocketAddress:
			if address != nil && address.SocketAddress != nil {
				ip = address.SocketAddress.Address
				switch portSpec := address.SocketAddress.PortSpecifier.(type) {
				case *core.SocketAddress_PortValue:
					if portSpec != nil {
						port = portSpec.PortValue
					}
				}
			}
		}
	}

	uid := "kubernetes://" + in.Node.ID
	attrs := attributes{
		"destination.uid":        attrStringValue(uid),
		"context.reporter.uid":   attrStringValue(uid),
		"context.reporter.local": attrBoolValue(true),
	}
	if len(ip) > 0 {
		attrs["destination.ip"] = attrIPValue(ip)
	}
	if port != 0 {
		attrs["destination.port"] = attrIntValue(int64(port))
	}
	// TODO(rshriram): labels should be pod labels in the proxy node
	instances := in.ProxyInstances
	if len(instances) > 0 {
		attrs["destination.labels"] = attrMapValue(instances[0].Labels)
	}

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		for cnum := range mutable.FilterChains {
			m := buildInboundHTTPFilter(in.Env.Mesh, in.Node, attrs, in.ProxyInstances)
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, m)
		}
		return nil
	case plugin.ListenerTypeTCP:
		for cnum := range mutable.FilterChains {
			m := buildInboundTCPFilter(in.Env.Mesh, in.Node, attrs, in.ProxyInstances)
			mutable.FilterChains[cnum].TCP = append(mutable.FilterChains[cnum].TCP, m)
		}
		return nil
	}

	return fmt.Errorf("unknown listener type %v in mixer.OnOutboundListener", in.ListenerType)
}

// OnOutboundCluster implements the Plugin interface method.
func (mixerplugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port, cluster *xdsapi.Cluster) {
	// do nothing
}

// OnInboundCluster implements the Plugin interface method.
func (mixerplugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port, cluster *xdsapi.Cluster) {
	// do nothing
}

// OnOutboundRouteConfiguration implements the Plugin interface method.
func (mixerplugin) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

// oc := BuildMixerConfig(node, serviceName, dest, proxyInstances, config, mesh.DisablePolicyChecks, false)
// func BuildMixerConfig(source model.Proxy, destName string, dest *model.Service, instances []*model.ServiceInstance, config model.IstioConfigStore,

// OnInboundRouteConfiguration implements the Plugin interface method.
func (mixerplugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
	forward := false
	if in.Node.Type == model.Ingress {
		forward = true
	}

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		var nvhs []route.VirtualHost
		for _, vh := range routeConfiguration.VirtualHosts {
			nvh := vh
			var nrs []route.Route
			for _, r := range vh.Routes {
				nr := r
				if nr.PerFilterConfig == nil {
					nr.PerFilterConfig = make(map[string]*types.Struct)
				}
				nr.PerFilterConfig[v1.MixerFilter] = util.MessageToStruct(
					buildMixerPerRouteConfig(in, false, forward, in.ServiceInstance.Service.Hostname.String()))
				nrs = append(nrs, nr)
			}
			nvh.Routes = nrs
			nvhs = append(nvhs, nvh)
		}
		routeConfiguration.VirtualHosts = nvhs

	case plugin.ListenerTypeTCP:
		// TODO: implement
	default:
		log.Warn("Unknown listener type in mixer#OnOutboundRouteConfiguration")
	}
}

func buildMixerPerRouteConfig(in *plugin.InputParams, outboundRoute bool, _ /*disableForward*/ bool, destinationService string) *mccpb.ServiceConfig {
	role := in.Node
	nodeInstances := in.ProxyInstances
	disableCheck := in.Env.Mesh.DisablePolicyChecks
	config := in.Env.IstioConfigStore

	out := serviceConfig(in.Service.Hostname.String(), in.ServiceInstance, config, disableCheck, false, role.Domain)
	// Report calls are never disabled. Disable forward is currently not in the proto.
	out.DisableCheckCalls = disableCheck

	if destinationService != "" {
		out.MixerAttributes = &mpb.Attributes{}
		out.MixerAttributes.Attributes = map[string]*mpb.Attributes_AttributeValue{
			v1.AttrDestinationService: {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: destinationService}},
		}
		addDestinationServiceAttributes(out.MixerAttributes.Attributes, destinationService, role.Domain)
	}

	var labels map[string]string
	// Note: instances are all running on mode.Node named 'role'
	// So instance labels are the workload / Node labels.
	if len(nodeInstances) > 0 {
		labels = nodeInstances[0].Labels
	}

	if !outboundRoute || role.Type == model.Router {
		// for outboundRoutes there are no default MixerAttributes except for gateway.
		// specific MixerAttributes are in per route configuration.
		v1.AddStandardNodeAttributes(out.MixerAttributes.Attributes, v1.AttrDestinationPrefix, role.IPAddress, role.ID, labels)
	}

	return out
}

// buildMixerHTTPFilter builds a filter with a v1 mixer config encapsulated as JSON in a proto.Struct for v2 consumption.
func buildMixerHTTPFilter(env *model.Environment, node *model.Proxy,
	proxyInstances []*model.ServiceInstance, outbound bool) *http_conn.HttpFilter {
	mesh := env.Mesh
	config := env.IstioConfigStore

	c := buildHTTPMixerFilterConfig(mesh, *node, proxyInstances, outbound, config)
	return &http_conn.HttpFilter{
		Name:   v1.MixerFilter,
		Config: util.MessageToStruct(c),
	}
}

func buildTransport(mesh *meshconfig.MeshConfig, uid string) *mccpb.TransportConfig {
	mcs, _, _ := net.SplitHostPort(mesh.MixerCheckServer)
	mrs, _, _ := net.SplitHostPort(mesh.MixerReportServer)

	port := mixerPortNumber
	if mesh.AuthPolicy == meshconfig.MeshConfig_MUTUAL_TLS {
		port = mixerMTLSPortNumber
	}

	return &mccpb.TransportConfig{
		CheckCluster:  model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(mcs), port),
		ReportCluster: model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(mrs), port),
		// internal telemetry forwarding
		AttributesForMixerProxy: &mpb.Attributes{
			Attributes: attributes{
				"source.uid": attrStringValue(uid),
			},
		},
	}
}

func buildInboundHTTPFilter(mesh *meshconfig.MeshConfig, node *model.Proxy, attrs attributes, instances []*model.ServiceInstance) *http_conn.HttpFilter {
	uid := "kubernetes://" + node.ID

	// TODO(rshriram): pick a single service for TCP destination workload. This needs to be removed once mixer config resolution
	// can scope by workload namespace. The choice of destination service should not be made here!
	destination := UnknownDestination
	if len(instances) > 0 {
		destination = instances[0].Service.Hostname.String()
	}
	serviceAttrs := make(attributes)
	addDestinationServiceAttributes(serviceAttrs, destination, node.Domain)

	return &http_conn.HttpFilter{
		Name: mixer,
		Config: util.MessageToStruct(&mccpb.HttpClientConfig{
			DefaultDestinationService: UnknownDestination,
			ServiceConfigs: map[string]*mccpb.ServiceConfig{
				UnknownDestination: {
					DisableCheckCalls: mesh.DisablePolicyChecks,
					MixerAttributes:   &mpb.Attributes{Attributes: serviceAttrs},
				},
			},
			MixerAttributes: &mpb.Attributes{Attributes: attrs},
			Transport:       buildTransport(mesh, uid),
		}),
	}
}
func buildOutboundTCPFilter(mesh *meshconfig.MeshConfig, node *model.Proxy) listener.Filter {
	uid := "kubernetes://" + node.ID
	// TODO(rshriram): destination service for TCP outbound requires parsing the struct config
	attrs := attributes{
		"source.uid":             attrStringValue(uid),
		"context.reporter.uid":   attrStringValue(uid),
		"context.reporter.local": attrBoolValue(false),
	}
	return listener.Filter{
		Name: mixer,
		Config: util.MessageToStruct(&mccpb.TcpClientConfig{
			DisableCheckCalls: true, /* TODO: enable client-side checks */
			MixerAttributes:   &mpb.Attributes{Attributes: attrs},
			Transport:         buildTransport(mesh, uid),
		}),
	}
}

func buildInboundTCPFilter(mesh *meshconfig.MeshConfig, node *model.Proxy, attrsIn attributes, instances []*model.ServiceInstance) listener.Filter {
	attrs := attrsCopy(attrsIn)
	// TODO(rshriram): pick a single service for TCP destination workload. This needs to be removed once mixer config resolution
	// can scope by workload namespace. The choice of destination service should not be made here!
	destination := UnknownDestination
	if len(instances) > 0 {
		destination = instances[0].Service.Hostname.String()
	}

	addDestinationServiceAttributes(attrs, destination, node.Domain)

	return listener.Filter{
		Name: mixer,
		Config: util.MessageToStruct(&mccpb.TcpClientConfig{
			DisableCheckCalls: mesh.DisablePolicyChecks,
			MixerAttributes:   &mpb.Attributes{Attributes: attrs},
			Transport:         buildTransport(mesh, "kubernetes://"+node.ID),
		}),
	}
}

// buildHTTPMixerFilterConfig builds a mixer HTTP filter config. Mixer filter uses outbound configuration by default
// (forward attributes, but not invoke check calls)  ServiceInstances belong to the Node.
func buildHTTPMixerFilterConfig(mesh *meshconfig.MeshConfig, role model.Proxy, nodeInstances []*model.ServiceInstance, outboundRoute bool, config model.IstioConfigStore) *mccpb.HttpClientConfig { // nolint: lll
	mcs, _, _ := net.SplitHostPort(mesh.MixerCheckServer)
	mrs, _, _ := net.SplitHostPort(mesh.MixerReportServer)

	port := mixerPortNumber
	if mesh.AuthPolicy == meshconfig.MeshConfig_MUTUAL_TLS {
		port = mixerMTLSPortNumber
	}

	// TODO: derive these port types.
	transport := &mccpb.TransportConfig{
		CheckCluster:  model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(mcs), port),
		ReportCluster: model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(mrs), port),
	}

	mxConfig := &mccpb.HttpClientConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"context.reporter.uid": {
					Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: "kubernetes://" + role.ID},
				},
			},
		},
		ServiceConfigs: map[string]*mccpb.ServiceConfig{},
		Transport:      transport,
	}

	var labels map[string]string
	// Note: instances are all running on mode.Node named 'role'
	// So instance labels are the workload / Node labels.
	if len(nodeInstances) > 0 {
		labels = nodeInstances[0].Labels
		mxConfig.DefaultDestinationService = nodeInstances[0].Service.Hostname.String()
	}

	if !outboundRoute || role.Type == model.Router {
		// for outboundRoutes there are no default MixerAttributes except for gateway.
		// specific MixerAttributes are in per route configuration.
		v1.AddStandardNodeAttributes(mxConfig.MixerAttributes.Attributes, v1.AttrDestinationPrefix, role.IPAddress, role.ID, labels)
		mxConfig.MixerAttributes.Attributes["context.reporter.local"] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_BoolValue{BoolValue: true},
		}
	}

	if role.Type == model.Sidecar && !outboundRoute {
		// Don't forward mixer attributes to the app from inbound sidecar routes
	} else {
		mxConfig.ForwardAttributes = &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{},
		}
		addStandardNodeAttributes(mxConfig.ForwardAttributes.Attributes, v1.AttrSourcePrefix, role.IPAddress, role.ID, labels)
	}

	// gateway case is special because upstream listeners are considered outbound, however we don't want to
	// automatically disable policy / report.
	disablePolicy := (outboundRoute && role.Type != model.Router) || mesh.DisablePolicyChecks
	disableReport := outboundRoute && role.Type != model.Router

	for _, instance := range nodeInstances {
		mxConfig.ServiceConfigs[instance.Service.Hostname.String()] =
			serviceConfig(instance.Service.Hostname.String(), instance, config, disablePolicy, disableReport, role.Domain)
	}

	return mxConfig
}

// set ip, uid, and labels attributes
func addStandardNodeAttributes(attr map[string]*mpb.Attributes_AttributeValue, prefix string, IPAddress string, ID string, labels map[string]string) {
	if len(IPAddress) > 0 {
		attr[prefix+".ip"] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_BytesValue{BytesValue: net.ParseIP(IPAddress)},
		}
	}

	attr[prefix+".uid"] = attrStringValue("kubernetes://" + ID)

	if len(labels) > 0 {
		attr[prefix+".labels"] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_StringMapValue{
				StringMapValue: &mpb.Attributes_StringMap{Entries: labels},
			},
		}
	}
}

// borrows heavily from v1.ServiceConfig (which this replaces)
func serviceConfig(serviceHostname string, dest *model.ServiceInstance, config model.IstioConfigStore, disableCheck, disableReport bool, proxyDomain string) *mccpb.ServiceConfig { // nolint: lll
	sc := &mccpb.ServiceConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"destination.service": attrStringValue(serviceHostname),
			},
		},
		DisableCheckCalls:  disableCheck,
		DisableReportCalls: disableReport,
	}

	if len(dest.Labels) > 0 {
		sc.MixerAttributes.Attributes["destination.labels"] = &mpb.Attributes_AttributeValue{
			Value: &mpb.Attributes_AttributeValue_StringMapValue{
				StringMapValue: &mpb.Attributes_StringMap{Entries: dest.Labels},
			},
		}
	}

	apiSpecs := config.HTTPAPISpecByDestination(dest)
	model.SortHTTPAPISpec(apiSpecs)
	for _, config := range apiSpecs {
		sc.HttpApiSpec = append(sc.HttpApiSpec, config.Spec.(*mccpb.HTTPAPISpec))
	}

	quotaSpecs := config.QuotaSpecByDestination(dest)
	model.SortQuotaSpec(quotaSpecs)
	for _, config := range quotaSpecs {
		sc.QuotaSpec = append(sc.QuotaSpec, config.Spec.(*mccpb.QuotaSpec))
	}

	addDestinationServiceAttributes(sc.MixerAttributes.Attributes, serviceHostname, proxyDomain)
	return sc
}

func addDestinationServiceAttributes(attrs attributes, destinationHostname, domain string) {
	svcName, svcNamespace := nameAndNamespace(destinationHostname, domain)
	attrs["destination.service"] = attrStringValue(destinationHostname) // DEPRECATED. Remove when fully out of use.
	attrs["destination.service.host"] = attrStringValue(destinationHostname)
	attrs["destination.service.uid"] = attrStringValue(fmt.Sprintf("istio://%s/services/%s", svcNamespace, svcName))
	attrs["destination.service.name"] = attrStringValue(svcName)
	if len(svcNamespace) > 0 {
		attrs["destination.service.namespace"] = attrStringValue(svcNamespace)
	}
}

func nameAndNamespace(serviceHostname, domain string) (name, namespace string) {
	domainParts := strings.SplitN(domain, ".", 2)
	if !strings.HasSuffix(serviceHostname, domainParts[1]) {
		return serviceHostname, ""
	}

	parts := strings.Split(serviceHostname, ".")
	if len(parts) > 1 {
		return parts[0], parts[1]
	}

	return serviceHostname, ""
}

func attrStringValue(value string) *mpb.Attributes_AttributeValue {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: value}}
}

func attrBoolValue(value bool) *mpb.Attributes_AttributeValue {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_BoolValue{BoolValue: value}}
}

func attrIntValue(value int64) *mpb.Attributes_AttributeValue {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_Int64Value{Int64Value: value}}
}

func attrIPValue(ip string) *mpb.Attributes_AttributeValue {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_BytesValue{BytesValue: net.ParseIP(ip)}}
}

func attrMapValue(kv map[string]string) *mpb.Attributes_AttributeValue {
	return &mpb.Attributes_AttributeValue{
		Value: &mpb.Attributes_AttributeValue_StringMapValue{
			StringMapValue: &mpb.Attributes_StringMap{Entries: kv},
		},
	}
}

func attrsCopy(attrs attributes) attributes {
	out := make(attributes)
	for k, v := range attrs {
		out[k] = v
	}
	return out
}
