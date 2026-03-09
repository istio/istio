// Copyright Istio Authors
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

package agentgateway

import (
	"fmt"
	"strconv"
	"time"

	"github.com/agentgateway/agentgateway/go/api"
	"github.com/golang/protobuf/ptypes/duration"
	"google.golang.org/protobuf/types/known/durationpb"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// Helper function to convert hostnames
func convertHostnames(hostnames []gatewayv1.Hostname) []string {
	return slices.Map(hostnames, func(h gatewayv1.Hostname) string {
		return string(h)
	})
}

// InternalRouteRuleKey returns the name of the internal Route Rule corresponding to the
// specified route. If ruleName is not specified, returns the internal name without the route rule.
// Format: routeNs/routeName.ruleName
func InternalRouteRuleKey(routeNamespace, routeName, ruleName string) string {
	if ruleName == "" {
		return fmt.Sprintf("%s/%s", routeNamespace, routeName)
	}
	return fmt.Sprintf("%s/%s.%s", routeNamespace, routeName, ruleName)
}

// RouteName constructs the RouteName for a route rule, including the rule name if specified
func RouteName[T ~string](kind string, namespace, name string, routeRule *T) *api.RouteName {
	var ls *string
	if routeRule != nil {
		ls = ptr.Of((string)(*routeRule))
	}
	return &api.RouteName{
		Name:      name,
		Namespace: namespace,
		RuleName:  ls,
		Kind:      kind,
	}
}

// Helper function to process route matches
func processRouteMatches(r *gatewayv1.HTTPRouteRule, res *api.Route) error {
	for _, match := range r.Matches {
		path := CreateAgwPathMatch(match)
		headers := CreateAgwHeadersMatch(match)
		method := CreateAgwMethodMatch(match)
		query := CreateAgwQueryMatch(match)

		res.Matches = append(res.GetMatches(), &api.RouteMatch{
			Path:        path,
			Headers:     headers,
			Method:      method,
			QueryParams: query,
		})
	}
	return nil
}

// ApplyTimeouts applies timeouts to an agw route
func ApplyTimeouts(rule *gatewayv1.HTTPRouteRule, route *api.Route) error {
	if rule == nil || rule.Timeouts == nil {
		return nil
	}
	if route.TrafficPolicies == nil {
		route.TrafficPolicies = []*api.TrafficPolicySpec{}
	}
	var reqDur, beDur *durationpb.Duration

	if rule.Timeouts.Request != nil {
		d, err := time.ParseDuration(string(*rule.Timeouts.Request))
		if err != nil {
			return fmt.Errorf("failed to parse request timeout: %w", err)
		}
		if d != 0 {
			// "Setting a timeout to the zero duration (e.g. "0s") SHOULD disable the timeout"
			// However, agentgateway already defaults to no timeout, so only set for non-zero
			reqDur = durationpb.New(d)
		}
	}
	if rule.Timeouts.BackendRequest != nil {
		d, err := time.ParseDuration(string(*rule.Timeouts.BackendRequest))
		if err != nil {
			return fmt.Errorf("failed to parse backend request timeout: %w", err)
		}
		if d != 0 {
			// "Setting a timeout to the zero duration (e.g. "0s") SHOULD disable the timeout"
			// However, agentgateway already defaults to no timeout, so only set for non-zero
			beDur = durationpb.New(d)
		}
	}
	if reqDur != nil || beDur != nil {
		route.TrafficPolicies = append(route.TrafficPolicies, &api.TrafficPolicySpec{
			Kind: &api.TrafficPolicySpec_Timeout{
				Timeout: &api.Timeout{
					Request:        reqDur,
					BackendRequest: beDur,
				},
			},
		})
	}
	return nil
}

// ApplyRetries applies retries to an agw route
func ApplyRetries(rule *gatewayv1.HTTPRouteRule, route *api.Route) error {
	if rule == nil || rule.Retry == nil {
		return nil
	}
	if a := rule.Retry.Attempts; a != nil && *a == 0 {
		return nil
	}
	if route.TrafficPolicies == nil {
		route.TrafficPolicies = []*api.TrafficPolicySpec{}
	}
	tpRetry := &api.Retry{}
	if rule.Retry.Codes != nil {
		for _, c := range rule.Retry.Codes {
			tpRetry.RetryStatusCodes = append(tpRetry.RetryStatusCodes, int32(c)) //nolint:gosec // G115: HTTP status codes are always positive integers (100-599)
		}
	}
	if rule.Retry.Backoff != nil {
		if d, err := time.ParseDuration(string(*rule.Retry.Backoff)); err == nil {
			tpRetry.Backoff = durationpb.New(d)
		}
	}
	if rule.Retry.Attempts != nil {
		tpRetry.Attempts = int32(*rule.Retry.Attempts) //nolint:gosec // G115: kubebuilder validation ensures 0 <= value, safe for int32
	}
	route.TrafficPolicies = append(route.TrafficPolicies, &api.TrafficPolicySpec{
		Kind: &api.TrafficPolicySpec_Retry{
			Retry: tpRetry,
		},
	})
	return nil
}

// ConvertHTTPRouteToAgw converts a HTTPRouteRule to an agentgateway HTTPRoute
func ConvertHTTPRouteToAgw(ctx RouteContext, r gatewayv1.HTTPRouteRule,
	obj *gatewayv1.HTTPRoute, pos int, matchPos int,
) (*api.Route, *condition) {
	routeRuleKey := strconv.Itoa(pos) + "." + strconv.Itoa(matchPos)
	res := &api.Route{
		// unique for route rule
		Key:  InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name: RouteName("HTTPRoute", obj.Namespace, obj.Name, r.Name),
		// filled in later
		ListenerKey: "",
	}

	if err := processRouteMatches(&r, res); err != nil {
		return nil, &condition{
			status: "False",
			error: &ConfigError{
				Reason:  "InvalidMatch",
				Message: fmt.Sprintf("failed to process route matches: %v", err),
			},
		}
	}

	policies, policiesErr := BuildAgwTrafficPolicyFilters(ctx, obj.Namespace, r.Filters)
	res.TrafficPolicies = policies

	if err := ApplyTimeouts(&r, res); err != nil {
		return nil, &condition{
			status: "False",
			error: &ConfigError{
				Reason:  "TranslationError",
				Message: fmt.Sprintf("failed to apply builtin route timeout: %v", err),
			},
		}
	}
	if err := ApplyRetries(&r, res); err != nil {
		return nil, &condition{
			status: "False",
			error: &ConfigError{
				Reason:  "TranslationError",
				Message: fmt.Sprintf("failed to apply builtin route retries: %v", err),
			},
		}
	}

	backends, backendErr, err := buildAgwHTTPDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		return nil, &condition{
			status: "False",
			error: &ConfigError{
				Reason:  "BackendError",
				Message: fmt.Sprintf("failed to build backend destination: %v", err),
			},
		}
	}
	res.Backends = backends

	res.Hostnames = convertHostnames(obj.Spec.Hostnames)

	if policiesErr != nil && !isPolicyErrorCritical(policiesErr) {
		return nil, policiesErr
	}

	return res, backendErr
}

