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

package route

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	xdsfault "github.com/envoyproxy/go-control-plane/envoy/config/filter/fault/v2"
	xdshttpfault "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/fault/v2"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route/retry"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

// Headers with special meaning in Envoy
const (
	HeaderMethod    = ":method"
	HeaderAuthority = ":authority"
	HeaderScheme    = ":scheme"
)

// VirtualHostWrapper is a context-dependent virtual host entry with guarded routes.
// Note: Currently we are not fully utilizing this structure. We could invoke this logic
// once for all sidecars in the cluster to compute all RDS for inside the mesh and arrange
// it by listener port. However to properly use such an optimization, we need to have an
// eventing subsystem to invalidate the computed routes if any service changes/virtual services change.
type VirtualHostWrapper struct {
	// Port is the listener port for outbound sidecar (e.g. service port)
	Port int

	// Services are the services from the registry. Each service
	// in this list should have a virtual host entry
	Services []*model.Service

	// VirtualServiceHosts is a list of hosts defined in the virtual service
	// if virtual service hostname is same as a the service registry host, then
	// the host would appear in Services as we need to generate all variants of the
	// service's hostname within a platform (e.g., foo, foo.default, foo.default.svc, etc.)
	VirtualServiceHosts []string

	// Routes in the virtual host
	Routes []route.Route
}

// BuildSidecarVirtualHostsFromConfigAndRegistry creates virtual hosts from
// the given set of virtual services and a list of services from the
// service registry. Services are indexed by FQDN hostnames.
func BuildSidecarVirtualHostsFromConfigAndRegistry(
	node *model.Proxy,
	push *model.PushContext,
	serviceRegistry map[model.Hostname]*model.Service,
	proxyLabels model.LabelsCollection,
	virtualServices []model.Config, listenPort int) []VirtualHostWrapper {

	out := make([]VirtualHostWrapper, 0)

	// translate all virtual service configs into virtual hosts
	for _, virtualService := range virtualServices {
		wrappers := buildSidecarVirtualHostsForVirtualService(node, push, virtualService, serviceRegistry, proxyLabels, listenPort)
		if len(wrappers) == 0 {
			// If none of the routes matched by source (i.e. proxyLabels), then discard this entire virtual service
			continue
		}
		out = append(out, wrappers...)
	}

	// compute services missing virtual service configs
	missing := make(map[model.Hostname]bool)
	for fqdn := range serviceRegistry {
		missing[fqdn] = true
	}
	for _, host := range out {
		for _, service := range host.Services {
			delete(missing, service.Hostname)
		}
	}

	// append default hosts for the service missing virtual services
	for fqdn := range missing {
		svc := serviceRegistry[fqdn]
		for _, port := range svc.Ports {
			if port.Protocol.IsHTTP() {
				cluster := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", svc.Hostname, port.Port)
				traceOperation := fmt.Sprintf("%s:%d/*", svc.Hostname, port.Port)
				out = append(out, VirtualHostWrapper{
					Port:     port.Port,
					Services: []*model.Service{svc},
					Routes:   []route.Route{*BuildDefaultHTTPOutboundRoute(cluster, traceOperation)},
				})
			}
		}
	}

	return out
}

// separateVSHostsAndServices splits the virtual service hosts into services (if they are found in the registry) and
// plain non-registry hostnames
func separateVSHostsAndServices(virtualService model.Config,
	serviceRegistry map[model.Hostname]*model.Service) ([]string, []*model.Service) {
	rule := virtualService.Spec.(*networking.VirtualService)
	hosts := make([]string, 0)
	servicesInVirtualService := make([]*model.Service, 0)
	for _, host := range rule.Hosts {
		// Say host is *.global
		vsHostname := model.Hostname(host)
		foundSvcMatch := false
		// TODO: Optimize me. This is O(n2) or worse. Need to prune at top level in config
		// Say we have services *.foo.global, *.bar.global
		for svcHost, svc := range serviceRegistry {
			// *.foo.global matches *.global
			if svcHost.Matches(vsHostname) {
				servicesInVirtualService = append(servicesInVirtualService, svc)
				foundSvcMatch = true
			}
		}
		if !foundSvcMatch {
			hosts = append(hosts, host)
		}
	}
	return hosts, servicesInVirtualService
}

