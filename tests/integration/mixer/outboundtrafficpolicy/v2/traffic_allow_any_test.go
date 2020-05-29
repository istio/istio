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

package v2

import (
	"testing"

	"istio.io/istio/tests/integration/mixer/outboundtrafficpolicy"
)

func TestOutboundTrafficPolicy_AllowAny_TelemetryV2(t *testing.T) {
	cases := []*outboundtrafficpolicy.TestCase{
		{
			Name:     "HTTP Traffic",
			PortName: "http",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{reporter="source",destination_service_name="PassthroughCluster",response_code="200"})`,
				ResponseCode:    []string{"200"},
			},
		},
		{
			Name:     "HTTPS Traffic",
			PortName: "https",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_tcp_connections_opened_total",
				PromQueryFormat: `sum(istio_tcp_connections_opened_total{reporter="source",destination_service_name="PassthroughCluster"})`,
				ResponseCode:    []string{"200"},
			},
		},
		{
			Name:     "HTTPS Traffic Conflict",
			PortName: "https-conflict",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_tcp_connections_opened_total",
				PromQueryFormat: `sum(istio_tcp_connections_opened_total{reporter="source",destination_service_name="PassthroughCluster"})`,
				ResponseCode:    []string{"200"},
			},
		},
		{
			Name:     "HTTP Traffic Egress",
			PortName: "http",
			Host:     "some-external-site.com",
			Gateway:  true,
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{reporter="source",destination_service_name="istio-egressgateway.istio-system.svc.cluster.local",response_code="200"})`, // nolint: lll
				ResponseCode:    []string{"200"},
			},
		},
		// TODO add HTTPS through gateway
		{
			Name:     "TCP",
			PortName: "tcp",
			Expected: outboundtrafficpolicy.Expected{
				// TODO(https://github.com/istio/istio/issues/22717) re-enable TCP
				//Metric:          "istio_tcp_connections_closed_total",
				//PromQueryFormat: `sum(istio_tcp_connections_closed_total{reporter="source",destination_service_name="PassthroughCluster",source_workload="client-v1"})`,
				ResponseCode: []string{"200"},
			},
		},
		{
			Name:     "TCP Conflict",
			PortName: "tcp",
			Expected: outboundtrafficpolicy.Expected{
				// TODO(https://github.com/istio/istio/issues/22717) re-enable TCP
				//Metric:          "istio_tcp_connections_closed_total",
				//PromQueryFormat: `sum(istio_tcp_connections_closed_total{reporter="source",destination_service_name="PassthroughCluster",source_workload="client-v1"})`,
				ResponseCode: []string{"200"},
			},
		},
	}

	outboundtrafficpolicy.RunExternalRequest(cases, prom, outboundtrafficpolicy.AllowAny, t)
}