// ConvertGRPCRouteToAgw converts a GRPCRouteRule to an agentgateway HTTPRoute
func ConvertGRPCRouteToAgw(ctx RouteContext, r gatewayv1.GRPCRouteRule,
	obj *gatewayv1.GRPCRoute, pos int,
) (*api.Route, *condition) {
	routeRuleKey := strconv.Itoa(pos)
	res := &api.Route{
		// unique for route rule
		Key:         InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name:        RouteName(gvk.GRPCRoute.Kind, obj.Namespace, obj.Name, r.Name),
		ListenerKey: "",
	}

	// Convert GRPC matches to Agw format
	for _, match := range r.Matches {
		headers, err := CreateAgwGRPCHeadersMatch(match)
		if err != nil {
			log.Errorf("failed to translate grpc header match", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
			return nil, err
		}
		// For GRPC, we don't have path match in the traditional sense, so we'll derive it from method
		var path *api.PathMatch
		if match.Method != nil {
			// Convert GRPC method to path for routing purposes
			if match.Method.Service != nil && match.Method.Method != nil {
				pathStr := fmt.Sprintf("/%s/%s", *match.Method.Service, *match.Method.Method)
				path = &api.PathMatch{Kind: &api.PathMatch_Exact{Exact: pathStr}}
			} else if match.Method.Service != nil {
				pathStr := fmt.Sprintf("/%s/", *match.Method.Service)
				path = &api.PathMatch{Kind: &api.PathMatch_Exact{Exact: pathStr}}
			} else if match.Method.Method != nil {
				// Convert wildcard to regex: "/*/{method}" becomes "/[^/]+/{method}"
				pathStr := fmt.Sprintf("/[^/]+/%s", *match.Method.Method)
				path = &api.PathMatch{Kind: &api.PathMatch_Regex{Regex: pathStr}}
			}
		}
		res.Matches = append(res.GetMatches(), &api.RouteMatch{
			Path:    path,
			Headers: headers,
			// note: the RouteMatch method field only applies for http methods
		})
	}
	if len(res.Matches) == 0 {
		// HTTPRoute defaults in the CRD itself, but GRPCRoute does not.
		// Agentgateway expects there to always be a match set.
		res.Matches = []*api.RouteMatch{{
			Path: &api.PathMatch{Kind: &api.PathMatch_PathPrefix{PathPrefix: "/"}},
		}}
	}

	policies, err := BuildAgwGRPCTrafficPolicies(ctx, obj.Namespace, r.Filters)
	if err != nil {
		log.Errorf("failed to translate grpc filter", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
		return nil, err
	}
	res.TrafficPolicies = policies

	route, backendErr, err := buildAgwGRPCDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		log.Errorf("failed to translate grpc destination", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
		return nil, err
	}
	res.Backends = route
	res.Hostnames = slices.Map(obj.Spec.Hostnames, func(e gatewayv1.Hostname) string {
		return string(e)
	})
	return res, backendErr
}

// ConvertTCPRouteToAgw converts a TCPRouteRule to an agentgateway TCPRoute
func ConvertTCPRouteToAgw(ctx RouteContext, r gatewayalpha.TCPRouteRule,
	obj *gatewayalpha.TCPRoute, pos int,
) (*api.TCPRoute, *condition) {
	routeRuleKey := strconv.Itoa(pos)
	res := &api.TCPRoute{
		// unique for route rule
		Key:         InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name:        RouteName(gvk.TCPRoute.Kind, obj.Namespace, obj.Name, r.Name),
		ListenerKey: "",
	}

	// Build TCP destinations
	route, backendErr, err := buildAgwTCPDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		log.Errorf("failed to translate tcp destination", "err", err)
		return nil, err
	}
	res.Backends = route

	return res, backendErr
}

// ConvertTLSRouteToAgw converts a TLSRouteRule to an agentgateway TCPRoute
func ConvertTLSRouteToAgw(ctx RouteContext, r gatewayalpha.TLSRouteRule,
	obj *gatewayalpha.TLSRoute, pos int,
) (*api.TCPRoute, *condition) {
	routeRuleKey := strconv.Itoa(pos)
	res := &api.TCPRoute{
		// unique for route rule
		Key:         InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name:        RouteName(gvk.TLSRoute.Kind, obj.Namespace, obj.Name, r.Name),
		ListenerKey: "",
	}

	// Build TLS destinations
	route, backendErr, err := buildAgwTLSDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		log.Errorf("failed to translate tls destination", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
		return nil, err
	}
	res.Backends = route

	// TLS Routes have hostnames in the spec (unlike TCP Routes)
	res.Hostnames = slices.Map(obj.Spec.Hostnames, func(e gatewayv1.Hostname) string {
		return string(e)
	})

	return res, backendErr
}

// GetStatus extracts the status from a route or gateway resource.
func GetStatus[I, IS any](spec I) IS {
	switch t := any(spec).(type) {
	case *gatewayalpha.TCPRoute:
		return any(t.Status).(IS)
	case *gatewayalpha.TLSRoute:
		return any(t.Status).(IS)
	case *gatewayv1.HTTPRoute:
		return any(t.Status).(IS)
	case *gatewayv1.GRPCRoute:
		return any(t.Status).(IS)
	case *gatewayv1.Gateway:
		return any(t.Status).(IS)
	case *gatewayv1.GatewayClass:
		return any(t.Status).(IS)
	case *gatewayx.XBackendTrafficPolicy:
		return any(t.Status).(IS)
	case *gatewayv1.BackendTLSPolicy:
		return any(t.Status).(IS)
	case *gatewayv1.ListenerSet:
		return any(t.Status).(IS)
	case *inferencev1.InferencePool:
		return any(t.Status).(IS)
	default:
		log.Fatalf("unknown type %T", t)
		return ptr.Empty[IS]()
	}
}

// GetCommonRouteInfo extracts parent references, hostnames, and GVK from a route resource.
func GetCommonRouteInfo(spec any) ([]gatewayv1.ParentReference, []gatewayv1.Hostname, config.GroupVersionKind) {
	switch t := spec.(type) {
	case *gatewayalpha.TCPRoute:
		return t.Spec.ParentRefs, nil, gvk.TCPRoute
	case *gatewayalpha.TLSRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.TLSRoute
	case *gatewayv1.HTTPRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.HTTPRoute
	case *gatewayv1.GRPCRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.GRPCRoute
	default:
		log.Fatalf("unknown type %T", t)
		return nil, nil, config.GroupVersionKind{}
	}
}

// createAgwCorsFilter converts a gatewayv1.HTTPCORSFilter to an agentgateway TrafficPolicySpec with CORS configuration
func createAgwCorsFilter(cors *gatewayv1.HTTPCORSFilter) *api.TrafficPolicySpec {
	if cors == nil {
		return nil
	}
	return &api.TrafficPolicySpec{
		Kind: &api.TrafficPolicySpec_Cors{Cors: &api.CORS{
			AllowCredentials: ptr.OrEmpty(cors.AllowCredentials),
			AllowHeaders:     slices.Map(cors.AllowHeaders, func(h gatewayv1.HTTPHeaderName) string { return string(h) }),
			AllowMethods:     slices.Map(cors.AllowMethods, func(m gatewayv1.HTTPMethodWithWildcard) string { return string(m) }),
			AllowOrigins:     slices.Map(cors.AllowOrigins, func(o gatewayv1.CORSOrigin) string { return string(o) }),
			ExposeHeaders:    slices.Map(cors.ExposeHeaders, func(h gatewayv1.HTTPHeaderName) string { return string(h) }),
			MaxAge: &duration.Duration{
				Seconds: int64(cors.MaxAge),
			},
		}},
	}
}

// createAgwExtensionRefFilter creates Agw filter from Gateway API ExtensionRef filter
func createAgwExtensionRefFilter(
	extensionRef *gatewayv1.LocalObjectReference,
) *condition {
	if extensionRef == nil {
		return nil
	}

	// TODO(jaellio): support other types of extension refs (TrafficPolicySpec, etc.)
	// https://github.com/kgateway-dev/kgateway/issues/12037

	// Unsupported ExtensionRef
	return &condition{
		status: "False",
		error: &ConfigError{
			Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
			Message: fmt.Sprintf("unsupported ExtensionRef: %s/%s", extensionRef.Group, extensionRef.Kind),
		},
	}
}
