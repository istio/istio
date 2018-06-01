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
	"regexp"
	"sort"
	"strings"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	xdsfault "github.com/envoyproxy/go-control-plane/envoy/config/filter/fault/v2"
	xdshttpfault "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/fault/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"
	"github.com/prometheus/client_golang/prometheus"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

// Headers with special meaning in Envoy
const (
	HeaderMethod    = ":method"
	HeaderAuthority = ":authority"
	HeaderScheme    = ":scheme"
)

const (
	// DefaultRoute is the default decorator
	DefaultRoute = "default-route"
)

var (
	// experiment on getting some monitoring on config errors.
	noClusterMissingPort = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_route_cluster_no_port",
		Help: "Routes with no clusters due to missing port.",
	}, []string{"service", "rule"})

	noClusterMissingService = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_route_nocluster_no_service",
		Help: "Routes with no clusters due to missing service",
	}, []string{"service", "rule"})
)

func init() {
	prometheus.MustRegister(noClusterMissingPort)
	prometheus.MustRegister(noClusterMissingService)
}

// GuardedHost is a context-dependent virtual host entry with guarded routes.
type GuardedHost struct {
	// Port is the capture port (e.g. service port)
	Port int

	// Services are the services matching the virtual host.
	// The service host names need to be contextualized by the source.
	Services []*model.Service

	// Hosts is a list of alternative literal host names for the host.
	Hosts []string

	// Routes in the virtual host
	Routes []route.Route
}

// TranslateVirtualHosts creates the entire routing table for Istio v1alpha3 configs.
// Services are indexed by FQDN hostnames.
// Cluster domain is used to resolve short service names (e.g. "svc.cluster.local").
func TranslateVirtualHosts(
	serviceConfigs []model.Config, services map[model.Hostname]*model.Service, proxyLabels model.LabelsCollection, gatewayNames map[string]bool) []GuardedHost {

	out := make([]GuardedHost, 0)

	// translate all virtual service configs
	for _, config := range serviceConfigs {
		out = append(out, translateVirtualHost(config, services, proxyLabels, gatewayNames)...)
	}

	// compute services missing service configs
	missing := make(map[model.Hostname]bool)
	for fqdn := range services {
		missing[fqdn] = true
	}
	for _, host := range out {
		for _, service := range host.Services {
			delete(missing, service.Hostname)
		}
	}

	// append default hosts for the service missing virtual services
	for fqdn := range missing {
		svc := services[fqdn]
		for _, port := range svc.Ports {
			if port.Protocol.IsHTTP() {
				cluster := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", svc.Hostname, port.Port)
				out = append(out, GuardedHost{
					Port:     port.Port,
					Services: []*model.Service{svc},
					Routes:   []route.Route{*BuildDefaultHTTPRoute(cluster)},
				})
			}
		}
	}

	return out
}

// matchServiceHosts splits the virtual service hosts into services and literal hosts
func matchServiceHosts(in model.Config, serviceIndex map[model.Hostname]*model.Service) ([]string, []*model.Service) {
	rule := in.Spec.(*networking.VirtualService)
	hosts := make([]string, 0)
	services := make([]*model.Service, 0)
	for _, host := range rule.Hosts {
		// Say host is *.global
		vsHostname := model.Hostname(host)
		foundSvcMatch := false
		// TODO: Optimize me. This is O(n2) or worse. Need to prune at top level in config
		// Say we have services *.foo.global, *.bar.global
		for svcHost, svc := range serviceIndex {
			// *.foo.global matches *.global
			if svcHost.Matches(vsHostname) {
				services = append(services, svc)
				foundSvcMatch = true
			}
		}
		if !foundSvcMatch {
			hosts = append(hosts, host)
		}
	}
	return hosts, services
}

