package envoy

import (
	"testing"

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
	"istio.io/manager/test/util"
)

const (
	ingressEnvoyConfig    = "testdata/ingress-envoy.json"
	ingressEnvoySSLConfig = "testdata/ingress-envoy-ssl.json"
	ingressRouteRule      = "testdata/ingress-route.yaml.golden"
	ingressCertFile       = "testdata/tls.crt"
	ingressKeyFile        = "testdata/tls.key"
	ingressSecret         = "secret"
)

var (
	ingressCert = []byte("abcdefghijklmnop")
	ingressKey  = []byte("qrstuvwxyz123456")
)

func testIngressConfig(c *IngressConfig, envoyConfig string, t *testing.T) {
	config := generateIngress(c)
	if config == nil {
		t.Fatal("Failed to generate config")
	}

	if err := config.WriteFile(envoyConfig); err != nil {
		t.Fatalf(err.Error())
	}

	util.CompareYAML(envoyConfig, t)
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
	testIngressConfig(&IngressConfig{
		Registry: r,
		Mesh:     &DefaultMeshConfig,
	}, ingressEnvoyConfig, t)
}

func TestIngressRoutesSSL(t *testing.T) {
	r := mock.MakeRegistry()
	s := mock.SecretRegistry{
		Secrets: map[string]map[string][]byte{
			ingressSecret + ".default": {
				"tls.crt": ingressCert,
				"tls.key": ingressKey,
			},
		},
	}
	addIngressRoute(r, t)
	testIngressConfig(&IngressConfig{
		CertFile:  ingressCertFile,
		KeyFile:   ingressKeyFile,
		Namespace: "",
		Secret:    ingressSecret,
		Secrets:   &s,
		Registry:  r,
		Mesh:      &DefaultMeshConfig,
	}, ingressEnvoySSLConfig, t)
	util.CompareFile(ingressCertFile, ingressCert, t)
	util.CompareFile(ingressKeyFile, ingressKey, t)
}