// buildSidecarVirtualHostsForVirtualService creates virtual hosts corresponding to a virtual service.
// Called for each port to determine the list of vhosts on the given port.
// It may return an empty list if no VirtualService rule has a matching service.
func buildSidecarVirtualHostsForVirtualService(
	node *model.Proxy,
	push *model.PushContext,
	virtualService model.Config,
	serviceRegistry map[model.Hostname]*model.Service,
	proxyLabels model.LabelsCollection,
	listenPort int) []VirtualHostWrapper {
	hosts, servicesInVirtualService := separateVSHostsAndServices(virtualService, serviceRegistry)

	// Now group these services by port so that we can infer the destination.port if the user
	// doesn't specify any port for a multiport service. We need to know the destination port in
	// order to build the cluster name (outbound|<port>|<subset>|<serviceFQDN>)
	// If the destination service is being accessed on port X, we set that as the default
	// destination port
	serviceByPort := make(map[int][]*model.Service)
	for _, svc := range servicesInVirtualService {
		for _, port := range svc.Ports {
			if port.Protocol.IsHTTP() {
				serviceByPort[port.Port] = append(serviceByPort[port.Port], svc)
			}
		}
	}

	// We need to group the virtual hosts by port, because each http connection manager is
	// going to send a separate RDS request
	// Note that we need to build non-default HTTP routes only for the virtual services.
	// The services in the serviceRegistry will always have a default route (/)
	if len(serviceByPort) == 0 {
		// This is a gross HACK. Fix me. Its a much bigger surgery though, due to the way
		// the current code is written.
		serviceByPort[80] = nil
	}
	meshGateway := map[string]bool{model.IstioMeshGateway: true}
	out := make([]VirtualHostWrapper, 0, len(serviceByPort))
	for port, portServices := range serviceByPort {
		routes, err := BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, listenPort, proxyLabels, meshGateway)
		if err != nil || len(routes) == 0 {
			continue
		}
		out = append(out, VirtualHostWrapper{
			Port:                port,
			Services:            portServices,
			VirtualServiceHosts: hosts,
			Routes:              routes,
		})
	}

	return out
}

// GetDestinationCluster generates a cluster name for the route, or error if no cluster
// can be found. Called by translateRule to determine if
func GetDestinationCluster(destination *networking.Destination, service *model.Service, listenerPort int) string {
	port := listenerPort
	if destination.Port != nil {
		switch selector := destination.Port.Port.(type) {
		// TODO: remove port name from route.Destination in the API
		case *networking.PortSelector_Name:
			log.Debuga("name based destination ports are not allowed => blackhole cluster")
			return util.BlackHoleCluster
		case *networking.PortSelector_Number:
			port = int(selector.Number)
		}
	} else {
		// if service only has one port defined, use that as the port, otherwise use default listenerPort
		if service != nil && len(service.Ports) == 1 {
			port = service.Ports[0].Port
		}
		// Do not return blackhole cluster for service==nil case as there is a legitimate use case for
		// calling this function with nil service: to route to a pre-defined statically configured cluster
		// declared as part of the bootstrap.
		// If blackhole cluster is needed, do the check on the caller side. See gateway and tls.go for examples.
	}

	return model.BuildSubsetKey(model.TrafficDirectionOutbound, destination.Subset, model.Hostname(destination.Host), port)
}

