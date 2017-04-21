package envoy

import (
	"testing"

	"istio.io/manager/proxy"
	"istio.io/manager/test/mock"
	"istio.io/manager/test/util"
)

const (
	egressEnvoyConfig = "testdata/egress-envoy.json"
)

func testEgressConfig(c *EgressConfig, envoyConfig string, t *testing.T) {
	config := generateEgress(c)
	if config == nil {
		t.Fatal("Failed to generate config")
	}

	if err := config.WriteFile(envoyConfig); err != nil {
		t.Fatalf(err.Error())
	}

	util.CompareYAML(envoyConfig, t)
}

func TestEgressRoutes(t *testing.T) {
	r := mock.Discovery
	mesh := proxy.DefaultMeshConfig()
	testEgressConfig(&EgressConfig{
		Services: r,
		Mesh:     &mesh,
		Port:     8888,
	}, egressEnvoyConfig, t)
}
