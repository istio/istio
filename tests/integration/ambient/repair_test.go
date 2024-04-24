//go:build integ
// +build integ

package ambient

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/ambient/traffic_test"
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireMinVersion(24).
		Setup(func(ctx resource.Context) error {
			ctx.Settings().Ambient = true
			ctx.Settings().CNI.Repair = true
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.EnableCNI = true
			cfg.CNI.Repair = true
			cfg.DeployEastWestGW = false
		})).
		Run()
}

func TestTraffic(t *testing.T) {
	traffic_test.TestTraffic(t)
}
