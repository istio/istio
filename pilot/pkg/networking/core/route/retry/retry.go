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

// Cached immutable default values to avoid repeated allocations.
// These are shared and must not be modified.
var (
	defaultNumRetries = &wrappers.UInt32Value{Value: 2}
	defaultRetryOn    = "connect-failure,refused-stream,unavailable,cancelled,retriable-status-codes"

	// Shared slice for RetryHostPredicate - immutable, safe to share
	defaultRetryHostPredicate = []*route.RetryPolicy_RetryHostPredicate{
		xdsfilters.RetryPreviousHosts,
	}

	// Cached default policy with RetryHostPredicate (for non-consistent-hash cases)
	cachedDefaultPolicy = &route.RetryPolicy{
		NumRetries:                    defaultNumRetries,
		RetryOn:                       defaultRetryOn,
		HostSelectionRetryMaxAttempts: 5,
		RetryHostPredicate:            defaultRetryHostPredicate,
	}

	// Cached default policy without RetryHostPredicate (for consistent-hash cases)
	cachedDefaultConsistentHashPolicy = &route.RetryPolicy{
		NumRetries:                    defaultNumRetries,
		RetryOn:                       defaultRetryOn,
		HostSelectionRetryMaxAttempts: 5,
	}

	cachedRetryRemoteLocalitiesPredicate = &route.RetryPolicy_RetryPriority{
		Name: "envoy.retry_priorities.previous_priorities",
		ConfigType: &route.RetryPolicy_RetryPriority_TypedConfig{
			TypedConfig: defaultRetryPriorityTypedConfig,
		},
	}
)

// DefaultPolicy returns the default retry policy.
// The returned policy is a shared immutable instance and must not be modified.
// If modification is needed, use newRetryPolicy() instead.
func DefaultPolicy() *route.RetryPolicy {
	return cachedDefaultPolicy
}

// DefaultConsistentHashPolicy returns the default retry policy without previous host predicate.
// When Consistent Hashing is enabled, we don't want to use other hosts during retries.
// The returned policy is a shared immutable instance and must not be modified.
func DefaultConsistentHashPolicy() *route.RetryPolicy {
	return cachedDefaultConsistentHashPolicy
}

// newRetryPolicy creates a new mutable retry policy with default values.
// Use this when the policy needs to be customized.
func newRetryPolicy(hashPolicy bool) *route.RetryPolicy {
	policy := &route.RetryPolicy{
		NumRetries:                    &wrappers.UInt32Value{Value: 2},
		RetryOn:                       defaultRetryOn,
		HostSelectionRetryMaxAttempts: 5,
	}
	if !hashPolicy {
		policy.RetryHostPredicate = defaultRetryHostPredicate
	}
	return policy
}

// ConvertPolicy converts the given Istio retry policy to an Envoy policy.
func ConvertPolicy(in *networking.HTTPRetry, hashPolicy bool) *route.RetryPolicy {
	// Fast path: if no custom config, return cached immutable default
	if in == nil {
		if hashPolicy {
			return cachedDefaultConsistentHashPolicy
		}
		return cachedDefaultPolicy
	}

	if in.Attempts <= 0 {
		// Configuration is explicitly disabling the retry policy.
		return nil
	}

	// A policy was specified - we need a mutable copy to customize
	out := newRetryPolicy(hashPolicy)

	// Override with user-provided fields
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
			retryHostPredicate = defaultRetryHostPredicate
		}
		out.RetryHostPredicate = retryHostPredicate
	}

	if in.RetryRemoteLocalities != nil && in.RetryRemoteLocalities.GetValue() {
		out.RetryPriority = cachedRetryRemoteLocalitiesPredicate
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