// BuildHTTPRoutesForVirtualService creates data plane HTTP routes from the virtual service spec.
// The rule should be adapted to destination names (outbound clusters).
// Each rule is guarded by source labels.
//
// This is called for each port to compute virtual hosts.
// Each VirtualService is tried, with a list of services that listen on the port.
// Error indicates the given virtualService can't be used on the port.
// This function is used by both the gateway and the sidecar
func BuildHTTPRoutesForVirtualService(
	node *model.Proxy,
	push *model.PushContext,
	virtualService model.Config,
	serviceRegistry map[model.Hostname]*model.Service,
	listenPort int,
	proxyLabels model.LabelsCollection,
	gatewayNames map[string]bool) ([]route.Route, error) {

	vs, ok := virtualService.Spec.(*networking.VirtualService)
	if !ok { // should never happen
		return nil, fmt.Errorf("in not a virtual service: %#v", virtualService)
	}

	out := make([]route.Route, 0, len(vs.Http))
allroutes:
	for _, http := range vs.Http {
		if len(http.Match) == 0 {
			if r := translateRoute(push, node, http, nil, listenPort, virtualService, serviceRegistry, proxyLabels, gatewayNames); r != nil {
				out = append(out, *r)
			}
			break allroutes // we have a rule with catch all match prefix: /. Other rules are of no use
		} else {
			for _, match := range http.Match {
				if r := translateRoute(push, node, http, match, listenPort, virtualService, serviceRegistry, proxyLabels, gatewayNames); r != nil {
					out = append(out, *r)
					rType, _ := getEnvoyRouteTypeAndVal(r)
					if rType == envoyCatchAll {
						// We have a catch all route. No point building other routes, with match conditions
						break allroutes
					}
				}
			}
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no routes matched")
	}
	return out, nil
}

// sourceMatchHttp checks if the sourceLabels or the gateways in a match condition match with the
// labels for the proxy or the gateway name for which we are generating a route
func sourceMatchHTTP(match *networking.HTTPMatchRequest, proxyLabels model.LabelsCollection, gatewayNames map[string]bool) bool {
	if match == nil {
		return true
	}

	// Trim by source labels or mesh gateway
	if len(match.Gateways) > 0 {
		for _, g := range match.Gateways {
			if gatewayNames[g] {
				return true
			}
		}
	} else if proxyLabels.IsSupersetOf(match.GetSourceLabels()) {
		return true
	}

	return false
}

// translateRoute translates HTTP routes
func translateRoute(push *model.PushContext, node *model.Proxy, in *networking.HTTPRoute,
	match *networking.HTTPMatchRequest, port int,
	virtualService model.Config,
	serviceRegistry map[model.Hostname]*model.Service,
	proxyLabels model.LabelsCollection,
	gatewayNames map[string]bool) *route.Route {

	// When building routes, its okay if the target cluster cannot be
	// resolved Traffic to such clusters will blackhole.

	// Match by source labels/gateway names inside the match condition
	if !sourceMatchHTTP(match, proxyLabels, gatewayNames) {
		return nil
	}

	// Match by the destination port specified in the match condition
	if match != nil && match.Port != 0 && match.Port != uint32(port) {
		return nil
	}

	out := &route.Route{
		Match:    translateRouteMatch(match),
		Metadata: util.BuildConfigInfoMetadata(virtualService.ConfigMeta),
	}

	if util.IsXDSMarshalingToAnyEnabled(node) {
		out.TypedPerFilterConfig = make(map[string]*types.Any)
	} else {
		out.PerFilterConfig = make(map[string]*types.Struct)
	}

	if redirect := in.Redirect; redirect != nil {
		out.Action = &route.Route_Redirect{
			Redirect: &route.RedirectAction{
				HostRedirect: redirect.Authority,
				PathRewriteSpecifier: &route.RedirectAction_PathRedirect{
					PathRedirect: redirect.Uri,
				},
			}}
	} else {
		action := &route.RouteAction{
			Cors:        translateCORSPolicy(in.CorsPolicy, node),
			RetryPolicy: retry.ConvertPolicy(in.Retries),
		}

		if in.Timeout != nil {
			d := util.GogoDurationToDuration(in.Timeout)
			// timeout
			action.Timeout = &d
			action.MaxGrpcTimeout = &d
		} else {
			// if no timeout is specified, disable timeouts. This is easier
			// to reason about than assuming some defaults.
			d := 0 * time.Second
			action.Timeout = &d
			action.MaxGrpcTimeout = &d
		}

		out.Action = &route.Route_Route{Route: action}

		if rewrite := in.Rewrite; rewrite != nil {
			action.PrefixRewrite = rewrite.Uri
			action.HostRewriteSpecifier = &route.RouteAction_HostRewrite{
				HostRewrite: rewrite.Authority,
			}
		}

		requestHeadersToAdd := translateAppendHeaders(in.Headers.GetRequest().GetSet(), false)
		requestHeadersToAdd = append(requestHeadersToAdd, translateAppendHeaders(in.Headers.GetRequest().GetAdd(), true)...)
		requestHeadersToAdd = append(requestHeadersToAdd, translateAppendHeaders(in.AppendRequestHeaders, true)...)
		requestHeadersToAdd = append(requestHeadersToAdd, translateAppendHeaders(in.AppendHeaders, true)...)
		out.RequestHeadersToAdd = requestHeadersToAdd
		responseHeadersToAdd := translateAppendHeaders(in.Headers.GetResponse().GetSet(), false)
		responseHeadersToAdd = append(responseHeadersToAdd, translateAppendHeaders(in.Headers.GetResponse().GetAdd(), true)...)
		responseHeadersToAdd = append(responseHeadersToAdd, translateAppendHeaders(in.AppendResponseHeaders, true)...)
		out.ResponseHeadersToAdd = responseHeadersToAdd
		requestHeadersToRemove := make([]string, 0)
		requestHeadersToRemove = append(requestHeadersToRemove, in.Headers.GetRequest().GetRemove()...)
		requestHeadersToRemove = append(requestHeadersToRemove, in.RemoveRequestHeaders...)
		out.RequestHeadersToRemove = requestHeadersToRemove
		responseHeadersToRemove := make([]string, 0)
		responseHeadersToRemove = append(responseHeadersToRemove, in.Headers.GetResponse().GetRemove()...)
		responseHeadersToRemove = append(responseHeadersToRemove, in.RemoveResponseHeaders...)
		out.ResponseHeadersToRemove = responseHeadersToRemove

		if in.Mirror != nil {
			n := GetDestinationCluster(in.Mirror, serviceRegistry[model.Hostname(in.Mirror.Host)], port)
			action.RequestMirrorPolicy = &route.RouteAction_RequestMirrorPolicy{Cluster: n}
		}

		// TODO: eliminate this logic and use the total_weight option in envoy route
		weighted := make([]*route.WeightedCluster_ClusterWeight, 0)
		for _, dst := range in.Route {
			weight := &types.UInt32Value{Value: uint32(dst.Weight)}
			if dst.Weight == 0 {
				// Ignore 0 weighted clusters if there are other clusters in the route.
				// But if this is the only cluster in the route, then add it as a cluster with weight 100
				if len(in.Route) == 1 {
					weight.Value = uint32(100)
				} else {
					continue
				}
			}

			requestHeadersToAdd := translateAppendHeaders(dst.Headers.GetRequest().GetSet(), false)
			requestHeadersToAdd = append(requestHeadersToAdd, translateAppendHeaders(dst.Headers.GetRequest().GetAdd(), true)...)
			requestHeadersToAdd = append(requestHeadersToAdd, translateAppendHeaders(dst.AppendRequestHeaders, true)...)
			responseHeadersToAdd := translateAppendHeaders(dst.Headers.GetResponse().GetSet(), false)
			responseHeadersToAdd = append(responseHeadersToAdd, translateAppendHeaders(dst.Headers.GetResponse().GetAdd(), true)...)
			responseHeadersToAdd = append(responseHeadersToAdd, translateAppendHeaders(dst.AppendResponseHeaders, true)...)
			requestHeadersToRemove := make([]string, 0)
			requestHeadersToRemove = append(requestHeadersToRemove, dst.Headers.GetRequest().GetRemove()...)
			requestHeadersToRemove = append(requestHeadersToRemove, dst.RemoveRequestHeaders...)
			responseHeadersToRemove := make([]string, 0)
			responseHeadersToRemove = append(responseHeadersToRemove, dst.Headers.GetResponse().GetRemove()...)
			responseHeadersToRemove = append(responseHeadersToRemove, dst.RemoveResponseHeaders...)

			hostname := model.Hostname(dst.GetDestination().GetHost())
			n := GetDestinationCluster(dst.Destination, serviceRegistry[hostname], port)

			clusterWeight := &route.WeightedCluster_ClusterWeight{
				Name:                    n,
				Weight:                  weight,
				RequestHeadersToAdd:     requestHeadersToAdd,
				RequestHeadersToRemove:  requestHeadersToRemove,
				ResponseHeadersToAdd:    responseHeadersToAdd,
				ResponseHeadersToRemove: responseHeadersToRemove,
			}

			weighted = append(weighted, clusterWeight)

			var configNamespace string
			if serviceRegistry[hostname] != nil {
				configNamespace = serviceRegistry[hostname].Attributes.Namespace
			}
			hashPolicy := getHashPolicy(push, node, dst, configNamespace)
			if hashPolicy != nil {
				action.HashPolicy = append(action.HashPolicy, hashPolicy)
			}
		}

		// rewrite to a single cluster if there is only weighted cluster
		if len(weighted) == 1 {
			action.ClusterSpecifier = &route.RouteAction_Cluster{Cluster: weighted[0].Name}
			out.RequestHeadersToAdd = append(out.RequestHeadersToAdd, weighted[0].RequestHeadersToAdd...)
			out.RequestHeadersToRemove = append(out.RequestHeadersToRemove, weighted[0].RequestHeadersToRemove...)
			out.ResponseHeadersToAdd = append(out.ResponseHeadersToAdd, weighted[0].ResponseHeadersToAdd...)
			out.ResponseHeadersToRemove = append(out.ResponseHeadersToRemove, weighted[0].ResponseHeadersToRemove...)
		} else {
			action.ClusterSpecifier = &route.RouteAction_WeightedClusters{
				WeightedClusters: &route.WeightedCluster{
					Clusters: weighted,
				},
			}
		}
	}

	out.Decorator = &route.Decorator{
		Operation: getRouteOperation(out, virtualService.Name, port),
	}
	if fault := in.Fault; fault != nil {
		if util.IsXDSMarshalingToAnyEnabled(node) {
			out.TypedPerFilterConfig[xdsutil.Fault] = util.MessageToAny(translateFault(node, in.Fault))
		} else {
			out.PerFilterConfig[xdsutil.Fault] = util.MessageToStruct(translateFault(node, in.Fault))
		}
	}

	return out
}

// SortHeaderValueOption type and the functions below (Len, Less and Swap) are for sort.Stable for type HeaderValueOption
type SortHeaderValueOption []*core.HeaderValueOption

// Len is i the sort.Interface for SortHeaderValueOption
func (b SortHeaderValueOption) Len() int {
	return len(b)
}

// Less is in the sort.Interface for SortHeaderValueOption
func (b SortHeaderValueOption) Less(i, j int) bool {
	if b[i] == nil || b[i].Header == nil {
		return false
	} else if b[j] == nil || b[j].Header == nil {
		return true
	}
	return strings.Compare(b[i].Header.Key, b[j].Header.Key) < 0
}

// Swap is in the sort.Interface for SortHeaderValueOption
func (b SortHeaderValueOption) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

// translateAppendHeaders translates headers
func translateAppendHeaders(headers map[string]string, appendFlag bool) []*core.HeaderValueOption {
	headerValueOptionList := make([]*core.HeaderValueOption, 0, len(headers))
	appendValue := &types.BoolValue{Value: appendFlag}
	for key, value := range headers {
		headerValueOptionList = append(headerValueOptionList, &core.HeaderValueOption{
			Header: &core.HeaderValue{
				Key:   key,
				Value: value,
			},
			Append: appendValue,
		})
	}
	sort.Stable(SortHeaderValueOption(headerValueOptionList))
	return headerValueOptionList
}

// translateRouteMatch translates match condition
func translateRouteMatch(in *networking.HTTPMatchRequest) route.RouteMatch {
	out := route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}}
	if in == nil {
		return out
	}

	for name, stringMatch := range in.Headers {
		matcher := translateHeaderMatch(name, stringMatch)
		out.Headers = append(out.Headers, &matcher)
	}

	// guarantee ordering of headers
	sort.Slice(out.Headers, func(i, j int) bool {
		return out.Headers[i].Name < out.Headers[j].Name
	})

	if in.Uri != nil {
		switch m := in.Uri.MatchType.(type) {
		case *networking.StringMatch_Exact:
			out.PathSpecifier = &route.RouteMatch_Path{Path: m.Exact}
		case *networking.StringMatch_Prefix:
			out.PathSpecifier = &route.RouteMatch_Prefix{Prefix: m.Prefix}
		case *networking.StringMatch_Regex:
			out.PathSpecifier = &route.RouteMatch_Regex{Regex: m.Regex}
		}
	}

	if in.Method != nil {
		matcher := translateHeaderMatch(HeaderMethod, in.Method)
		out.Headers = append(out.Headers, &matcher)
	}

	if in.Authority != nil {
		matcher := translateHeaderMatch(HeaderAuthority, in.Authority)
		out.Headers = append(out.Headers, &matcher)
	}

	if in.Scheme != nil {
		matcher := translateHeaderMatch(HeaderScheme, in.Scheme)
		out.Headers = append(out.Headers, &matcher)
	}

	return out
}

