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
	"strings"

	"github.com/agentgateway/agentgateway/go/api"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// CreateAgwMethodMatch creates an agw MethodMatch from a HTTPRouteMatch.
func CreateAgwMethodMatch(match gatewayv1.HTTPRouteMatch) *api.MethodMatch {
	if match.Method == nil {
		return nil
	}
	return &api.MethodMatch{
		Exact: string(*match.Method),
	}
}

// CreateAgwQueryMatch creates an agw QueryMatch from a HTTPRouteMatch.
func CreateAgwQueryMatch(match gatewayv1.HTTPRouteMatch) []*api.QueryMatch {
	res := []*api.QueryMatch{}
	for _, header := range match.QueryParams {
		tp := gatewayv1.QueryParamMatchExact
		if header.Type != nil {
			tp = *header.Type
		}
		switch tp {
		case gatewayv1.QueryParamMatchExact:
			res = append(res, &api.QueryMatch{
				Name:  string(header.Name),
				Value: &api.QueryMatch_Exact{Exact: header.Value},
			})
		case gatewayv1.QueryParamMatchRegularExpression:
			res = append(res, &api.QueryMatch{
				Name:  string(header.Name),
				Value: &api.QueryMatch_Regex{Regex: header.Value},
			})
		default:
			// Should never happen, unless a new field is added
			return nil
		}
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// CreateAgwPathMatch creates an agw PathMatch from a HTTPRouteMatch.
func CreateAgwPathMatch(match gatewayv1.HTTPRouteMatch) *api.PathMatch {
	if match.Path == nil {
		return nil
	}
	tp := gatewayv1.PathMatchPathPrefix
	if match.Path.Type != nil {
		tp = *match.Path.Type
	}
	// Path value must start with "/". If empty/nil, coerce to "/".
	dest := "/"
	if match.Path.Value != nil && *match.Path.Value != "" {
		dest = *match.Path.Value
	}
	if !strings.HasPrefix(dest, "/") {
		dest = "/" + dest
	}
	switch tp {
	case gatewayv1.PathMatchPathPrefix:
		// Spec: trailing "/" is ignored in a prefix (except the root "/").
		if dest != "/" {
			dest = strings.TrimRight(dest, "/")
			if dest == "" {
				dest = "/"
			}
		}
		return &api.PathMatch{
			Kind: &api.PathMatch_PathPrefix{
				PathPrefix: dest,
			},
		}

	case gatewayv1.PathMatchExact:
		// EXACT: do not normalize trailing slash; it must match byte-for-byte.
		return &api.PathMatch{
			Kind: &api.PathMatch_Exact{
				Exact: dest,
			},
		}
	case gatewayv1.PathMatchRegularExpression:
		// Pass regex through unchanged.
		return &api.PathMatch{
			Kind: &api.PathMatch_Regex{
				Regex: dest,
			},
		}
	default:
		// Defensive: unknown type => UnsupportedValue.
		return nil
	}
}

// CreateAgwHeadersMatch creates an agw HeadersMatch from a HTTPRouteMatch.
func CreateAgwHeadersMatch(match gatewayv1.HTTPRouteMatch) []*api.HeaderMatch {
	var res []*api.HeaderMatch
	for _, header := range match.Headers {
		tp := gatewayv1.HeaderMatchExact
		if header.Type != nil {
			tp = *header.Type
		}
		switch tp {
		case gatewayv1.HeaderMatchExact:
			res = append(res, &api.HeaderMatch{
				Name:  string(header.Name),
				Value: &api.HeaderMatch_Exact{Exact: header.Value},
			})
		case gatewayv1.HeaderMatchRegularExpression:
			res = append(res, &api.HeaderMatch{
				Name:  string(header.Name),
				Value: &api.HeaderMatch_Regex{Regex: header.Value},
			})
		default:
			// Should never happen, unless a new field is added
			return nil
		}
	}

	if len(res) == 0 {
		return nil
	}
	return res
}

// CreateAgwHeadersFilter creates an agw HeaderModifier based on a HTTPHeaderFilter
func CreateAgwHeadersFilter(filter *gatewayv1.HTTPHeaderFilter) *api.HeaderModifier {
	if filter == nil {
		return nil
	}
	return &api.HeaderModifier{
		Add:    headerListToAgw(filter.Add),
		Set:    headerListToAgw(filter.Set),
		Remove: filter.Remove,
	}
}

// CreateAgwResponseHeadersFilter creates an agw TrafficPolicySpec based on a HTTPHeaderFilter
func CreateAgwResponseHeadersFilter(filter *gatewayv1.HTTPHeaderFilter) *api.HeaderModifier {
	if filter == nil {
		return nil
	}
	return &api.HeaderModifier{
		Add:    headerListToAgw(filter.Add),
		Set:    headerListToAgw(filter.Set),
		Remove: filter.Remove,
	}
}

// CreateAgwRewriteFilter creates an agw TrafficPolicySpec based on a HTTPURLRewriteFilter
func CreateAgwRewriteFilter(filter *gatewayv1.HTTPURLRewriteFilter) *api.TrafficPolicySpec {
	if filter == nil {
		return nil
	}

	var hostname string
	if filter.Hostname != nil {
		hostname = string(*filter.Hostname)
	}
	ff := &api.UrlRewrite{
		Host: hostname,
	}
	if filter.Path != nil {
		switch filter.Path.Type {
		case gatewayv1.PrefixMatchHTTPPathModifier:
			ff.Path = &api.UrlRewrite_Prefix{Prefix: strings.TrimSuffix(*filter.Path.ReplacePrefixMatch, "/")}
		case gatewayv1.FullPathHTTPPathModifier:
			ff.Path = &api.UrlRewrite_Full{Full: strings.TrimSuffix(*filter.Path.ReplaceFullPath, "/")}
		}
	}
	return &api.TrafficPolicySpec{
		Kind: &api.TrafficPolicySpec_UrlRewrite{
			UrlRewrite: ff,
		},
	}
}

// CreateAgwMirrorFilter creates an agw RequestMirror based on a HTTPRequestMirrorFilter
func CreateAgwMirrorFilter(
	ctx RouteContext,
	filter *gatewayv1.HTTPRequestMirrorFilter,
	ns string,
	k config.GroupVersionKind,
) *api.RequestMirrors_Mirror {
	if filter == nil {
		return nil
	}
	var weightOne int32 = 1
	// TODO(jaellio): handle error
	dst := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
		BackendRef: gatewayv1.BackendRef{
			BackendObjectReference: filter.BackendRef,
			Weight:                 &weightOne,
		},
	}, ns, k)
	var percent float64
	if f := filter.Fraction; f != nil {
		denominator := float64(100)
		if f.Denominator != nil {
			denominator = float64(*f.Denominator)
		}
		percent = (100 * float64(f.Numerator)) / denominator
	} else if p := filter.Percent; p != nil {
		percent = float64(*p)
	} else {
		percent = 100
	}
	if percent == 0 {
		return nil
	}
	return &api.RequestMirrors_Mirror{
		Percentage: percent,
		Backend:    dst.GetBackend(),
	}
}

