package envoy

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"istio.io/manager/model"
	"istio.io/manager/test/util"
)

const (
	ingressEnvoyConfig = "testdata/envoy-ingress.json"
	ingressRouteRule1  = "testdata/ingress-route-world.yaml.golden"
	ingressRouteRule2  = "testdata/ingress-route-foo.yaml.golden"
	ingressCertFile    = "testdata/tls.crt"
	ingressKeyFile     = "testdata/tls.key"
	ingressNamespace   = "default"
)

var (
	ingressCert      = []byte("abcdefghijklmnop")
	ingressKey       = []byte("qrstuvwxyz123456")
	ingressTLSSecret = &model.TLSSecret{Certificate: ingressCert, PrivateKey: ingressKey}
)

func addIngressRoutes(r *model.IstioRegistry, t *testing.T) {
	for i, file := range []string{ingressRouteRule1, ingressRouteRule2} {
		msg, err := configObjectFromYAML(model.IngressRule, file)
		if err != nil {
			t.Fatal(err)
		}
		key := model.Key{Kind: model.IngressRule, Name: fmt.Sprintf("route-%d", i), Namespace: ingressNamespace}
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

func TestIngressRoutesSSL(t *testing.T) {
	mesh := makeMeshConfig()
	config := generateIngress(&mesh, ingressTLSSecret, ingressCertFile, ingressKeyFile)
	if config == nil {
		t.Fatal("Failed to generate config")
	}

	if err := config.WriteFile(ingressEnvoyConfig); err != nil {
		t.Fatal(err)
	}

	util.CompareYAML(ingressEnvoyConfig, t)
	compareFile(ingressCertFile, ingressCert, t)
	compareFile(ingressKeyFile, ingressKey, t)
}
