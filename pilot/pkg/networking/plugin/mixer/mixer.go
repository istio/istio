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

type attribute = *mpb.Attributes_AttributeValue

type attributes map[string]attribute

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

	attrs := attributes{
		"source.uid":             attrUID(in.Node),
		"context.reporter.uid":   attrUID(in.Node),
		"context.reporter.local": attrBoolValue(false),
	}

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		if in.Node.Type == model.Router {
			// TODO: design attributes for the gateway
			return nil
		}
		m := buildOutboundHTTPFilter(in.Env.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, m)
		}
		return nil
	case plugin.ListenerTypeTCP:
		m := buildOutboundTCPFilter(in.Env.Mesh, attrs, in.Node)
		for cnum := range mutable.FilterChains {
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

	attrs := attributes{
		"destination.uid":        attrUID(in.Node),
		"context.reporter.uid":   attrUID(in.Node),
		"context.reporter.local": attrBoolValue(true),
	}

	if mutable.Listener != nil {
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
	}

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		m := buildInboundHTTPFilter(in.Env.Mesh, in.Node, attrs, in.ProxyInstances)
		for cnum := range mutable.FilterChains {
			mutable.FilterChains[cnum].HTTP = append(mutable.FilterChains[cnum].HTTP, m)
		}
		return nil
	case plugin.ListenerTypeTCP:
		m := buildInboundTCPFilter(in.Env.Mesh, in.Node, attrs, in.ProxyInstances)
		for cnum := range mutable.FilterChains {
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
	// TODO: set destination service on routes
}

// OnInboundRouteConfiguration implements the Plugin interface method.
func (mixerplugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
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
				nr.PerFilterConfig[v1.MixerFilter] = util.MessageToStruct(buildMixerPerRouteConfig(in, in.ServiceInstance))
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

func buildMixerPerRouteConfig(in *plugin.InputParams, instance *model.ServiceInstance) *mccpb.ServiceConfig {
	config := in.Env.IstioConfigStore

	attrs := make(attributes)
	addDestinationServiceAttributes(attrs, instance.Service.Hostname.String(), in.Node.Domain)

	out := &mccpb.ServiceConfig{
		DisableCheckCalls: in.Env.Mesh.DisablePolicyChecks,
		MixerAttributes:   &mpb.Attributes{Attributes: attrs},
	}

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

	return out
}

func buildTransport(mesh *meshconfig.MeshConfig, uid attribute) *mccpb.TransportConfig {
	policy, _, _ := net.SplitHostPort(mesh.MixerCheckServer)
	telemetry, _, _ := net.SplitHostPort(mesh.MixerReportServer)

	port := mixerPortNumber
	if mesh.AuthPolicy == meshconfig.MeshConfig_MUTUAL_TLS {
		port = mixerMTLSPortNumber
	}

	return &mccpb.TransportConfig{
		CheckCluster:  model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(policy), port),
		ReportCluster: model.BuildSubsetKey(model.TrafficDirectionOutbound, "", model.Hostname(telemetry), port),
		// internal telemetry forwarding
		AttributesForMixerProxy: &mpb.Attributes{Attributes: attributes{
			"source.uid": uid,
		}},
	}
}

func buildOutboundHTTPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) *http_conn.HttpFilter {
	return &http_conn.HttpFilter{
		Name: mixer,
		Config: util.MessageToStruct(&mccpb.HttpClientConfig{
			MixerAttributes: &mpb.Attributes{Attributes: attrs},
			ForwardAttributes: &mpb.Attributes{Attributes: attributes{
				"source.uid": attrUID(node),
			}},
			DefaultDestinationService: UnknownDestination,
			ServiceConfigs: map[string]*mccpb.ServiceConfig{
				UnknownDestination: {
					DisableCheckCalls: true, /* TODO: enable client-side checks */
					MixerAttributes: &mpb.Attributes{Attributes: attributes{ // TODO: fall through destination servicec
						"destination.service": attrStringValue(UnknownDestination),
					}},
				},
			},
			Transport: buildTransport(mesh, attrUID(node)),
		}),
	}
}

func buildInboundHTTPFilter(mesh *meshconfig.MeshConfig, node *model.Proxy, attrs attributes, instances []*model.ServiceInstance) *http_conn.HttpFilter {
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
			Transport:       buildTransport(mesh, attrUID(node)),
		}),
	}
}
func buildOutboundTCPFilter(mesh *meshconfig.MeshConfig, attrs attributes, node *model.Proxy) listener.Filter {
	// TODO(rshriram): set destination service for TCP outbound requires parsing the struct config
	return listener.Filter{
		Name: mixer,
		Config: util.MessageToStruct(&mccpb.TcpClientConfig{
			DisableCheckCalls: true, /* TODO: enable client-side checks */
			MixerAttributes:   &mpb.Attributes{Attributes: attrs},
			Transport:         buildTransport(mesh, attrUID(node)),
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
			Transport:         buildTransport(mesh, attrUID(node)),
		}),
	}
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

func nameAndNamespace(serviceHostname, proxyDomain string) (name, namespace string) {
	domainParts := strings.SplitN(proxyDomain, ".", 2)
	if !strings.HasSuffix(serviceHostname, domainParts[1]) {
		return serviceHostname, ""
	}

	parts := strings.Split(serviceHostname, ".")
	if len(parts) > 1 {
		return parts[0], parts[1]
	}

	return serviceHostname, ""
}

func attrStringValue(value string) attribute {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: value}}
}

func attrUID(node *model.Proxy) attribute {
	return attrStringValue("kubernetes://" + node.ID)
}

func attrBoolValue(value bool) attribute {
	return &mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_BoolValue{BoolValue: value}}
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
