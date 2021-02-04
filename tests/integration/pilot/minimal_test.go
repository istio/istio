package pilot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

func TestMinimal(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("vm.autoregistration").
		Run(func(ctx framework.TestContext) {
			client := apps.PodA.GetOrFail(ctx, echo.InCluster(ctx.Clusters().Default()))
			server := apps.PodB.GetOrFail(ctx, echo.InCluster(ctx.Clusters().Default()))
			resps, err := client.Call(echo.CallOptions{
				Target:   server,
				PortName: "http",
			})
			if err != nil {
				fmt.Println("" +
					"")
			}
		})
}
