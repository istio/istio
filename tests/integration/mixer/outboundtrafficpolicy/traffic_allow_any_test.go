// Copyright 2020 Istio Authors
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

package outboundtrafficpolicy

import (
	"testing"
)

func TestOutboundTrafficPolicy_AllowAny(t *testing.T) {
	expected := map[string]Response{
		"http": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{reporter="source",destination_service_name="PassthroughCluster",response_code="200"})`,
			Code:      []string{"200"},
		},
		"http_egress": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{reporter="source",destination_service_name="istio-egressgateway",response_code="200"})`,
			Code:      []string{"200"},
		},
		"https": {
			Metric:    "istio_tcp_connections_opened_total",
			PromQuery: `sum(istio_tcp_connections_opened_total{reporter="source",destination_service_name="PassthroughCluster"})`,
			Code:      []string{"200"},
		},
		"tcp": {
			Metric:    "istio_tcp_connections_closed_total",
			PromQuery: `sum(istio_tcp_connections_closed_total{reporter="destination",source_workload="client-v1",destination_workload="destination-v1"})`,
			Code:      []string{"200"},
		},
	}
	RunExternalRequest(prom, AllowAny, expected, t)
}