// translateVirtualHost creates virtual hosts corresponding to a virtual service.
// Called for each port to determine the list of vhosts on the given port.
// It may return an empty list if no VirtualService rule has a matching service.
func translateVirtualHost(
	in model.Config, serviceIndex map[model.Hostname]*model.Service, proxyLabels model.LabelsCollection, gatewayName map[string]bool) []GuardedHost {

	hosts, services := matchServiceHosts(in, serviceIndex)
	serviceByPort := make(map[int][]*model.Service)
	for _, svc := range services {
		for _, port := range svc.Ports {
			if port.Protocol.IsHTTP() {
				serviceByPort[port.Port] = append(serviceByPort[port.Port], svc)
			}
		}
	}

	// if no services matched, then we have no port information -- default to 80 for now
	if len(serviceByPort) == 0 {
		serviceByPort[80] = nil
	}

	out := make([]GuardedHost, 0, len(serviceByPort))
	for port, portServices := range serviceByPort {
		routes, err := TranslateRoutes(in, serviceIndex, port, proxyLabels, gatewayName)
		if err != nil || len(routes) == 0 {
			continue
		}
		out = append(out, GuardedHost{
			Port:     port,
			Services: portServices,
			Hosts:    hosts,
			Routes:   routes,
		})
	}

	return out
}

// GetDestinationCluster generate a cluster name for the route, or error if no cluster
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
	}

	return model.BuildSubsetKey(model.TrafficDirectionOutbound, destination.Subset, model.Hostname(destination.Host), port)
}