// translateHeaderMatch translates to HeaderMatcher
func translateHeaderMatch(name string, in *networking.StringMatch) route.HeaderMatcher {
	out := route.HeaderMatcher{
		Name: name,
	}

	switch m := in.MatchType.(type) {
	case *networking.StringMatch_Exact:
		out.HeaderMatchSpecifier = &route.HeaderMatcher_ExactMatch{ExactMatch: m.Exact}
	case *networking.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		out.HeaderMatchSpecifier = &route.HeaderMatcher_PrefixMatch{PrefixMatch: m.Prefix}
	case *networking.StringMatch_Regex:
		out.HeaderMatchSpecifier = &route.HeaderMatcher_RegexMatch{RegexMatch: m.Regex}
	}

	return out
}

// translateCORSPolicy translates CORS policy
func translateCORSPolicy(in *networking.CorsPolicy, node *model.Proxy) *route.CorsPolicy {
	if in == nil {
		return nil
	}

	out := route.CorsPolicy{
		AllowOrigin: in.AllowOrigin,
	}

	if util.IsProxyVersionGE11(node) {
		out.EnabledSpecifier = &route.CorsPolicy_FilterEnabled{}
	} else {
		out.EnabledSpecifier = &route.CorsPolicy_Enabled{Enabled: &types.BoolValue{Value: true}}
	}

	out.AllowCredentials = in.AllowCredentials
	out.AllowHeaders = strings.Join(in.AllowHeaders, ",")
	out.AllowMethods = strings.Join(in.AllowMethods, ",")
	out.ExposeHeaders = strings.Join(in.ExposeHeaders, ",")
	if in.MaxAge != nil {
		out.MaxAge = strconv.FormatInt(in.MaxAge.GetSeconds(), 10)
	}
	return &out
}

