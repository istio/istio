package blackhole

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/components/sleep"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	util "istio.io/istio/tests/integration/mixer"
)

var (
	sleepNs    namespace.Instance
	ist        istio.Instance
	prom       prometheus.Instance
)

func TestPassthroughClusterMetric(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			sleepInst := sleep.DeployOrFail(t, ctx, sleep.Config{Namespace: sleepNs, Cfg: sleep.Sleep})
			respCode, err := sleepInst.Curl("http://istio.io")
			if err != nil {
				t.Fatalf("Unable to exec curl http://istio.io from sleep pod: %v", err)
			}
			if respCode != "301" {
				t.Fatalf("301 not returned from sleep pod; received http response code: %s", respCode)
			}
			query := `sum(istio_requests_total{destination_service_name="PassthroughCluster"})`
			util.ValidateMetric(t, prom, query, "istio_requests_total", 1)

			respCode, err = sleepInst.Curl("https://istio.io")
			if err != nil {
				t.Fatalf("Unable to exec curl https://istio.io from sleep pod: %v", err)
			}
			if respCode != "200" {
				t.Fatalf("200 not returned from sleep pod; received http response code: %s", respCode)
			}
			query = `sum(istio_tcp_connections_closed_total{destination_service="PassthroughCluster",destination_service_name="PassthroughCluster"})`
			util.ValidateMetric(t, prom, query, "istio_tcp_connections_closed_total", 1)
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("mixer_telemetry_metrics", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  prometheus:
    enabled: true
  global:
    disablePolicyChecks: false
    outboundTrafficPolicy:
      mode: ALLOW_ANY
  telemetry:
    v1:
      enabled: true
    v2:
      enabled: false
components:
  policy:
    enabled: true
  telemetry:
    enabled: true`
		})).
		Setup(testsetup).
		Run()
}

func testsetup(ctx resource.Context) (err error) {
	sleepNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-sleep",
		Inject: true,
	})
	if err != nil {
		return
	}
	g, err := galley.New(ctx, galley.Config{})
	if err != nil {
		return err
	}
	if _, err = mixer.New(ctx, mixer.Config{Galley: g}); err != nil {
		return err
	}
	prom, err = prometheus.New(ctx)
	if err != nil {
		return err
	}

	return nil
}
