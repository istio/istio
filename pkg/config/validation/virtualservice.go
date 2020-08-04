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

	networking "istio.io/api/networking/v1alpha3"
)

func validateRootHTTPRoute(http *networking.HTTPRoute) (errs error) {
	if http.Delegate == nil {
		errs = appendErrors(errs, fmt.Errorf("root HTTP route %s must specify delegate", http.Name))
	}
	// only delegate can be specified
	if http.Redirect != nil {
		errs = appendErrors(errs, fmt.Errorf("root HTTP route %s must not specify redirect", http.Name))
	}
	if http.Route != nil {
		errs = appendErrors(errs, fmt.Errorf("root HTTP route %s must not specify route", http.Name))
	}

	errs = appendErrors(errs, validateChainingHTTPRoute(http))
	return
}

func validateDelegateHTTPRoute(http *networking.HTTPRoute) (errs error) {
	if http.Delegate != nil {
		errs = appendErrors(errs, errors.New("delegate HTTP route cannot contain delegate"))
	}
	// check for conflicts
	if http.Redirect != nil {
		if len(http.Route) > 0 {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both route and redirect"))
		}
	} else if len(http.Route) == 0 {
		errs = appendErrors(errs, errors.New("HTTP route or redirect is required"))
	}
	errs = appendErrors(errs, validateChainingHTTPRoute(http))
	return
}

// TODO(@hzxuzhonghu): deduplicate with validateHTTPRoute
func validateChainingHTTPRoute(http *networking.HTTPRoute) (errs error) {
	// check for conflicts
	if http.Redirect != nil {
		if http.Fault != nil {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both fault and redirect"))
		}

		if http.Rewrite != nil {
			errs = appendErrors(errs, errors.New("HTTP route rule cannot contain both rewrite and redirect"))
		}
	}
	// header manipulation
	for name := range http.Headers.GetRequest().GetAdd() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for name := range http.Headers.GetRequest().GetSet() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for _, name := range http.Headers.GetRequest().GetRemove() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for name := range http.Headers.GetResponse().GetAdd() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for name := range http.Headers.GetResponse().GetSet() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for _, name := range http.Headers.GetResponse().GetRemove() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}

	errs = appendErrors(errs, validateCORSPolicy(http.CorsPolicy))
	errs = appendErrors(errs, validateHTTPFaultInjection(http.Fault))

	for _, match := range http.Match {
		if match != nil {
			if containRegexMatch(match.Uri) {
				errs = appendErrors(errs, errors.New("url match does not support regex match for delegating"))
			}
			if containRegexMatch(match.Scheme) {
				errs = appendErrors(errs, errors.New("scheme match does not support regex match for delegating"))
			}
			if containRegexMatch(match.Method) {
				errs = appendErrors(errs, errors.New("method match does not support regex match for delegating"))
			}
			if containRegexMatch(match.Authority) {
				errs = appendErrors(errs, errors.New("authority match does not support regex match for delegating"))
			}

			for name, header := range match.Headers {
				if header == nil {
					errs = appendErrors(errs, fmt.Errorf("header match %v cannot be null", name))
				}
				if containRegexMatch(header) {
					errs = appendErrors(errs, fmt.Errorf("header match %v does not support regex match for delegating", name))
				}
				errs = appendErrors(errs, ValidateHTTPHeaderName(name))
			}
			for name, param := range match.QueryParams {
				if param == nil {
					errs = appendErrors(errs, fmt.Errorf("query param match %v cannot be null", name))
				}
				if containRegexMatch(param) {
					errs = appendErrors(errs, fmt.Errorf("query param match %v does not support regex match for delegating", name))
				}
			}
			for name, header := range match.WithoutHeaders {
				if header == nil {
					errs = appendErrors(errs, fmt.Errorf("withoutHeaders match %v cannot be null", name))
				}
				if containRegexMatch(header) {
					errs = appendErrors(errs, fmt.Errorf("withoutHeaders match %v does not support regex match for delegating", name))
				}
				errs = appendErrors(errs, ValidateHTTPHeaderName(name))
			}

			if match.Port != 0 {
				errs = appendErrors(errs, ValidatePort(int(match.Port)))
			}

			// delegate only applies to gateway, so sourceLabels or sourceNamespace does not apply.
			if len(match.SourceLabels) != 0 {
				errs = appendErrors(errs, fmt.Errorf("sourceLabels match can not specify on delegate"))
			}
			if match.SourceNamespace != "" {
				errs = appendErrors(errs, fmt.Errorf("sourceNamespace match can not specify on delegate"))
			}
			if len(match.Gateways) != 0 {
				errs = appendErrors(errs, fmt.Errorf("gateways match can not specify on delegate"))
			}
		}
	}

	if http.MirrorPercent != nil {
		if value := http.MirrorPercent.GetValue(); value > 100 {
			errs = appendErrors(errs, fmt.Errorf("mirror_percent must have a max value of 100 (it has %d)", value))
		}
	}

	if http.MirrorPercentage != nil {
		if value := http.MirrorPercentage.GetValue(); value > 100 {
			errs = appendErrors(errs, fmt.Errorf("mirror_percentage must have a max value of 100 (it has %f)", value))
		}
	}

	errs = appendErrors(errs, validateDestination(http.Mirror))
	errs = appendErrors(errs, validateHTTPRedirect(http.Redirect))
	errs = appendErrors(errs, validateHTTPRetry(http.Retries))
	errs = appendErrors(errs, validateHTTPRewrite(http.Rewrite))
	errs = appendErrors(errs, validateHTTPRouteDestinations(http.Route))
	if http.Timeout != nil {
		errs = appendErrors(errs, ValidateDurationGogo(http.Timeout))
	}

	return
}

func containRegexMatch(config *networking.StringMatch) bool {
	if config == nil {
		return false
	}
	if config.GetRegex() != "" {
		return true
	}
	return false
}
