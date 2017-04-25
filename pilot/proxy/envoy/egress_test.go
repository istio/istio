package envoy

import (
	"testing"

	proxyconfig "istio.io/api/proxy/v1/config"

	"istio.io/manager/test/util"
)

const (
	egressEnvoyConfig    = "testdata/egress-envoy.json"
	egressEnvoySSLConfig = "testdata/egress-envoy-auth.json"
)

func TestEgress(t *testing.T) {
	mesh := makeMeshConfig()
	config := generateEgress(&mesh)
	if config == nil {
		t.Fatal("Failed to generate config")
	}
	if err := config.WriteFile(egressEnvoyConfig); err != nil {
		t.Fatal(err)
	}
	util.CompareYAML(egressEnvoyConfig, t)
}

func TestEgressSSL(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	config := generateEgress(&mesh)
	if config == nil {
		t.Fatal("Failed to generate config")
	}
	if err := config.WriteFile(egressEnvoySSLConfig); err != nil {
		t.Fatal(err)
	}
	util.CompareYAML(egressEnvoySSLConfig, t)
}
