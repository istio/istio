package echotest

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

type (
	perDeploymentTest func(ctx framework.TestContext, instances echo.Instances)
	perInstanceTest   func(ctx framework.TestContext, src echo.Instance, dst echo.Instances)
)

// Run will generate nested subtests for every instance in every deployment. The subtests will be nested including
// the source service, source cluster and destination deployment. Example: `a/to_b/from_cluster-0`
func (t *T) Run(testFn perInstanceTest) {
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, srcInstances echo.Instances) {
		t.setup(ctx, srcInstances)
		t.toEachDeployment(ctx, func(ctx framework.TestContext, dstInstances echo.Instances) {
			t.setupPair(ctx, srcInstances, dstInstances)
			fromEachCluster(ctx, srcInstances, dstInstances, testFn)
		})
	})
}

// fromEachDeployment enumerates subtests for deployment with the structure <src>
// Intended to be used in combination with other helpers to enumerate subtests for destinations.
func (t *T) fromEachDeployment(ctx framework.TestContext, testFn perDeploymentTest) {
	for srcDeployment, srcInstances := range t.sources.Deployments() {
		srcDeployment, srcInstances := srcDeployment, srcInstances
		ctx.NewSubTestf("%s", srcDeployment.Service).Run(func(ctx framework.TestContext) {
			if len(srcInstances) == 0 {
				// this can only happen due to filters being applied, be noisy about it to highlight missing coverage
				ctx.Skip("cases with %s as source are removed by filters", srcDeployment.Service)
			}
			testFn(ctx, srcInstances)
		})
	}
}

// toEachDeployment enumerates subtests for every deployment as a destination, adding /to_<dst> to the parent test.
// Intended to be used in combination with other helpers which enumerates the subtests and chooses the srcInstnace.
func (t *T) toEachDeployment(ctx framework.TestContext, testFn perDeploymentTest) {
	for dstDeployment, destInstances := range t.destinations.Deployments() {
		dstDeployment, destInstances := dstDeployment, destInstances
		ctx.NewSubTestf("to %s", dstDeployment.Service).Run(func(ctx framework.TestContext) {
			if len(destInstances) == 0 {
				// this can only happen due to filters being applied, be noisy about it to highlight missing coverage
				ctx.Skip("cases with %s as destination are removed by filters", dstDeployment.Service)
			}
			testFn(ctx, destInstances)
		})
	}
}

func fromEachCluster(ctx framework.TestContext, src, dst echo.Instances, testFn perInstanceTest) {
	for _, srcInstance := range src {
		srcInstance := srcInstance
		ctx.NewSubTestf("in %s", srcInstance.Config().Cluster.StableName()).Run(func(ctx framework.TestContext) {
			testFn(ctx, srcInstance, dst)
		})
	}
}