// getRouteOperation returns readable route description for trace.
func getRouteOperation(in *route.Route, vsName string, port int) string {
	path := "/*"
	m := in.GetMatch()
	ps := m.GetPathSpecifier()
	if ps != nil {
		switch ps.(type) {
		case *route.RouteMatch_Prefix:
			path = fmt.Sprintf("%s*", m.GetPrefix())
		case *route.RouteMatch_Path:
			path = m.GetPath()
		case *route.RouteMatch_Regex:
			path = m.GetRegex()
		}
	}

	// If there is only one destination cluster in route, return host:port/uri as description of route.
	// Otherwise there are multiple destination clusters and destination host is not clear. For that case
	// return virtual serivce name:port/uri as substitute.
	if c := in.GetRoute().GetCluster(); model.IsValidSubsetKey(c) {
		// Parse host and port from cluster name.
		_, _, h, p := model.ParseSubsetKey(c)
		return fmt.Sprintf("%s:%d%s", h, p, path)
	}
	return fmt.Sprintf("%s:%d%s", vsName, port, path)
}

// BuildDefaultHTTPInboundRoute builds a default inbound route.
func BuildDefaultHTTPInboundRoute(clusterName string, operation string) *route.Route {
	notimeout := 0 * time.Second

	return &route.Route{
		Match: translateRouteMatch(nil),
		Decorator: &route.Decorator{
			Operation: operation,
		},
		Action: &route.Route_Route{
			Route: &route.RouteAction{
				ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName},
				Timeout:          &notimeout,
				MaxGrpcTimeout:   &notimeout,
			},
		},
	}
}