// TranslateRoutes creates virtual host routes from the v1alpha3 config.
// The rule should be adapted to destination names (outbound clusters).
// Each rule is guarded by source labels.
//
// This is called for each port to compute virtual hosts.
// Each VirtualService is tried, with a list of services that listen on the port.
// Error indicates the given virtualService can't be used on the port.
func TranslateRoutes(
	virtualService model.Config,
	serviceIndex map[model.Hostname]*model.Service,
	port int,
	proxyLabels model.LabelsCollection,
	gatewayNames map[string]bool) ([]route.Route, error) {

	rule, ok := virtualService.Spec.(*networking.VirtualService)
	if !ok { // should never happen
		return nil, fmt.Errorf("in not a virtual service: %#v", virtualService)
	}

	operation := virtualService.ConfigMeta.Name

	out := make([]route.Route, 0, len(rule.Http))
	for _, http := range rule.Http {
		if len(http.Match) == 0 {
			if r := translateRoute(http, nil, port, operation, serviceIndex, proxyLabels, gatewayNames); r != nil {
				out = append(out, *r)
			}
			break // we have a rule with catch all match prefix: /. Other rules are of no use
		} else {
			// TODO: https://github.com/istio/istio/issues/4239
			for _, match := range http.Match {
				if r := translateRoute(http, match, port, operation, serviceIndex, proxyLabels, gatewayNames); r != nil {
					out = append(out, *r)
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
func translateRoute(in *networking.HTTPRoute,
	match *networking.HTTPMatchRequest, port int,
	operation string,
	serviceIndex map[model.Hostname]*model.Service,
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
		Match: translateRouteMatch(match),
		Decorator: &route.Decorator{
			Operation: operation,
		},
		PerFilterConfig: make(map[string]*types.Struct),
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
			Cors:         translateCORSPolicy(in.CorsPolicy),
			RetryPolicy:  translateRetryPolicy(in.Retries),
			UseWebsocket: &types.BoolValue{Value: in.WebsocketUpgrade},
		}
		if in.Timeout != nil {
			d := util.GogoDurationToDuration(in.Timeout)
			// timeout
			//(Duration) Specifies the timeout for the route. If not specified, the default is 15s.
			// Having it set to 0 may work as well.
			action.Timeout = &d
		}

		out.Action = &route.Route_Route{Route: action}

		if rewrite := in.Rewrite; rewrite != nil {
			action.PrefixRewrite = rewrite.Uri
			action.HostRewriteSpecifier = &route.RouteAction_HostRewrite{
				HostRewrite: rewrite.Authority,
			}
		}

		if len(in.AppendHeaders) > 0 {
			action.RequestHeadersToAdd = make([]*core.HeaderValueOption, 0)
			for key, value := range in.AppendHeaders {
				action.RequestHeadersToAdd = append(action.RequestHeadersToAdd, &core.HeaderValueOption{
					Header: &core.HeaderValue{
						Key:   key,
						Value: value,
					},
				})
			}
		}

		if in.Mirror != nil {
			n := GetDestinationCluster(in.Mirror, serviceIndex[model.Hostname(in.Mirror.Host)], port)
			action.RequestMirrorPolicy = &route.RouteAction_RequestMirrorPolicy{Cluster: n}
		}

		weighted := make([]*route.WeightedCluster_ClusterWeight, 0)
		for _, dst := range in.Route {
			weight := &types.UInt32Value{Value: uint32(dst.Weight)}
			if dst.Weight == 0 {
				weight.Value = uint32(100)
			}
			n := GetDestinationCluster(dst.Destination, serviceIndex[model.Hostname(dst.Destination.Host)], port)
			weighted = append(weighted, &route.WeightedCluster_ClusterWeight{
				Name:   n,
				Weight: weight,
			})
		}

		// rewrite to a single cluster if there is only weighted cluster
		if len(weighted) == 1 {
			action.ClusterSpecifier = &route.RouteAction_Cluster{Cluster: weighted[0].Name}
		} else {
			action.ClusterSpecifier = &route.RouteAction_WeightedClusters{
				WeightedClusters: &route.WeightedCluster{
					Clusters: weighted,
				},
			}
		}
	}

	if fault := in.Fault; fault != nil {
		out.PerFilterConfig[xdsutil.Fault] = util.MessageToStruct(translateFault(in.Fault))
	}

	return out
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
		if out.Headers[i].Name == out.Headers[j].Name {
			return out.Headers[i].Value < out.Headers[j].Value
		}
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
		out.Value = m.Exact
	case *networking.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		out.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		out.Regex = &types.BoolValue{Value: true}
	case *networking.StringMatch_Regex:
		out.Value = m.Regex
		out.Regex = &types.BoolValue{Value: true}
	}

	return out
}

// translateRetryPolicy translates retry policy
func translateRetryPolicy(in *networking.HTTPRetry) *route.RouteAction_RetryPolicy {
	if in != nil && in.Attempts > 0 {
		d := util.GogoDurationToDuration(in.PerTryTimeout)
		return &route.RouteAction_RetryPolicy{
			NumRetries:    &types.UInt32Value{Value: uint32(in.GetAttempts())},
			RetryOn:       "5xx,connect-failure,refused-stream",
			PerTryTimeout: &d,
		}
	}
	return nil
}

// translateCORSPolicy translates CORS policy
func translateCORSPolicy(in *networking.CorsPolicy) *route.CorsPolicy {
	if in == nil {
		return nil
	}

	out := route.CorsPolicy{
		AllowOrigin: in.AllowOrigin,
		Enabled:     &types.BoolValue{Value: true},
	}
	out.AllowCredentials = in.AllowCredentials
	out.AllowHeaders = strings.Join(in.AllowHeaders, ",")
	out.AllowMethods = strings.Join(in.AllowMethods, ",")
	out.ExposeHeaders = strings.Join(in.ExposeHeaders, ",")
	if in.MaxAge != nil {
		out.MaxAge = in.MaxAge.String()
	}
	return &out
}

// BuildDefaultHTTPRoute builds a default route.
func BuildDefaultHTTPRoute(clusterName string) *route.Route {
	return &route.Route{
		Match: translateRouteMatch(nil),
		Decorator: &route.Decorator{
			Operation: DefaultRoute,
		},
		Action: &route.Route_Route{
			Route: &route.RouteAction{
				ClusterSpecifier: &route.RouteAction_Cluster{Cluster: clusterName},
			},
		},
	}
}

// translateFault translates networking.HTTPFaultInjection into Envoy's HTTPFault
func translateFault(in *networking.HTTPFaultInjection) *xdshttpfault.HTTPFault {
	if in == nil {
		return nil
	}

	out := xdshttpfault.HTTPFault{}
	if in.Delay != nil {
		out.Delay = &xdsfault.FaultDelay{
			Type:    xdsfault.FaultDelay_FIXED,
			Percent: uint32(in.Delay.Percent),
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
		out.Abort = &xdshttpfault.FaultAbort{
			Percent: uint32(in.Abort.Percent),
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
