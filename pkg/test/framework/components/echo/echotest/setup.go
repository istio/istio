package echotest

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

// Setup runs the given function in the source deployment context. For example, given apps a, b, and c in 2 clusters,
// these tests would all run before the the context is cleaned up:
//     a/to_b/from_cluster-1
//     a/to_b/from_cluster-2
//     a/to_c/from_cluster-1
//     a/to_c/from_cluster-2
//     cleanup...
//     b/to_a/from_cluster-1
//     ...
func (t *T) Setup(setupFn func(ctx framework.TestContext, src echo.Instances) error) *T {
	t.sourceDeploymentSetup = append(t.sourceDeploymentSetup, setupFn)
	return t
}

func (t *T) setup(ctx framework.TestContext, srcInstances echo.Instances) {
	for _, setupFn := range t.sourceDeploymentSetup {
		if err := setupFn(ctx, srcInstances); err != nil {
			ctx.Fatal(err)
		}
	}
}

// SetupForPair runs the given function in the source + destination deployment context. For example, given apps a, b,
// and c in 2 clusters, these tests would all run before the the context is cleaned up:
//     a/to_b/from_cluster-1
//     a/to_b/from_cluster-2
//     cleanup...
//     a/to_b/from_cluster-2
//     ...
func (t *T) SetupForPair(setupFn func(ctx framework.TestContext, src echo.Instances, dst echo.Instances) error) *T {
	t.deploymentPairSetup = append(t.deploymentPairSetup, setupFn)
	return t
}

func (t *T) setupPair(ctx framework.TestContext, src echo.Instances, dst echo.Instances) {
	for _, setupFn := range t.deploymentPairSetup {
		if err := setupFn(ctx, src, dst); err != nil {
			ctx.Fatal(err)
		}
	}
}
