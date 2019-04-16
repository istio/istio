// Package meshexp contains test suite for mesh expansion.
package meshexp

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("meshexp_test", m).
		// Restrict the test to the K8s environment only, tests will be skipped in native environment.
		RequireEnvironment(environment.Kube).
		// Deploy Istio on the cluster
		Setup(istio.SetupOnKube(nil, setupMeshExpansionInstall)).
		// Create a VM instance before running the test.
		Setup(provisionVMInstance).
		Run()
}

func setupMeshExpansionInstall(cfg *istio.Config) {
	cfg.Values["global.meshExpansion.enabled"] = "true"
}

func provisionVMInstance(ctx framework.SuiteContext) error {
	return nil
}

func TestKubernetesToVM(t *testing.T) {
}

func TestVMToKuberentes(t *testing.T) {

}
