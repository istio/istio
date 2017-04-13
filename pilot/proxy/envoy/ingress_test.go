package envoy

import (
	"io/ioutil"
	"os"
	"testing"

	"fmt"

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
	"istio.io/manager/test/util"
)

const (
	ingressEnvoyConfig           = "testdata/ingress-envoy.json"
	ingressEnvoySSLConfig        = "testdata/ingress-envoy-ssl.json"
	ingressEnvoyPartialSSLConfig = "testdata/ingress-envoy-partial-ssl.json"
	ingressRouteRule1            = "testdata/ingress-route-world.yaml.golden"
	ingressRouteRule2            = "testdata/ingress-route-foo.yaml.golden"
	ingressCertFile              = "testdata/tls.crt"
	ingressKeyFile               = "testdata/tls.key"
	ingressNamespace             = "default"
)

var (
	ingressCert      = []byte("abcdefghijklmnop")
	ingressKey       = []byte("qrstuvwxyz123456")
	ingressTLSSecret = &model.TLSSecret{Certificate: ingressCert, PrivateKey: ingressKey}
)

func testIngressConfig(c *IngressConfig, envoyConfig string, t *testing.T) {
	config := generateIngress(c)
	if config == nil {
		t.Fatal("Failed to generate config")
	}

	if err := config.WriteFile(envoyConfig); err != nil {
		t.Fatal(err)
	}

	util.CompareYAML(envoyConfig, t)
}

func addIngressRoutes(r *model.IstioRegistry, t *testing.T) {
	for i, file := range []string{ingressRouteRule1, ingressRouteRule2} {
		msg, err := configObjectFromYAML(model.IngressRule, file)
		if err != nil {
			t.Fatal(err)
		}
		key := model.Key{Kind: model.IngressRule, Name: fmt.Sprintf("route_%d", i), Namespace: ingressNamespace}
		if err = r.Post(key, msg); err != nil {
			t.Fatal(err)
		}
	}
}

func compareFile(filename string, golden []byte, t *testing.T) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Error loading %s: %s", filename, err.Error())
	}
	if string(content) != string(golden) {
		t.Errorf("Failed validating file %s, got %s", filename, string(content))
	}
	err = os.Remove(filename)
	if err != nil {
		t.Errorf("Failed cleaning up temporary file %s", filename)
	}
}

func TestIngressRoutes(t *testing.T) {
	r := mock.MakeRegistry()
	s := &mock.SecretRegistry{}
	addIngressRoutes(r, t)
	testIngressConfig(&IngressConfig{
		Registry:  r,
		Namespace: ingressNamespace,
		Secrets:   s,
		Mesh:      &DefaultMeshConfig,
	}, ingressEnvoyConfig, t)
}

func TestIngressRoutesSSL(t *testing.T) {
	r := mock.MakeRegistry()
	s := &mock.SecretRegistry{"*": ingressTLSSecret}
	addIngressRoutes(r, t)
	testIngressConfig(&IngressConfig{
		CertFile:  ingressCertFile,
		KeyFile:   ingressKeyFile,
		Namespace: ingressNamespace,
		Secrets:   s,
		Registry:  r,
		Mesh:      &DefaultMeshConfig,
	}, ingressEnvoySSLConfig, t)
	compareFile(ingressCertFile, ingressCert, t)
	compareFile(ingressKeyFile, ingressKey, t)
}

func TestIngressRoutesPartialSSL(t *testing.T) {
	r := mock.MakeRegistry()
	s := &mock.SecretRegistry{fmt.Sprintf("world.%v.svc.cluster.local", ingressNamespace): ingressTLSSecret}
	addIngressRoutes(r, t)
	testIngressConfig(&IngressConfig{
		CertFile:  ingressCertFile,
		KeyFile:   ingressKeyFile,
		Namespace: ingressNamespace,
		Secrets:   s,
		Registry:  r,
		Mesh:      &DefaultMeshConfig,
	}, ingressEnvoyPartialSSLConfig, t)
	compareFile(ingressCertFile, ingressCert, t)
	compareFile(ingressKeyFile, ingressKey, t)
}
