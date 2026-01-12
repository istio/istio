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

package validation

import (
	"errors"
	"fmt"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/validation/agent"
)

type HTTPRouteType int

const (
	IndependentRoute = iota
	RootRoute
	DelegateRoute
)

func getHTTPRouteType(http *networking.HTTPRoute, isDelegate bool) HTTPRouteType {
	if isDelegate {
		return DelegateRoute
	}
	// root vs's http route
	if http.Delegate != nil {
		return RootRoute
	}
	return IndependentRoute
}

func validateHTTPRoute(http *networking.HTTPRoute, delegate, gatewaySemantics bool) (errs Validation) {
	routeType := getHTTPRouteType(http, delegate)
	// check for conflicts
	errs = WrapError(validateHTTPRouteConflict(http, routeType))

	// check http route match requests
	errs = AppendValidation(errs, validateHTTPRouteMatchRequest(http))

	// header manipulation
	for name, val := range http.Headers.GetRequest().GetAdd() {
		errs = AppendValidation(errs, ValidateHTTPHeaderWithAuthorityOperationName(name))
		errs = AppendValidation(errs, ValidateHTTPHeaderValue(val))
	}
	for name, val := range http.Headers.GetRequest().GetSet() {
		errs = AppendValidation(errs, ValidateHTTPHeaderWithAuthorityOperationName(name))
		errs = AppendValidation(errs, ValidateHTTPHeaderValue(val))
	}
	for _, name := range http.Headers.GetRequest().GetRemove() {
		errs = AppendValidation(errs, ValidateHTTPHeaderOperationName(name))
	}
	for name, val := range http.Headers.GetResponse().GetAdd() {
		errs = AppendValidation(errs, ValidateHTTPHeaderOperationName(name))
		errs = AppendValidation(errs, ValidateHTTPHeaderValue(val))
	}
	for name, val := range http.Headers.GetResponse().GetSet() {
		errs = AppendValidation(errs, ValidateHTTPHeaderOperationName(name))
		errs = AppendValidation(errs, ValidateHTTPHeaderValue(val))
	}
	for _, name := range http.Headers.GetResponse().GetRemove() {
		errs = AppendValidation(errs, ValidateHTTPHeaderOperationName(name))
	}

	errs = AppendValidation(errs, validateCORSPolicy(http.CorsPolicy))
	errs = AppendValidation(errs, validateHTTPFaultInjection(http.Fault))

	// nolint: staticcheck
	if http.MirrorPercent != nil {
		if value := http.MirrorPercent.GetValue(); value > 100 {
			errs = AppendValidation(errs, fmt.Errorf("mirror_percent must have a max value of 100 (it has %d)", value))
		}
		errs = AppendValidation(errs, WrapWarning(errors.New(`using deprecated setting "mirrorPercent", use "mirrorPercentage" instead`)))
	}

	if http.MirrorPercentage != nil {
		value := http.MirrorPercentage.GetValue()
		if value > 100 {
			errs = AppendValidation(errs, fmt.Errorf("mirror_percentage must have a max value of 100 (it has %f)", value))
		}
		if value < 0 {
			errs = AppendValidation(errs, fmt.Errorf("mirror_percentage must have a min value of 0 (it has %f)", value))
		}
	}

	errs = AppendValidation(errs, validateDestination(http.Mirror))
	errs = AppendValidation(errs, validateHTTPMirrors(http.Mirrors))
	errs = AppendValidation(errs, validateHTTPRedirect(http.Redirect))
	errs = AppendValidation(errs, validateHTTPDirectResponse(http.DirectResponse))
	errs = AppendValidation(errs, validateHTTPRetry(http.Retries))
	errs = AppendValidation(errs, validateHTTPRewrite(http.Rewrite))
	errs = AppendValidation(errs, validateAuthorityRewrite(http.Rewrite, http.Headers))
	errs = AppendValidation(errs, validateHTTPRouteDestinations(http.Route, gatewaySemantics))
	if http.Timeout != nil {
		errs = AppendValidation(errs, agent.ValidateDuration(http.Timeout))
	}

	return errs
}

// validateAuthorityRewrite ensures we only attempt rewrite authority in a single place.
func validateAuthorityRewrite(rewrite *networking.HTTPRewrite, headers *networking.Headers) error {
	current := rewrite.GetAuthority()
	for k, v := range headers.GetRequest().GetSet() {
		if !isAuthorityHeader(k) {
			continue
		}
		if current != "" {
			return fmt.Errorf("authority header cannot be set multiple times: have %q, attempting to set %q", current, v)
		}
		current = v
	}
	for k, v := range headers.GetRequest().GetAdd() {
		if !isAuthorityHeader(k) {
			continue
		}
		if current != "" {
			return fmt.Errorf("authority header cannot be set multiple times: have %q, attempting to set %q", current, v)
		}
		current = v
	}
	return nil
}

