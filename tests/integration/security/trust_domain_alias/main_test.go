package trustdomainalias

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util/cert"
)

var (
	inst istio.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig, cert.CreateCASecret)).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  meshConfig:
    trustDomainAliases: [trust-domain-foo]
`
}