// BuildDefaultHTTPOutboundRoute builds a default outbound route, including a retry policy.
func BuildDefaultHTTPOutboundRoute(clusterName string, operation string) *route.Route {
	// Start with the same configuration as for inbound.
	out := BuildDefaultHTTPInboundRoute(clusterName, operation)

	// Add a default retry policy for outbound routes.
	out.GetRoute().RetryPolicy = retry.DefaultPolicy()
	return out
}

// translatePercentToFractionalPercent translates an v1alpha3 Percent instance
// to an envoy.type.FractionalPercent instance.
func translatePercentToFractionalPercent(p *networking.Percent) *xdstype.FractionalPercent {
	return &xdstype.FractionalPercent{
		Numerator:   uint32(p.Value * 10000),
		Denominator: xdstype.FractionalPercent_MILLION,
	}
}

// translateIntegerToFractionalPercent translates an int32 instance to an
// envoy.type.FractionalPercent instance.
func translateIntegerToFractionalPercent(p int32) *xdstype.FractionalPercent {
	return &xdstype.FractionalPercent{
		Numerator:   uint32(p * 10000),
		Denominator: xdstype.FractionalPercent_MILLION,
	}
}

// translateFault translates networking.HTTPFaultInjection into Envoy's HTTPFault
func translateFault(node *model.Proxy, in *networking.HTTPFaultInjection) *xdshttpfault.HTTPFault {
	if in == nil {
		return nil
	}

	out := xdshttpfault.HTTPFault{}
	if in.Delay != nil {
		out.Delay = &xdsfault.FaultDelay{Type: xdsfault.FaultDelay_FIXED}
		if util.IsProxyVersionGE11(node) {
			if in.Delay.Percentage != nil {
				out.Delay.Percentage = translatePercentToFractionalPercent(in.Delay.Percentage)
			} else {
				out.Delay.Percentage = translateIntegerToFractionalPercent(in.Delay.Percent)
			}
		} else {
			if in.Delay.Percentage != nil {
				out.Delay.Percentage = translatePercentToFractionalPercent(in.Delay.Percentage)
			} else {
				out.Delay.Percentage = translateIntegerToFractionalPercent(in.Delay.Percent)
			}
		}
		switch d := in.Delay.HttpDelayType.(type) {
		case *networking.HTTPFaultInjection_Delay_FixedDelay:
			delayDuration := util.GogoDurationToDuration(d.FixedDelay)
			out.Delay.FaultDelaySecifier = &xdsfault.FaultDelay_FixedDelay{
				FixedDelay: &delayDuration,
			}
		default:
			log.Warnf("Exponential faults are not yet supported")
			out.Delay = nil
		}
	}

	if in.Abort != nil {
		out.Abort = &xdshttpfault.FaultAbort{}
		if util.IsProxyVersionGE11(node) {
			if in.Abort.Percentage != nil {
				out.Abort.Percentage = translatePercentToFractionalPercent(in.Abort.Percentage)
			} else {
				out.Abort.Percentage = translateIntegerToFractionalPercent(in.Abort.Percent)
			}
		} else {
			if in.Abort.Percentage != nil {
				out.Abort.Percentage = translatePercentToFractionalPercent(in.Abort.Percentage)
			} else {
				out.Abort.Percentage = translateIntegerToFractionalPercent(in.Abort.Percent)
			}
		}
		switch a := in.Abort.ErrorType.(type) {
		case *networking.HTTPFaultInjection_Abort_HttpStatus:
			out.Abort.ErrorType = &xdshttpfault.FaultAbort_HttpStatus{
				HttpStatus: uint32(a.HttpStatus),
			}
		default:
			log.Warnf("Non-HTTP type abort faults are not yet supported")
			out.Abort = nil
		}
	}

	if out.Delay == nil && out.Abort == nil {
		return nil
	}

	return &out
}

