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

func TestOutboundTrafficPolicy_RegistryOnly(t *testing.T) {
	expected := map[string]Response{
		"http": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{destination_service_name="BlackHoleCluster",response_code="502"})`,
			Code:      []string{"502"},
		}, // HTTP will return an error code
		"http_egress": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{destination_service_name="istio-egressgateway",response_code="200"})`,
			Code:      []string{"200"},
		}, // we define the virtual service in the namespace, so we should be able to reach it
		"https": {
			Metric:    "istio_tcp_connections_closed_total",
			PromQuery: `sum(istio_tcp_connections_closed_total{destination_service="BlackHoleCluster",destination_service_name="BlackHoleCluster"})`,
			Code:      []string{},
		}, // HTTPS will direct to blackhole cluster, giving no response
		"tcp": {
			Metric:    "",
			PromQuery: "",
			Code:      []string{},
		}, // TCP will direct to blackhole cluster
	}

	RunExternalRequest(prom, RegistryOnly, expected, t)

}
