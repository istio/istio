package echotest

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

// T ... TODO document me
type T struct {
	// rootCtx is the top level test context to generate subtests from and should only be referenced from RunX methods.
	rootCtx framework.TestContext

	sources      echo.Instances
	destinations echo.Instances

	sourceDeploymentSetup []func(ctx framework.TestContext, src echo.Instances) error
	deploymentPairSetup   []func(ctx framework.TestContext, src, dst echo.Instances) error
}

// New ... TODO document me
func New(ctx framework.TestContext, instances echo.Instances) *T {
	s, d := make(echo.Instances, len(instances)), make(echo.Instances, len(instances))
	copy(s, instances)
	copy(d, instances)
	return &T{rootCtx: ctx, sources: s, destinations: d}
}

type (
	perDeploymentTest func(ctx framework.TestContext, instances echo.Instances)
	perInstanceTest   func(ctx framework.TestContext, src echo.Instance, dst echo.Instances)
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