func portLevelSettingsConsistentHash(dst *networking.Destination,
	pls []*networking.TrafficPolicy_PortTrafficPolicy) *networking.LoadBalancerSettings_ConsistentHashLB {
	if dst.Port != nil {
		switch dst.Port.Port.(type) {
		case *networking.PortSelector_Name:
			log.Warnf("using deprecated name on port selector - ignoring")
		case *networking.PortSelector_Number:
			portNumber := dst.GetPort().GetNumber()
			for _, setting := range pls {
				number := setting.GetPort().GetNumber()
				if number == portNumber {
					return setting.GetLoadBalancer().GetConsistentHash()
				}
			}
		}
	}

	return nil
}

func getHashPolicy(push *model.PushContext, node *model.Proxy, dst *networking.HTTPRouteDestination,
	configNamespace string) *route.RouteAction_HashPolicy {
	if push == nil {
		return nil
	}

	destination := dst.GetDestination()
	destinationRule := push.DestinationRule(node,
		&model.Service{
			Hostname:   model.Hostname(destination.Host),
			Attributes: model.ServiceAttributes{Namespace: configNamespace},
		})
	if destinationRule == nil {
		return nil
	}
	rule := destinationRule.Spec.(*networking.DestinationRule)

	consistentHash := rule.GetTrafficPolicy().GetLoadBalancer().GetConsistentHash()
	portLevelSettings := rule.GetTrafficPolicy().GetPortLevelSettings()
	plsHash := portLevelSettingsConsistentHash(destination, portLevelSettings)

	var subsetHash, subsetPLSHash *networking.LoadBalancerSettings_ConsistentHashLB
	for _, subset := range rule.GetSubsets() {
		if subset.GetName() == destination.GetSubset() {
			subsetPortLevelSettings := subset.GetTrafficPolicy().GetPortLevelSettings()
			subsetHash = subset.GetTrafficPolicy().GetLoadBalancer().GetConsistentHash()
			subsetPLSHash = portLevelSettingsConsistentHash(destination, subsetPortLevelSettings)

			break
		}
	}

	switch {
	case subsetPLSHash != nil:
		consistentHash = subsetPLSHash
	case subsetHash != nil:
		consistentHash = subsetHash
	case plsHash != nil:
		consistentHash = plsHash
	}

	switch consistentHash.GetHashKey().(type) {
	case *networking.LoadBalancerSettings_ConsistentHashLB_HttpHeaderName:
		return &route.RouteAction_HashPolicy{
			PolicySpecifier: &route.RouteAction_HashPolicy_Header_{
				Header: &route.RouteAction_HashPolicy_Header{
					HeaderName: consistentHash.GetHttpHeaderName(),
				},
			},
		}
	case *networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie:
		cookie := consistentHash.GetHttpCookie()

		return &route.RouteAction_HashPolicy{
			PolicySpecifier: &route.RouteAction_HashPolicy_Cookie_{
				Cookie: &route.RouteAction_HashPolicy_Cookie{
					Name: cookie.GetName(),
					Ttl:  cookie.GetTtl(),
					Path: cookie.GetPath(),
				},
			},
		}
	case *networking.LoadBalancerSettings_ConsistentHashLB_UseSourceIp:
		return &route.RouteAction_HashPolicy{
			PolicySpecifier: &route.RouteAction_HashPolicy_ConnectionProperties_{
				ConnectionProperties: &route.RouteAction_HashPolicy_ConnectionProperties{
					SourceIp: consistentHash.GetUseSourceIp(),
				},
			},
		}
	}

	return nil
}

