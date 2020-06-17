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

package common

import (
	"istio.io/pkg/monitoring"
)

type EchoMetrics struct {
	HTTPRequests monitoring.Metric
	GrpcRequests monitoring.Metric
	TCPRequests  monitoring.Metric
}

var (
	PortLabel = monitoring.MustCreateLabel("port")
	Metrics   = &EchoMetrics{
		HTTPRequests: monitoring.NewSum(
			"istio_echo_http_requests_total",
			"The number of http requests total",
		),
		GrpcRequests: monitoring.NewSum(
			"istio_echo_grpc_requests_total",
			"The number of grpc requests total",
		),
		TCPRequests: monitoring.NewSum(
			"istio_echo_tcp_requests_total",
			"The number of tcp requests total",
		),
	}
)

func init() {
	monitoring.MustRegister(Metrics.HTTPRequests, Metrics.GrpcRequests, Metrics.TCPRequests)
}