// CreateAgwExternalAuthFilter creates Agw filter from Gateway API ExternalAuth filter
func CreateAgwExternalAuthFilter(
	ctx RouteContext,
	filter *gatewayv1.HTTPExternalAuthFilter,
	ns string,
	k config.GroupVersionKind,
) *api.TrafficPolicySpec {
	if filter == nil {
		return nil
	}
	// TODO(jaellio): handle error
	dst := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
		BackendRef: gatewayv1.BackendRef{
			BackendObjectReference: filter.BackendRef,
			Weight:                 ptr.Of(int32(1)),
		},
	}, ns, k)
	pol := &api.TrafficPolicySpec_ExternalAuth{
		Target: dst.GetBackend(),
	}
	if b := filter.ForwardBody; b != nil {
		pol.IncludeRequestBody = &api.TrafficPolicySpec_ExternalAuth_BodyOptions{
			MaxRequestBytes: uint32(b.MaxSize),
			// "Bodies over that size must fail processing"
			AllowPartialMessage: false,
			// TODO(https://github.com/kubernetes-sigs/gateway-api/issues/4198): do we want this?
			PackAsBytes: false,
		}
	}
	switch filter.ExternalAuthProtocol {
	case gatewayv1.HTTPRouteExternalAuthHTTPProtocol:
		http := ptr.OrEmpty(filter.HTTPAuthConfig)
		pp := &api.TrafficPolicySpec_ExternalAuth_HTTPProtocol{
			// Not supported in the API
			Redirect: nil,
			// Not supported in the API
			AddRequestHeaders: nil,
			// Not supported in the API
			Metadata: nil,
		}
		if http.Path != "" {
			path := http.Path
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			pp.Path = ptr.Of(fmt.Sprintf("%q + request.path", path))
		}
		// Per spec, this must always be included
		pol.IncludeRequestHeaders = []string{
			"Authorization",
			":authority",
		}
		if len(http.AllowedRequestHeaders) > 0 {
			pol.IncludeRequestHeaders = append(pol.IncludeRequestHeaders, http.AllowedRequestHeaders...)
		}
		pp.IncludeResponseHeaders = http.AllowedResponseHeaders
		pol.Protocol = &api.TrafficPolicySpec_ExternalAuth_Http{
			Http: pp,
		}
	case gatewayv1.HTTPRouteExternalAuthGRPCProtocol:
		grpc := ptr.OrEmpty(filter.GRPCAuthConfig)
		pp := &api.TrafficPolicySpec_ExternalAuth_GRPCProtocol{
			// Not supported in the API
			Context: nil,
			// Not supported in the API
			Metadata: nil,
		}
		if len(grpc.AllowedRequestHeaders) > 0 {
			pol.IncludeRequestHeaders = grpc.AllowedRequestHeaders
		}
		pol.Protocol = &api.TrafficPolicySpec_ExternalAuth_Grpc{
			Grpc: pp,
		}
	}
	return &api.TrafficPolicySpec{
		Kind: &api.TrafficPolicySpec_ExtAuthz{
			ExtAuthz: pol,
		},
	}
}

