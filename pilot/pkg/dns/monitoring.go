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

package dns

import (
	"strconv"

	"github.com/miekg/dns"

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
		"dns_total_upstream_failures",
		"Total number of DNS requests forwarded to upstream.",
	)

	upstreamRequestDuration = monitoring.NewDistribution(
		"dns_upstream_request_time_ms",
		"Total time in milliseconds Istio takes to get DNS response from upstream.",
		[]float64{1, 3, 5, 10, 25, 50},
	)

	requestDuration = monitoring.NewDistribution(
		"dns_request_time_ms",
		"Total time in milliseconds Istio takes to serve DNS response.",
		[]float64{1, 3, 5, 10, 25, 50},
	)

	rcodeLabel = monitoring.MustCreateLabel("rcode")

	responses = monitoring.NewSum(
		"dns_responses_total",
		"Total number of DNS responses.",
		monitoring.WithLabels(rcodeLabel),
	)
)

func registerStats() {
	monitoring.MustRegister(requests)
	monitoring.MustRegister(upstreamRequests)
	monitoring.MustRegister(failures)
	monitoring.MustRegister(upstreamRequestDuration)
	monitoring.MustRegister(requestDuration)
	monitoring.MustRegister(responses)
}

func rcodeToLabelValue(rcode int) monitoring.LabelValue {
	name, found := dns.RcodeToString[rcode]
	if found {
		return rcodeLabel.Value(name)
	}
	return rcodeLabel.Value(strconv.Itoa(rcode))
}
