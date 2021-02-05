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
			server := apps.PodA.GetOrFail(ctx, echo.InCluster(ctx.Clusters().Default()))
			client := apps.VM.GetOrFail(ctx, echo.InCluster(ctx.Clusters().Default()))
			resps, err := client.Call(echo.CallOptions{
				Target:   server,
				PortName: "http",
			})
			if err != nil {
				fmt.Printf("echo call failed with: %v\n", err)
				ctx.Fatal()
			}
			fmt.Printf("SAM: got back %d responses: %v\n", len(resps), resps)
		})
}
