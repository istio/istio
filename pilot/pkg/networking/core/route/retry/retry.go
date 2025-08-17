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

package retry

import (
	"net/http"
	"strconv"
	"strings"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	previouspriorities "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/priority/previous_priorities/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

var defaultRetryPriorityTypedConfig = protoconv.MessageToAny(buildPreviousPrioritiesConfig())

// DefaultPolicy gets a copy of the default retry policy.
func DefaultPolicy() *route.RetryPolicy {
	policy := defaultPolicy()
	policy.RetryHostPredicate = []*route.RetryPolicy_RetryHostPredicate{
		// to configure retries to prefer hosts that haven’t been attempted already,
		// the builtin `envoy.retry_host_predicates.previous_hosts` predicate can be used.
		xdsfilters.RetryPreviousHosts,
	}
	return policy
}

func defaultPolicy() *route.RetryPolicy {
	return &route.RetryPolicy{
		NumRetries: &wrappers.UInt32Value{Value: 2},
		RetryOn:    "connect-failure,refused-stream,unavailable,cancelled,retriable-status-codes",
		// TODO: allow this to be configured via API.
		HostSelectionRetryMaxAttempts: 5,
	}
}

// DefaultConsistentHashPolicy gets a copy of the default retry policy without previous host predicate.
// When Consistent Hashing is enabled, we don't want to use other hosts during retries.
func DefaultConsistentHashPolicy() *route.RetryPolicy {
	return defaultPolicy()
}

// ConvertPolicy converts the given Istio retry policy to an Envoy policy.
//
// If in is nil, DefaultPolicy is returned.
//
// If in.Attempts == 0, returns nil.
//
// Otherwise, the returned policy is DefaultPolicy with the following overrides:
//
// - NumRetries: set from in.Attempts
//
// - RetryOn, RetriableStatusCodes: set from in.RetryOn (if specified). RetriableStatusCodes
// is appended when encountering parts that are valid HTTP status codes.
//
// - PerTryTimeout: set from in.PerTryTimeout (if specified)
func ConvertPolicy(in *networking.HTTPRetry, hashPolicy bool) *route.RetryPolicy {
	var out *route.RetryPolicy
	if hashPolicy {
		out = DefaultConsistentHashPolicy()
	} else {
		out = DefaultPolicy()
	}
	if in == nil {
		// No policy was set, use a default.
		return out
	}

	if in.Attempts <= 0 {
		// Configuration is explicitly disabling the retry policy.
		return nil
	}

	// A policy was specified. Start with the default and override with user-provided fields where appropriate.
	out.NumRetries = &wrappers.UInt32Value{Value: uint32(in.Attempts)}

	if in.RetryOn != "" {
		// Allow the incoming configuration to specify both Envoy RetryOn and RetriableStatusCodes. Any integers are
		// assumed to be status codes.
		out.RetryOn, out.RetriableStatusCodes = parseRetryOn(in.RetryOn)
		// If user has just specified HTTP status codes in retryOn but have not specified "retriable-status-codes", let us add it.
		if len(out.RetriableStatusCodes) > 0 && !strings.Contains(out.RetryOn, "retriable-status-codes") {
			out.RetryOn += ",retriable-status-codes"
		}
	}

	if in.PerTryTimeout != nil {
		out.PerTryTimeout = in.PerTryTimeout
	}

	// User has specified RetryIgnorePreviousHosts, so honor it.
	if in.RetryIgnorePreviousHosts != nil {
		var retryHostPredicate []*route.RetryPolicy_RetryHostPredicate
		if in.RetryIgnorePreviousHosts.GetValue() {
			retryHostPredicate = []*route.RetryPolicy_RetryHostPredicate{
				// to configure retries to prefer hosts that haven’t been attempted already,
				// the builtin `envoy.retry_host_predicates.previous_hosts` predicate can be used.
				xdsfilters.RetryPreviousHosts,
			}
		}
		out.RetryHostPredicate = retryHostPredicate
	}

	if in.RetryRemoteLocalities != nil && in.RetryRemoteLocalities.GetValue() {
		out.RetryPriority = &route.RetryPolicy_RetryPriority{
			Name: "envoy.retry_priorities.previous_priorities",
			ConfigType: &route.RetryPolicy_RetryPriority_TypedConfig{
				TypedConfig: defaultRetryPriorityTypedConfig,
			},
		}
	}

	if in.Backoff != nil {
		out.RetryBackOff = &route.RetryPolicy_RetryBackOff{
			BaseInterval: in.Backoff,
		}
	}

	return out
}

func parseRetryOn(retryOn string) (string, []uint32) {
	codes := make([]uint32, 0)
	tojoin := make([]string, 0)

	parts := strings.Split(retryOn, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Try converting it to an integer to see if it's a valid HTTP status code.
		i, err := strconv.Atoi(part)

		if err == nil && http.StatusText(i) != "" {
			codes = append(codes, uint32(i))
		} else {
			tojoin = append(tojoin, part)
		}
	}

	return strings.Join(tojoin, ","), codes
}

// buildPreviousPrioritiesConfig builds a PreviousPrioritiesConfig with a default
// value for UpdateFrequency which indicated how often to update the priority.
func buildPreviousPrioritiesConfig() *previouspriorities.PreviousPrioritiesConfig {
	return &previouspriorities.PreviousPrioritiesConfig{
		UpdateFrequency: int32(2),
	}
}