// CreateAgwGRPCHeadersMatch creates an agw HeaderMatch from a GRPCRouteMatch.
func CreateAgwGRPCHeadersMatch(match gatewayv1.GRPCRouteMatch) []*api.HeaderMatch {
	var res []*api.HeaderMatch
	for _, header := range match.Headers {
		tp := gatewayv1.GRPCHeaderMatchExact
		if header.Type != nil {
			tp = *header.Type
		}
		switch tp {
		case gatewayv1.GRPCHeaderMatchExact:
			res = append(res, &api.HeaderMatch{
				Name:  string(header.Name),
				Value: &api.HeaderMatch_Exact{Exact: header.Value},
			})
		case gatewayv1.GRPCHeaderMatchRegularExpression:
			res = append(res, &api.HeaderMatch{
				Name:  string(header.Name),
				Value: &api.HeaderMatch_Regex{Regex: header.Value},
			})
		default:
			// Should never happen, unless a new field is added
			return nil
		}
	}

	if len(res) == 0 {
		return nil
	}
	return res
}

func headerListToAgw(hl []gatewayv1.HTTPHeader) []*api.Header {
	return slices.Map(hl, func(hl gatewayv1.HTTPHeader) *api.Header {
		return &api.Header{
			Name:  string(hl.Name),
			Value: hl.Value,
		}
	})
}

// CreateAgwRedirectFilter converts a HTTPRequestRedirectFilter into an api.RequestRedirect for request redirection.
func CreateAgwRedirectFilter(filter *gatewayv1.HTTPRequestRedirectFilter) *api.RequestRedirect {
	if filter == nil {
		return nil
	}
	var scheme, host string
	var port, statusCode uint32
	if filter.Scheme != nil {
		scheme = *filter.Scheme
	}
	if filter.Hostname != nil {
		host = string(*filter.Hostname)
	}
	if filter.Port != nil {
		port = uint32(*filter.Port) //nolint:gosec // G115: Gateway API PortNumber is int32 with validation 1-65535, always safe
	}
	if filter.StatusCode != nil {
		statusCode = uint32(*filter.StatusCode) //nolint:gosec // G115: HTTP status codes are always positive integers (100-599)
	}

	ff := &api.RequestRedirect{
		Scheme: scheme,
		Host:   host,
		Port:   port,
		Status: statusCode,
	}
	if filter.Path != nil {
		switch filter.Path.Type {
		case gatewayv1.PrefixMatchHTTPPathModifier:
			ff.Path = &api.RequestRedirect_Prefix{Prefix: strings.TrimSuffix(*filter.Path.ReplacePrefixMatch, "/")}
		case gatewayv1.FullPathHTTPPathModifier:
			ff.Path = &api.RequestRedirect_Full{Full: strings.TrimSuffix(*filter.Path.ReplaceFullPath, "/")}
		}
	}
	return ff
}

