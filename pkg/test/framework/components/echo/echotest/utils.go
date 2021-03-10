package echotest

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

// T ... TODO document me
type T struct {
	// rootCtx is the top level test context to generate subtests from and should only be referenced from RunX methods.
	rootCtx      framework.TestContext
	sources      echo.Instances
	destinations echo.Instances
}

// New ... TODO document me
func New(ctx framework.TestContext, instances echo.Instances) *T {
	s, d := make(echo.Instances, len(instances)), make(echo.Instances, len(instances))
	copy(s, instances)
	copy(d, instances)
	return &T{rootCtx: ctx, sources: s, destinations: d}
}

type perInstanceTest func(ctx framework.TestContext, src echo.Instance, dest echo.Instances)

// Run will generate nested subtests for every instance in every deployment. The subtests will be nested including
// the source service, source cluster and destination deployment. Example: `from_a/in_cluster-0/to_b`
func (i *T) Run(testFn perInstanceTest) {
	i.fromEach(i.rootCtx, func(ctx framework.TestContext, srcInstance echo.Instance) {
		i.toEach(ctx, srcInstance, testFn)
	})
}

// fromEach enumerates subtests for every instance in every deployment with the structure from_<src>/in_<cluster>.
// Intended to be used in combination with other helpers to enumerate subtests for destinations.
func (i *T) fromEach(ctx framework.TestContext, testFn func(ctx framework.TestContext, src echo.Instance)) {
	for srcDeployment, srcInstances := range i.sources.Deployments() {
		srcDeployment, srcInstances := srcDeployment, srcInstances
		ctx.NewSubTestf("from %s", srcDeployment.Service).Run(func(ctx framework.TestContext) {
			if len(srcInstances) == 0 {
				// this can only happen due to filters being applied, be noisy about it to highlight missing coverage
				ctx.Skip("cases with %s as source are removed by filters", srcDeployment.Service)
			}
			for _, srcInstance := range srcInstances {
				srcInstance := srcInstance
				ctx.NewSubTestf("in %s", srcInstance.Config().Cluster.StableName()).Run(func(ctx framework.TestContext) {
					testFn(ctx, srcInstance)
				})
			}
		})
	}
}

// toEach enumerates subtests for every deployment as a destination, adding /to_<dst> to the parent test.
// Intended to be used in combination with other helpers which enumerates the subtests and chooses the srcInstnace.
func (i *T) toEach(ctx framework.TestContext, srcInstance echo.Instance, testFn perInstanceTest) {
	for dstDeployment, destInstances := range i.destinations.Deployments() {
		dstDeployment, destInstances := dstDeployment, destInstances
		ctx.NewSubTestf("to %s", dstDeployment.Service).Run(func(ctx framework.TestContext) {
			if len(destInstances) == 0 {
				// this can only happen due to filters being applied, be noisy about it to highlight missing coverage
				ctx.Skip("cases with %s as destination are removed by filters", dstDeployment.Service)
			}
			testFn(ctx, srcInstance, destInstances)
		})
	}
}
