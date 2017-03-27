package envoy

import (
	"testing"

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
)

const (
	ingressEnvoyV0Config = "testdata/ingress-envoy-v0.json"
	ingressRouteRule     = "testdata/ingress-route.yaml.golden"
)

func testIngressConfig(r *model.IstioRegistry, envoyConfig string, t *testing.T) {
	config := generateIngress("", "", nil, r, DefaultMeshConfig)
	if config == nil {
		t.Fatal("Failed to generate config")
	}

	if err := config.WriteFile(envoyConfig); err != nil {
		t.Fatalf(err.Error())
	}

	compareJSON(envoyConfig, t)
}

func addIngressRoute(r *model.IstioRegistry, t *testing.T) {
	msg, err := configObjectFromYAML(model.IngressRule, ingressRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Post(model.Key{Kind: model.IngressRule, Name: "route"}, msg); err != nil {
		t.Fatal(err)
	}
}

func TestIngressRoutes(t *testing.T) {
	r := mock.MakeRegistry()
	addIngressRoute(r, t)
	testIngressConfig(r, ingressEnvoyV0Config, t)
}
