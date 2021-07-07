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

package client

import (
	"istio.io/pkg/monitoring"
)

var (
	requests = monitoring.NewSum(
		"dns_requests_total",
		"Total number of DNS requests.",
	)

	upstreamRequests = monitoring.NewSum(
		"dns_upstream_requests_total",
		"Total number of DNS requests forwarded to upstream.",
	)

	failures = monitoring.NewSum(
		"dns_upstream_failures_total",
		"Total number of DNS requests forwarded to upstream.",
	)

	requestDuration = monitoring.NewDistribution(
		"dns_upstream_request_duration_seconds",
		"Total time in seconds Istio takes to get DNS response from upstream.",
		[]float64{.005, .001, 0.01, 0.1, 1, 5},
	)
)

func registerStats() {
	monitoring.MustRegister(requests)
	monitoring.MustRegister(upstreamRequests)
	monitoring.MustRegister(failures)
	monitoring.MustRegister(requestDuration)
}
