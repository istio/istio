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

package registryonly

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

var (
	prom prometheus.Instance
)

func TestMain(m *testing.M) {
	var ist istio.Instance
	framework.NewSuite("outbound_traffic_policy_registry_only", m).
		RequireEnvironment(environment.Kube).
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
  telemetry:
    enabled: true
values:
  global:
    outboundTrafficPolicy:
      mode: REGISTRY_ONLY
  telemetry:
    v1:
      enabled: true
    v2:
      enabled: false`
}

func TestOutboundTrafficPolicyRegistryOnly_NetworkingResponse(t *testing.T) {
	expected := map[string][]string{
		"http":        {"502"}, // HTTP will return an error code
		"http_egress": {"200"}, // We define the virtual service in the namespace, so we should be able to reach it
		"https":       {},      // HTTPS will direct to blackhole cluster, giving no response
		"tcp":         {},      // TCP will direct to blackhole cluster, giving no response
	}
	outboundtrafficpolicy.RunExternalRequestResponseCodeTest(expected, t)
}

func TestOutboundTrafficPolicyRegistryOnly_MetricsResponse(t *testing.T) {
	expected := map[string]outboundtrafficpolicy.MetricsResponse{
		"http": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{destination_service_name="BlackHoleCluster",response_code="502"})`,
		}, // HTTP will return an error code
		"http_egress": {
			Metric:    "istio_requests_total",
			PromQuery: `sum(istio_requests_total{destination_service_name="istio-egressgateway",response_code="200"})`,
		}, // we define the virtual service in the namespace, so we should be able to reach it
		"https": {
			Metric:    "istio_tcp_connections_closed_total",
			PromQuery: `sum(istio_tcp_connections_closed_total{destination_service="BlackHoleCluster",destination_service_name="BlackHoleCluster"})`,
		}, // HTTPS will direct to blackhole cluster, giving no response
	}
	// destination_service="BlackHoleCluster" does not get filled in when using sidecar scoping
	outboundtrafficpolicy.RunExternalRequestMetricsTest(prom, expected, t)
}

func setupPrometheus(ctx resource.Context) (err error) {
	prom, err = prometheus.New(ctx, prometheus.Config{})
	return err
}
