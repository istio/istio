// Copyright 2019 Istio Authors
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

package monitoring

import "github.com/prometheus/client_golang/prometheus"

const (
	// Namespace of the metrics in the Prometheus system. This is not the namespace where Citadel agents
	// reside, e.g. istio-system.
	citadelAgentMetricsNamespace = "citadel_agent"
)

var (
	totalOutgoingRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: citadelAgentMetricsNamespace,
		Name:      "total_outgoing_requests",
		Help:      "the total requests from Citadel agent to the token exchange service or to the CA",
	}, []string{"request_type"})

	totalSuccessfulOutgoingRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: citadelAgentMetricsNamespace,
		Name:      "total_outgoing_successful_requests",
		Help: "the total successful requests from Citadel agent to the token exchange service or" +
			" to the CA",
	}, []string{"request_type"})

	numRetries = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: citadelAgentMetricsNamespace,
		Name:      "num_retries",
		Help: "numRetries is the number of retries from Citadel agent to the token exchange service" +
			" or to the CA from each request.",
	}, []string{"request_type"})

	outgoingLatency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: citadelAgentMetricsNamespace,
		Name:      "latency",
		Help:      "the round-trip time from Citadel agent to the token exchange service or to the CA",
	}, []string{"request_type"})
)

// metrics are counters for Citadel agent related operations at the top level.
type metrics struct {
	// totalOutgoingRequests is the total requests from Citadel agent to the token exchange service or to the
	// CA.
	totalOutgoingRequests *prometheus.CounterVec
	// totalSuccessfulOutgoingRequests is the total successful requests from Citadel agent to the token
	// exchange service or to the CA.
	totalSuccessfulOutgoingRequests *prometheus.CounterVec
	// numRetries is the number of retries from Citadel agent to the token exchange service or to the
	// CA for each request.
	numRetries *prometheus.GaugeVec
	// outgoingLatency is the round-trip time from Citadel agent to the token exchange service or to
	// the CA for each request.
	outgoingLatency *prometheus.GaugeVec
}

// newMetrics creates a new monitoring metrics for Citadel agent.
func newMetrics() metrics {
	return metrics{
		totalOutgoingRequests:           totalOutgoingRequests,
		totalSuccessfulOutgoingRequests: totalSuccessfulOutgoingRequests,
		numRetries:                      numRetries,
		outgoingLatency:                 outgoingLatency,
	}
}
