package envoy

import (
	"testing"

	"fmt"
	"io/ioutil"
	"reflect"

	"github.com/pmezard/go-difflib/difflib"
	"istio.io/manager/model"
	"istio.io/manager/test/mock"
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

func compareFile(filename string, expect []byte, t *testing.T) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Error loading %s: %s", filename, err.Error())
	}
	if !reflect.DeepEqual(data, expect) {
		diff := difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(expect)),
			B:        difflib.SplitLines(string(data)),
			FromFile: filename,
			ToFile:   "",
			Context:  2,
		}
		text, _ := difflib.GetUnifiedDiffString(diff)
		fmt.Println(text)
		t.Fatalf("Failed validating file %s", filename)
	}
}

func testIngressConfig(c *IngressConfig, envoyConfig string, t *testing.T) {
	config := generateIngress(c)
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
	testIngressConfig(&IngressConfig{
		Registry: r,
		Mesh:     DefaultMeshConfig,
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
		Mesh:      DefaultMeshConfig,
	}, ingressEnvoySSLConfig, t)
	compareFile(ingressCertFile, ingressCert, t)
	compareFile(ingressKeyFile, ingressKey, t)
}
