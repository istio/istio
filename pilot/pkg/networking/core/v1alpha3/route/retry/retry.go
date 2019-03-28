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

package retry

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
)

// DefaultPolicy gets a copy of the default retry policy.
func DefaultPolicy() *route.RetryPolicy {
	policy := route.RetryPolicy{
		NumRetries:           &types.UInt32Value{Value: 2},
		RetryOn:              "connect-failure,refused-stream,unavailable,cancelled,resource-exhausted,retriable-status-codes",
		RetriableStatusCodes: []uint32{http.StatusServiceUnavailable},
		RetryHostPredicate: []*route.RetryPolicy_RetryHostPredicate{
			{
				// to configure retries to prefer hosts that haven’t been attempted already,
				// the builtin `envoy.retry_host_predicates.previous_hosts` predicate can be used.
				Name: "envoy.retry_host_predicates.previous_hosts",
			},
		},
		HostSelectionRetryMaxAttempts: 3,
	}
	return &policy
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
func ConvertPolicy(in *networking.HTTPRetry) *route.RetryPolicy {
	if in == nil {
		// No policy was set, use a default.
		return DefaultPolicy()
	}

	if in.Attempts <= 0 {
		// Configuration is explicitly disabling the retry policy.
		return nil
	}

	// A policy was specified. Start with the default and override with user-provided fields where appropriate.
	out := DefaultPolicy()
	out.NumRetries = &types.UInt32Value{Value: uint32(in.GetAttempts())}

	if in.RetryOn != "" {
		// Allow the incoming configuration to specify both Envoy RetryOn and RetriableStatusCodes. Any integers are
		// assumed to be status codes.
		out.RetryOn, out.RetriableStatusCodes = parseRetryOn(in.RetryOn)
	}

	if in.PerTryTimeout != nil {
		d := util.GogoDurationToDuration(in.PerTryTimeout)
		out.PerTryTimeout = &d
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
		i, _ := strconv.Atoi(part)

		if http.StatusText(i) != "" {
			codes = append(codes, uint32(i))
		} else {
			tojoin = append(tojoin, part)
		}
	}

	return strings.Join(tojoin, ","), codes
}
