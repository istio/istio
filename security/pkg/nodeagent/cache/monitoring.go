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

package cache

import "istio.io/pkg/monitoring"

const (
	TokenExchange = "token_exchange"
	CSR           = "csr"
)

var (
	RequestType = monitoring.MustCreateLabel("request_type")
)

// Metrics for outgoing requests from citadel agent to external services such as token exchange server or a CA.
// This is different from incoming request metrics (i.e. from Envoy to citadel agent).
var (
	outgoingLatency = monitoring.NewSum(
		"outgoing_latency",
		"The latency of "+
			"outgoing requests (e.g. to a token exchange server, CA, etc.) in milliseconds.",
		monitoring.WithLabels(RequestType), monitoring.WithUnit(monitoring.Milliseconds))

	numOutgoingRequests = monitoring.NewSum(
		"num_outgoing_requests",
		"Number of total outgoing requests (e.g. to a token exchange server, CA, etc.)",
		monitoring.WithLabels(RequestType))

	numOutgoingRetries = monitoring.NewSum(
		"num_outgoing_retries",
		"Number of outgoing retry requests (e.g. to a token exchange server, CA, etc.)",
		monitoring.WithLabels(RequestType))

	numFailedOutgoingRequests = monitoring.NewSum(
		"num_failed_outgoing_requests",
		"Number of failed outgoing requests (e.g. to a token exchange server, CA, etc.)",
		monitoring.WithLabels(RequestType))
)

func init() {
	monitoring.MustRegister(
		outgoingLatency,
		numOutgoingRequests,
		numOutgoingRetries,
		numFailedOutgoingRequests,
	)
}
