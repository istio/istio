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

	"istio.io/istio/pkg/test/echo/common/scheme"
)

func TestOutboundTrafficPolicy_AllowAny(t *testing.T) {
	cases := []*TestCase{
		{
			Name:     "HTTP Traffic",
			PortName: "http",
			Scheme:   scheme.HTTP,
			Expected: Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{reporter="source",destination_service_name="PassthroughCluster",response_code="200"})`,
				ResponseCode:    []string{"200"},
			},
		},
		{
			Name:     "HTTPS Traffic",
			PortName: "https",
			// TODO: set up TLS here instead of just sending HTTP. We get a false positive here
			Scheme: scheme.HTTP,
			Expected: Expected{
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
			Scheme:   scheme.HTTP,
			Expected: Expected{
				Metric:          "istio_requests_total",
				PromQueryFormat: `sum(istio_requests_total{reporter="source",destination_service_name="istio-egressgateway",response_code="200"})`,
				ResponseCode:    []string{"200"},
			},
		},
		// TODO add HTTPS through gateway
		{
			Name:     "TCP",
			PortName: "tcp",
			Scheme:   scheme.TCP,
			Expected: Expected{
				Metric:          "istio_tcp_connections_closed_total",
				PromQueryFormat: `sum(istio_tcp_connections_closed_total{reporter="destination",source_workload="client-v1",destination_workload="destination-v1"})`,
				ResponseCode:    []string{"200"},
			},
		},
	}
	RunExternalRequest(cases, prom, AllowAny, t)
}