type envoyRouteType int

const (
	envoyPath envoyRouteType = iota
	envoyPrefix
	envoyRegex
	envoyCatchAll
)

func getEnvoyRouteTypeAndVal(r *route.Route) (envoyRouteType, string) {
	var iType envoyRouteType
	var iVal string

	switch iR := r.Match.PathSpecifier.(type) {
	case *route.RouteMatch_Path:
		iVal = iR.Path
		iType = envoyPath
	case *route.RouteMatch_Prefix:
		iVal = iR.Prefix
		iType = envoyPrefix
	case *route.RouteMatch_Regex:
		iVal = iR.Regex
		iType = envoyRegex
	}

	// A route is catch all if and only if it has no header/query param match
	// and has a prefix / or regex *.
	if (iVal == "/" && iType == envoyPrefix) || (iVal == "*" && iType == envoyRegex) {
		if len(r.Match.Headers) == 0 && len(r.Match.QueryParameters) == 0 {
			iType = envoyCatchAll
		}
	}
	return iType, iVal
}

// CombineVHostRoutes semi concatenates two Vhost's routes into a single route set.
// Moves the catch all routes alone to the end, while retaining
// the relative order of other routes in the concatenated route.
// Assumes that the virtual services that generated first and second are ordered by
// time.
func CombineVHostRoutes(first []route.Route, second []route.Route) []route.Route {
	allroutes := make([]route.Route, 0, len(first)+len(second))
	catchAllRoutes := make([]route.Route, 0)

	for _, f := range first {
		rType, _ := getEnvoyRouteTypeAndVal(&f)
		switch rType {
		case envoyCatchAll:
			catchAllRoutes = append(catchAllRoutes, f)
		default:
			allroutes = append(allroutes, f)
		}
	}

	for _, s := range second {
		rType, _ := getEnvoyRouteTypeAndVal(&s)
		switch rType {
		case envoyCatchAll:
			catchAllRoutes = append(catchAllRoutes, s)
		default:
			allroutes = append(allroutes, s)
		}
	}

	allroutes = append(allroutes, catchAllRoutes...)
	return allroutes
}
