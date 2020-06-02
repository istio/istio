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

func TestOutboundTrafficPolicy_RegistryOnly_TelemetryV2(t *testing.T) {
	cases := []*outboundtrafficpolicy.TestCase{
		{
			Name:     "HTTP Traffic",
			PortName: "http",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{destination_service_name="BlackHoleCluster",response_code="502"})`,
				ResponseCode:    []string{"502"},
			},
		},
		{
			Name:     "HTTPS Traffic",
			PortName: "https",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{destination_service_name="BlackHoleCluster"})`,
				ResponseCode:    []string{},
			},
		},
		{
			Name:     "HTTPS Traffic Conflict",
			PortName: "https-conflict",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{destination_service_name="BlackHoleCluster"})`,
				ResponseCode:    []string{},
			},
		},
		{
			Name:     "HTTP Traffic Egress",
			PortName: "http",
			Host:     "some-external-site.com",
			Gateway:  true,
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{destination_service_name="istio-egressgateway.istio-system.svc.cluster.local",response_code="200"})`,
				ResponseCode:    []string{"200"},
			},
		},
		// TODO add HTTPS through gateway
		{
			Name:     "TCP",
			PortName: "tcp",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{reporter="source",destination_service_name="BlackHoleCluster",source_workload="client-v1"})`,
				ResponseCode:    []string{},
			},
		},
		{
			Name:     "TCP Conflict",
			PortName: "tcp-conflict",
			Expected: outboundtrafficpolicy.Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{reporter="source",destination_service_name="BlackHoleCluster",source_workload="client-v1"})`,
				ResponseCode:    []string{},
			},
		},
	}

	// destination_service="BlackHoleCluster" does not get filled in when using sidecar scoping
	outboundtrafficpolicy.RunExternalRequest(cases, prom, outboundtrafficpolicy.RegistryOnly, t)
}
