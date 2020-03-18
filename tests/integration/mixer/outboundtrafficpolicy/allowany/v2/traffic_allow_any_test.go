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

package v2

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/tests/integration/mixer/outboundtrafficpolicy"
)

func TestMain(m *testing.M) {
	var ist istio.Instance
	framework.
		NewSuite("outbound_traffic_policy_allow_any", m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, setupConfig)).
		Setup(setupPrometheus).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
components:
  egressGateways:
  - enabled: true
values:
  global:
    outboundTrafficPolicy:
      mode: ALLOW_ANY`
}
var (
	prom prometheus.Instance
)

func TestOutboundTrafficPolicyAllowAny_NetworkingResponse(t *testing.T) {
	expected := map[string][]string{
		"http":        {"200"},
		"http_egress": {"200"},
		"https":       {"200"},
	}
	outboundtrafficpolicy.RunExternalRequestResponseCodeTest(expected, t)
}

func TestOutboundTrafficPolicyAllowAny_MetricsResponse(t *testing.T) {
	expected := map[string]outboundtrafficpolicy.MetricsResponse{
		"http": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{reporter="source",destination_service_name="PassthroughCluster",response_code="200"})`,
		}, // HTTP will return an error code
		"http_egress": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{reporter="source",destination_service_name="PassthroughCluster",response_code="200"})`,
		}, // we define the virtual service in the namespace, so we should be able to reach it
		"https": {
			Metric:    "istio_tcp_connections_closed_total",
			PromQuery: `sum(istio_requests_total{reporter="source",destination_service_name="PassthroughCluster",response_code="200"})`,
		}, // HTTPS will direct to blackhole cluster, giving no response
	}
	outboundtrafficpolicy.RunExternalRequestMetricsTest(prom, expected, t)
}

func setupPrometheus(ctx resource.Context) (err error) {
	prom, err = prometheus.New(ctx, prometheus.Config{})
	return err
}
