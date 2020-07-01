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

package outboundtrafficpolicy

import (
	"testing"

	trafficpolicytest "istio.io/istio/tests/integration/telemetry/outboundtrafficpolicy"
)

func TestOutboundTrafficPolicy_RegistryOnly_Mixer(t *testing.T) {
	cases := []*trafficpolicytest.TestCase{
		{
			Name:     "HTTP Traffic",
			PortName: "http",
			Expected: trafficpolicytest.Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{destination_service_name="BlackHoleCluster",response_code="502"})`,
				ResponseCode:    []string{"502"},
			},
		},
		{
			Name:     "HTTPS Traffic",
			PortName: "https",
			Expected: trafficpolicytest.Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{destination_service="BlackHoleCluster",destination_service_name="BlackHoleCluster"})`,
				ResponseCode:    []string{},
			},
		},
		{
			Name:     "HTTPS Traffic Conflict",
			PortName: "https-conflict",
			Expected: trafficpolicytest.Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{destination_service="BlackHoleCluster",destination_service_name="BlackHoleCluster"})`,
				ResponseCode:    []string{},
			},
		},
		{
			Name:     "HTTP Traffic Egress",
			PortName: "http",
			Host:     "some-external-site.com",
			Expected: trafficpolicytest.Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{destination_service_name="istio-egressgateway",response_code="200"})`,
				ResponseCode:    []string{"200"},
				Metadata: map[string]string{
					// We inject this header in the VirtualService
					"Handled-By-Egress-Gateway": "true",
				},
			},
		},
		// TODO add HTTPS through gateway
		{
			Name:     "TCP",
			PortName: "tcp",
			Expected: trafficpolicytest.Expected{
				// TODO(https://github.com/istio/istio/issues/22735) add metrics
				Metric:          "",
				PromQueryFormat: "",
				ResponseCode:    []string{},
			},
		},
		{
			Name:     "TCP Conflict",
			PortName: "tcp-conflict",
			Expected: trafficpolicytest.Expected{
				// TODO(https://github.com/istio/istio/issues/22735) add metrics
				Metric:          "",
				PromQueryFormat: "",
				ResponseCode:    []string{},
			},
		},
	}

	trafficpolicytest.RunExternalRequest(cases, prom, trafficpolicytest.RegistryOnly, t)

}
