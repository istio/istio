package revision_cmd

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
revision: stable
`
		})).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
profile: empty
revision: canary
components:
  pilot:
    enabled: true
`
		})).
		Run()
}
