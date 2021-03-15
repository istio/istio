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

	destinationFilters []destinationFilter

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
