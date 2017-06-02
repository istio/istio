package envoy

import (
	"testing"

	proxyconfig "istio.io/api/proxy/v1/config"

	"istio.io/pilot/test/util"
)

const (
	egressEnvoyConfig    = "testdata/envoy-egress.json"
	egressEnvoySSLConfig = "testdata/envoy-egress-auth.json"
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
