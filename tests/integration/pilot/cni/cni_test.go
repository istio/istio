package cni

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/tests/integration/security/util/reachability"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("cni", m).
		SetupOnEnv(environment.Kube, istio.Setup(nil, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
components:
  cni:
     enabled: true
`
		})).
		Run()
}

// This test verifies reachability under different authN scenario:
// - app A to app B using mTLS.
// In each test, the steps are:
// - Configure authn policy.
// - Wait for config propagation.
// - Send HTTP/gRPC requests between apps.
func TestCNIReachability(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			g, err := galley.New(ctx, galley.Config{})
			if err != nil {
				ctx.Fatal(err)
			}
			p, err := pilot.New(ctx, pilot.Config{
				Galley: g,
			})
			if err != nil {
				ctx.Fatal(err)
			}
			rctx := reachability.CreateContext(ctx, g, p)
			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)

			testCases := []reachability.TestCase{
				{
					ConfigFile:          "global-mtls-on.yaml",
					Namespace:           systemNM,
					RequiredEnvironment: environment.Kube,
					Include: func(src echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to the headless TCP port.
						if opts.Target == rctx.Headless && opts.PortName == "tcp" {
							return false
						}
						return true
					},
					ExpectSuccess: func(src echo.Instance, opts echo.CallOptions) bool {
						if src == rctx.Naked && opts.Target == rctx.Naked {
							// naked->naked should always succeed.
							return true
						}

						// If one of the two endpoints is naked, expect failure.
						return src != rctx.Naked && opts.Target != rctx.Naked
					},
				},
			}
			rctx.Run(testCases)
		})
}