func validateHTTPRouteMatchRequest(http *networking.HTTPRoute) (errs error) {
	for _, match := range http.Match {
		if match != nil {
			for name, header := range match.Headers {
				if header == nil {
					errs = appendErrors(errs, fmt.Errorf("header match %v cannot be null", name))
				}
				errs = appendErrors(errs, ValidateHTTPHeaderNameOrJwtClaimRoute(name))
				errs = appendErrors(errs, validateStringMatch(header, "headers"))
			}

			for name, header := range match.WithoutHeaders {
				errs = appendErrors(errs, ValidateHTTPHeaderNameOrJwtClaimRoute(name))
				// `*` is NOT a RE2 style regex, it will be translated as present_match.
				if header != nil && header.GetRegex() != "*" {
					errs = appendErrors(errs, validateStringMatch(header, "withoutHeaders"))
				}
			}

			// uri allows empty prefix match:
			// https://github.com/envoyproxy/envoy/blob/v1.29.2/api/envoy/config/route/v3/route_components.proto#L560
			// whereas scheme/method/authority does not:
			// https://github.com/envoyproxy/envoy/blob/v1.29.2/api/envoy/type/matcher/string.proto#L38
			errs = appendErrors(errs, validateStringMatchRegexp(match.GetUri(), "uri"))
			errs = appendErrors(errs, validateStringMatch(match.GetScheme(), "scheme"))
			errs = appendErrors(errs, validateStringMatch(match.GetMethod(), "method"))
			errs = appendErrors(errs, validateStringMatch(match.GetAuthority(), "authority"))
			for _, qp := range match.GetQueryParams() {
				errs = appendErrors(errs, validateStringMatch(qp, "queryParams"))
			}
		}
	}

	for _, match := range http.Match {
		if match != nil {
			if match.Port != 0 {
				errs = appendErrors(errs, agent.ValidatePort(int(match.Port)))
			}
			errs = appendErrors(errs, labels.Instance(match.SourceLabels).Validate())
			errs = appendErrors(errs, validateGatewayNames(match.Gateways, false))
			if match.SourceNamespace != "" {
				if !labels.IsDNS1123Label(match.SourceNamespace) {
					errs = appendErrors(errs, fmt.Errorf("sourceNamespace match %s is invalid", match.SourceNamespace))
				}
			}
		}
	}

	return errs
}

func validateHTTPRouteConflict(http *networking.HTTPRoute, routeType HTTPRouteType) (errs error) {
	if routeType == RootRoute {
		// This is to check root conflict
		// only delegate can be specified
		if http.Redirect != nil {
			errs = appendErrors(errs, fmt.Errorf("root HTTP route %s must not specify redirect", http.Name))
		}
		if http.Route != nil {
			errs = appendErrors(errs, fmt.Errorf("root HTTP route %s must not specify route", http.Name))
		}
		return errs
	}

	// This is to check delegate conflict
	if routeType == DelegateRoute {
		if http.Delegate != nil {
			errs = appendErrors(errs, errors.New("delegate HTTP route cannot contain delegate"))
		}
	}

	// check for conflicts
	if http.Redirect != nil {
		if len(http.Route) > 0 {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both route and redirect"))
		}

		if http.Fault != nil {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both fault and redirect"))
		}

		if http.Rewrite != nil {
			errs = appendErrors(errs, errors.New("HTTP route rule cannot contain both rewrite and redirect"))
		}

		if http.DirectResponse != nil {
			errs = appendErrors(errs, errors.New("HTTP route rule cannot contain both direct_response and redirect"))
		}
	} else if http.DirectResponse != nil {
		if len(http.Route) > 0 {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both route and direct_response"))
		}

		if http.Fault != nil {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both fault and direct_response"))
		}

		if http.Rewrite != nil {
			errs = appendErrors(errs, errors.New("HTTP route rule cannot contain both rewrite and direct_response"))
		}

		if http.Redirect != nil {
			errs = appendErrors(errs, errors.New("HTTP route rule cannot contain both redirect and direct_response"))
		}
	} else if len(http.Route) == 0 {
		errs = appendErrors(errs, errors.New("HTTP route, redirect or direct_response is required"))
	}

	if http.Mirror != nil && len(http.Mirrors) > 0 {
		errs = appendErrors(errs, errors.New("HTTP route cannot contain both mirror and mirrors"))
	}

	return errs
}

// isInternalHeader returns true if a header refers to an internal value that cannot be modified by Envoy
func isInternalHeader(headerKey string) bool {
	return strings.HasPrefix(headerKey, ":") || strings.EqualFold(headerKey, "host")
}

// isAuthorityHeader returns true if a header refers to the authority header
func isAuthorityHeader(headerKey string) bool {
	return strings.EqualFold(headerKey, ":authority") || strings.EqualFold(headerKey, "host")
}
