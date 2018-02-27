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

package xds

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"

	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// Headers with special meaning in Envoy
const (
	HeaderMethod    = ":method"
	HeaderAuthority = ":authority"
	HeaderScheme    = ":scheme"
)

// ClusterName specifies cluster name for a destination
type ClusterName func(*routingv2.Destination) string

// GuardedRoute are routes for a destination guarded by deployment conditions.
type GuardedRoute struct {
	route.Route

	// SourceLabels guarding the route
	SourceLabels map[string]string

	// Gateways pre-condition
	Gateways []string
}

// TranslateRoutes creates virtual host routes from the v1alpha2 config.
// The rule should be adapted to destination names (outbound clusters).
// Each rule is guarded by source labels.
func TranslateRoutes(in model.Config, name ClusterName, defaultCluster string) []GuardedRoute {
	rule, ok := in.Spec.(*routingv2.RouteRule)
	if !ok {
		return nil
	}

	operation := in.ConfigMeta.Name

	if len(rule.Http) == 0 {
		return []GuardedRoute{
			TranslateRoute(&routingv2.HTTPRoute{}, nil, operation, name, defaultCluster),
		}
	}

	out := make([]GuardedRoute, 0)
	for _, http := range rule.Http {
		if len(http.Match) == 0 {
			out = append(out, TranslateRoute(http, nil, operation, name, defaultCluster))
		} else {
			for _, match := range http.Match {
				out = append(out, TranslateRoute(http, match, operation, name, defaultCluster))
			}
		}
	}

	return out
}

// TranslateRoute translates HTTP routes
// TODO: fault filters -- issue https://github.com/istio/api/issues/388
func TranslateRoute(in *routingv2.HTTPRoute,
	match *routingv2.HTTPMatchRequest,
	operation string,
	name ClusterName,
	defaultCluster string) GuardedRoute {
	out := route.Route{
		Match: TranslateRouteMatch(match),
		Decorator: &route.Decorator{
			Operation: operation,
		},
	}

	if redirect := in.Redirect; redirect != nil {
		out.Action = &route.Route_Redirect{
			Redirect: &route.RedirectAction{
				HostRedirect: redirect.Authority,
				PathRedirect: redirect.Uri,
			}}
	} else {
		action := &route.RouteAction{
			Cors:         TranslateCORSPolicy(in.CorsPolicy),
			RetryPolicy:  TranslateRetryPolicy(in.Retries),
			Timeout:      TranslateTime(in.Timeout),
			UseWebsocket: &types.BoolValue{Value: in.WebsocketUpgrade},
		}
		out.Action = &route.Route_Route{Route: action}

		if rewrite := in.Rewrite; rewrite != nil {
			action.PrefixRewrite = rewrite.Uri
			action.HostRewriteSpecifier = &route.RouteAction_HostRewrite{
				HostRewrite: rewrite.Authority,
			}
		}

		if len(in.AppendHeaders) > 0 {
			action.RequestHeadersToAdd = make([]*core.HeaderValueOption, len(in.AppendHeaders))
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
			action.RequestMirrorPolicy = &route.RouteAction_RequestMirrorPolicy{Cluster: name(in.Mirror)}
		}

		if len(in.Route) == 0 { // build default cluster
			action.ClusterSpecifier = &route.RouteAction_Cluster{Cluster: defaultCluster}
		} else {
			weighted := make([]*route.WeightedCluster_ClusterWeight, len(in.Route))
			for _, dst := range in.Route {
				weighted = append(weighted, &route.WeightedCluster_ClusterWeight{
					Name:   name(dst.Destination),
					Weight: &types.UInt32Value{Value: uint32(dst.Weight)},
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
	}

	return GuardedRoute{
		Route:        out,
		SourceLabels: match.GetSourceLabels(),
		Gateways:     match.GetGateways(),
	}
}

// TranslateRouteMatch translates match condition
func TranslateRouteMatch(in *routingv2.HTTPMatchRequest) route.RouteMatch {
	out := route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}}
	if in == nil {
		return out
	}

	for name, stringMatch := range in.Headers {
		matcher := TranslateHeaderMatcher(name, stringMatch)
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
		case *routingv2.StringMatch_Exact:
			out.PathSpecifier = &route.RouteMatch_Path{Path: m.Exact}
		case *routingv2.StringMatch_Prefix:
			out.PathSpecifier = &route.RouteMatch_Prefix{Prefix: m.Prefix}
		case *routingv2.StringMatch_Regex:
			out.PathSpecifier = &route.RouteMatch_Regex{Regex: m.Regex}
		}
	}

	if in.Method != nil {
		matcher := TranslateHeaderMatcher(HeaderMethod, in.Method)
		out.Headers = append(out.Headers, &matcher)
	}

	if in.Authority != nil {
		matcher := TranslateHeaderMatcher(HeaderAuthority, in.Authority)
		out.Headers = append(out.Headers, &matcher)
	}

	if in.Scheme != nil {
		matcher := TranslateHeaderMatcher(HeaderScheme, in.Scheme)
		out.Headers = append(out.Headers, &matcher)
	}

	// TODO: match.DestinationPorts

	return out
}

// TranslateHeaderMatcher translates to HeaderMatcher
func TranslateHeaderMatcher(name string, in *routingv2.StringMatch) route.HeaderMatcher {
	out := route.HeaderMatcher{
		Name: name,
	}

	switch m := in.MatchType.(type) {
	case *routingv2.StringMatch_Exact:
		out.Value = m.Exact
	case *routingv2.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		out.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		out.Regex = &types.BoolValue{Value: true}
	case *routingv2.StringMatch_Regex:
		out.Value = m.Regex
		out.Regex = &types.BoolValue{Value: true}
	}

	return out
}

// TranslateRetryPolicy translates retry policy
func TranslateRetryPolicy(in *routingv2.HTTPRetry) *route.RouteAction_RetryPolicy {
	if in != nil && in.Attempts > 0 {
		return &route.RouteAction_RetryPolicy{
			NumRetries:    &types.UInt32Value{Value: uint32(in.GetAttempts())},
			RetryOn:       "5xx,connect-failure,refused-stream",
			PerTryTimeout: TranslateTime(in.PerTryTimeout),
		}
	}
	return nil
}

// TranslateCORSPolicy translates CORS policy
func TranslateCORSPolicy(in *routingv2.CorsPolicy) *route.CorsPolicy {
	if in == nil {
		return nil
	}

	out := route.CorsPolicy{
		AllowOrigin: in.AllowOrigin,
		Enabled:     &types.BoolValue{Value: true},
	}
	if in.AllowCredentials != nil {
		out.AllowCredentials = TranslateBool(in.AllowCredentials)
	}
	if len(in.AllowHeaders) > 0 {
		out.AllowHeaders = strings.Join(in.AllowHeaders, ",")
	}
	if len(in.AllowMethods) > 0 {
		out.AllowMethods = strings.Join(in.AllowMethods, ",")
	}
	if len(in.ExposeHeaders) > 0 {
		out.ExposeHeaders = strings.Join(in.ExposeHeaders, ",")
	}
	if in.MaxAge != nil {
		out.MaxAge = in.MaxAge.String()
	}
	return &out
}

// TranslateBool converts bool wrapper.
func TranslateBool(in *wrappers.BoolValue) *types.BoolValue {
	if in == nil {
		return nil
	}
	return &types.BoolValue{Value: in.Value}
}

// TranslateTime converts time protos.
func TranslateTime(in *duration.Duration) *time.Duration {
	if in == nil {
		return nil
	}
	out, err := ptypes.Duration(in)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", in, err)
	}
	return &out
}
