package istioctl

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/label"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("nop-test", m).
		Label(label.CustomSetup).
		Run()
}

// Legacy CI targets try to call this folder and fail if there are no tests. Until that is cleaned up,
// Have a nop test.
func TestNothing(t *testing.T) {
	// You can specify additional constraints using the more verbose form
	framework.NewTest(t).
		Label(label.Postsubmit).
		RequiresEnvironment(environment.Native).
		Run(func(ctx framework.TestContext) {})
}
