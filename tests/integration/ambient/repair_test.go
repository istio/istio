//go:build integ
// +build integ

package ambient

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

var i istio.Instance

// TestTrafficWithCNIRepair validates TestTraffic with CNI repair enabled
func TestTrafficWithCNIRepair(t *testing.T) {
	// Create a new test suite specifically for testing with CNI repair enabled
	suite := framework.NewTest(t)

	// Set up the suite to enable CNI repair
	suite.
		RequireMinVersion(24).
		Setup(func(ctx resource.Context) error {
			ctx.Settings().Ambient = true
			ctx.Settings().CNI.Repair = true // Enable CNI repair
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			// Configure Istio with CNI repair enabled
			cfg.EnableCNI = true
			cfg.CNI.Repair = true // Enable CNI repair in Istio
			cfg.DeployEastWestGW = false
		})).
		Run(func(t framework.TestContext) {
			// Call the TestTraffic function from traffic_test.go
			traffic_test.TestTraffic(t)
		})
}
