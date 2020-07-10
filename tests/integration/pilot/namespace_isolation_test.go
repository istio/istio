package pilot

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
	"testing"
	"time"
)

const isolationSidecar = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: isolate-ns
spec:
  egress:
  - hosts:
    - "./*"
    - "istio-system/*"
`

func TestPushTriggerIsolation(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.sidecar.isolation").
		Run(func(ctx framework.TestContext) {
			ns1 := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "isolation", Inject: true})
			ns2 := namespace.NewOrFail(ctx, ctx, namespace.Config{Prefix: "isolation", Inject: true})

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns1, "a")).
				With(&b, echoConfig(ns2, "b")).
				BuildOrFail(ctx)

			time.Sleep(2 * time.Second)
			baseline := cdsPushesOrFail(ctx)
			scopes.Framework.Infof("cds pushes before sidecar: %d", baseline)

			ctx.Config().ApplyYAMLOrFail(ctx, ns1.Name(), isolationSidecar)

			time.Sleep(3 * time.Second)
			after := cdsPushesOrFail(ctx)
			scopes.Framework.Infof("cds pushes after sidecar: %d", after)

			if after-baseline > 1 {
				ctx.Fatalf("expected no more than 2 cds pushes when creating sidecar, got: %d", after-baseline)
			}
		})
}

func cdsPushesOrFail(ctx framework.TestContext) int {
	stats := i.ControlPlaneStatsOrFail(ctx, ctx.Clusters()[0])
	pushes := stats["pilot_xds_pushes"]
	for _, m := range pushes.Metric {
		for _, l := range m.Label {
			if *l.Name == "type" && *l.Value == "cds" {
				return int(m.Counter.GetValue())
			}
		}
	}
	ctx.Fatalf("did not find cds stat in pilot_xds_pushes")
	return 0
}
