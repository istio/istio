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

	"github.com/agentgateway/agentgateway/go/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
)

// BuildAgwTrafficPolicyFilters builds a list of agentgateway TrafficPolicySpec from a list of k8s gateway api HTTPRoute filters
func BuildAgwTrafficPolicyFilters(
	ctx RouteContext,
	ns string,
	inputFilters []gatewayv1.HTTPRouteFilter,
) ([]*api.TrafficPolicySpec, *Condition) {
	var policies []*api.TrafficPolicySpec
	var hasTerminalFilter bool
	var terminalFilterType string
	var policyError *Condition
	// Collect multiples of same-type filters to merge
	var mergedReqHdr *api.HeaderModifier
	var mergedRespHdr *api.HeaderModifier
	var mergedMirror []*api.RequestMirrors_Mirror
	for _, filter := range inputFilters {
		switch filter.Type {
		case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
			h := CreateAgwHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			mergedReqHdr = mergeHeaderModifiers(mergedReqHdr, h)
		case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
			h := CreateAgwResponseHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			mergedRespHdr = mergeHeaderModifiers(mergedRespHdr, h)
		case gatewayv1.HTTPRouteFilterRequestRedirect:
			if hasTerminalFilter {
				policyError = &Condition{
					status: metav1.ConditionFalse,
					error: &ConfigError{
						Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
						Message: terminalFilterCombinationError(terminalFilterType, "RequestRedirect"),
					},
				}
				continue
			}
			h := CreateAgwRedirectFilter(filter.RequestRedirect)
			if h == nil {
				continue
			}
			policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestRedirect{RequestRedirect: h}})
			hasTerminalFilter = true
			terminalFilterType = "RequestRedirect"
		case gatewayv1.HTTPRouteFilterRequestMirror:
			h, err := CreateAgwMirrorFilter(ctx, filter.RequestMirror, ns, gvk.HTTPRoute)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
			} else {
				mergedMirror = append(mergedMirror, h)
			}
		case gatewayv1.HTTPRouteFilterURLRewrite:
			h := CreateAgwRewriteFilter(filter.URLRewrite)
			if h == nil {
				continue
			}
			policies = append(policies, h)
		case gatewayv1.HTTPRouteFilterCORS:
			h := createAgwCorsFilter(filter.CORS)
			if h == nil {
				continue
			}
			policies = append(policies, h)
		case gatewayv1.HTTPRouteFilterExternalAuth:
			h, err := CreateAgwExternalAuthFilter(ctx, filter.ExternalAuth, ns, gvk.HTTPRoute)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
			}
			policies = append(policies, h)
		case gatewayv1.HTTPRouteFilterExtensionRef:
			err := createAgwExtensionRefFilter(filter.ExtensionRef)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
				continue
			}
		default:
			return nil, &Condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
					Message: fmt.Sprintf("unsupported filter type: %v", filter.Type),
				},
			}
		}
	}
	// Append merged header modifiers at the end to avoid duplicates
	if mergedReqHdr != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestHeaderModifier{RequestHeaderModifier: mergedReqHdr}})
	}
	if mergedRespHdr != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_ResponseHeaderModifier{ResponseHeaderModifier: mergedRespHdr}})
	}
	if mergedMirror != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestMirror{RequestMirror: &api.RequestMirrors{Mirrors: mergedMirror}}})
	}
	return policies, policyError
}

// BuildAgwBackendPolicyFilters builds a list of agentgateway BackendPolicySpec from a list of k8s gateway api HTTPRoute filters
func BuildAgwBackendPolicyFilters(
	ctx RouteContext,
	ns string,
	inputFilters []gatewayv1.HTTPRouteFilter,
) ([]*api.BackendPolicySpec, *Condition) {
	var policies []*api.BackendPolicySpec
	var hasTerminalFilter bool
	var terminalFilterType string
	var policyError *Condition
	// Collect multiples of same-type filters to merge
	var mergedReqHdr *api.HeaderModifier
	var mergedRespHdr *api.HeaderModifier
	var mergedMirror []*api.RequestMirrors_Mirror
	for _, filter := range inputFilters {
		switch filter.Type {
		case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
			h := CreateAgwHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			mergedReqHdr = mergeHeaderModifiers(mergedReqHdr, h)
		case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
			h := CreateAgwResponseHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			mergedRespHdr = mergeHeaderModifiers(mergedRespHdr, h)
		case gatewayv1.HTTPRouteFilterRequestRedirect:
			if hasTerminalFilter {
				policyError = &Condition{
					status: metav1.ConditionFalse,
					error: &ConfigError{
						Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
						Message: terminalFilterCombinationError(terminalFilterType, "RequestRedirect"),
					},
				}
				continue
			}
			h := CreateAgwRedirectFilter(filter.RequestRedirect)
			if h == nil {
				continue
			}
			policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_RequestRedirect{RequestRedirect: h}})
			hasTerminalFilter = true
			terminalFilterType = "RequestRedirect"
		case gatewayv1.HTTPRouteFilterRequestMirror:
			h, err := CreateAgwMirrorFilter(ctx, filter.RequestMirror, ns, gvk.HTTPRoute)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
			} else {
				mergedMirror = append(mergedMirror, h)
			}
		default:
			return nil, &Condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
					Message: fmt.Sprintf("unsupported filter type: %v", filter.Type),
				},
			}
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
	return policies, policyError
}

// mergeHeaderModifiers merges two api.HeaderModifier instances by concatenating their Add/Set/Remove lists.
// Later entries are applied after earlier ones by preserving order in the resulting slices.
func mergeHeaderModifiers(dst, src *api.HeaderModifier) *api.HeaderModifier {
	if src == nil {
		return dst
	}
	if dst == nil {
		// Create a copy of src to avoid mutating input
		out := &api.HeaderModifier{}
		if len(src.Add) > 0 {
			out.Add = append([]*api.Header{}, src.Add...)
		}
		if len(src.Set) > 0 {
			out.Set = append([]*api.Header{}, src.Set...)
		}
		if len(src.Remove) > 0 {
			out.Remove = append([]string{}, src.Remove...)
		}
		return out
	}
	if len(src.Add) > 0 {
		dst.Add = append(dst.Add, src.Add...)
	}
	if len(src.Set) > 0 {
		dst.Set = append(dst.Set, src.Set...)
	}
	if len(src.Remove) > 0 {
		dst.Remove = append(dst.Remove, src.Remove...)
	}
	return dst
}

// terminalFilterCombinationError creates a standardized error message for when multiple terminal filters are used together
func terminalFilterCombinationError(existingFilter, newFilter string) string {
	return fmt.Sprintf("Cannot combine multiple terminal filters: %s and %s are mutually exclusive. Only one terminal filter "+
		"is allowed per route rule.", existingFilter, newFilter)
}

func isPolicyErrorCritical(filterError *Condition) bool {
	criticalReasons := []gatewayv1.RouteConditionReason{
		"FilterNotSupported",
		"FilterConfigInvalid",
		// Add other critical filter error reasons as needed
	}

	return slices.Contains(criticalReasons, gatewayv1.RouteConditionReason(filterError.reason))
}