// TODO(jaellio): Handle errors and setting conditions
// BuildAgwGRPCTrafficPolicies constructs gRPC route filters for agent gateway based on the input filters and route context.
func BuildAgwGRPCTrafficPolicies(
	ctx RouteContext,
	ns string,
	inputFilters []gatewayv1.GRPCRouteFilter,
) []*api.TrafficPolicySpec {
	var policies []*api.TrafficPolicySpec
	// Collect multiples of same-type filters to merge
	var mergedReqHdr *api.HeaderModifier
	var mergedRespHdr *api.HeaderModifier
	var mergedMirror []*api.RequestMirrors_Mirror
	for _, filter := range inputFilters {
		switch filter.Type {
		case gatewayv1.GRPCRouteFilterRequestHeaderModifier:
			h := CreateAgwHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			mergedReqHdr = mergeHeaderModifiers(mergedReqHdr, h)
		case gatewayv1.GRPCRouteFilterResponseHeaderModifier:
			h := CreateAgwResponseHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			mergedRespHdr = mergeHeaderModifiers(mergedRespHdr, h)
		case gatewayv1.GRPCRouteFilterRequestMirror:
			h := CreateAgwMirrorFilter(ctx, filter.RequestMirror, ns, gvk.GRPCRoute)
			mergedMirror = append(mergedMirror, h)
		default:
			return nil
		}
	}
	if mergedReqHdr != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestHeaderModifier{RequestHeaderModifier: mergedReqHdr}})
	}
	if mergedRespHdr != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_ResponseHeaderModifier{ResponseHeaderModifier: mergedRespHdr}})
	}
	if mergedMirror != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestMirror{RequestMirror: &api.RequestMirrors{Mirrors: mergedMirror}}})
	}
	return policies
}

// BuildAgwGRPCBackendPolicies constructs gRPC route filters for agent gateway based on the input filters and route context.
func BuildAgwGRPCBackendPolicies(
	ctx RouteContext,
	ns string,
	inputFilters []gatewayv1.GRPCRouteFilter,
) []*api.BackendPolicySpec {
	var policies []*api.BackendPolicySpec
	// Collect multiples of same-type filters to merge
	var mergedReqHdr *api.HeaderModifier
	var mergedRespHdr *api.HeaderModifier
	var mergedMirror []*api.RequestMirrors_Mirror
	for _, filter := range inputFilters {
		switch filter.Type {
		case gatewayv1.GRPCRouteFilterRequestHeaderModifier:
			h := CreateAgwHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			mergedReqHdr = mergeHeaderModifiers(mergedReqHdr, h)
		case gatewayv1.GRPCRouteFilterResponseHeaderModifier:
			h := CreateAgwResponseHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			mergedRespHdr = mergeHeaderModifiers(mergedRespHdr, h)
		case gatewayv1.GRPCRouteFilterRequestMirror:
			h := CreateAgwMirrorFilter(ctx, filter.RequestMirror, ns, gvk.GRPCRoute)
			mergedMirror = append(mergedMirror, h)
		default:
			return nil
		}
	}
	// Append merged header modifiers at the end to avoid duplicates
	if mergedReqHdr != nil {
		policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_RequestHeaderModifier{RequestHeaderModifier: mergedReqHdr}})
	}
	if mergedRespHdr != nil {
		policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_ResponseHeaderModifier{ResponseHeaderModifier: mergedRespHdr}})
	}
	if mergedMirror != nil {
		policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_RequestMirror{RequestMirror: &api.RequestMirrors{Mirrors: mergedMirror}}})
	}
	return policies
}

// TODO(jaellio): Handle errors and setting conditions
func buildAgwGRPCDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.GRPCBackendRef,
	ns string,
) []*api.RouteBackend {
	if forwardTo == nil {
		return nil
	}

	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
			BackendRef: fwd.BackendRef,
			Filters:    nil, // GRPC filters are handled separately
		}, ns, gvk.GRPCRoute)
		// TODO(jaellio): handle error building agw destination
		if dst != nil {
			policies := BuildAgwGRPCBackendPolicies(ctx, ns, fwd.Filters)
			dst.BackendPolicies = policies
		}
		res = append(res, dst)
	}
	return res
}
