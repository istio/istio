package envoy

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/pilot/model"
	"istio.io/pilot/test/util"
)

const (
	ingressEnvoyConfig = "testdata/envoy-ingress.json"
	ingressRouteRule1  = "testdata/ingress-route-world.yaml.golden"
	ingressRouteRule2  = "testdata/ingress-route-foo.yaml.golden"
	ingressCertFile    = "testdata/tls.crt"
	ingressKeyFile     = "testdata/tls.key"
)

var (
	ingressCert      = []byte("abcdefghijklmnop")
	ingressKey       = []byte("qrstuvwxyz123456")
	ingressTLSSecret = &model.TLSSecret{Certificate: ingressCert, PrivateKey: ingressKey}
)

func addIngressRoutes(r model.ConfigStore, t *testing.T) {
	for _, file := range []string{ingressRouteRule1, ingressRouteRule2} {
		msg, err := configObjectFromYAML(model.IngressRule, file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Post(msg); err != nil {
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

func TestRouteCombination(t *testing.T) {
	path1 := &HTTPRoute{Path: "/xyz"}
	path2 := &HTTPRoute{Path: "/xy"}
	path3 := &HTTPRoute{Path: "/z"}
	prefix1 := &HTTPRoute{Prefix: "/xyz"}
	prefix2 := &HTTPRoute{Prefix: "/x"}
	prefix3 := &HTTPRoute{Prefix: "/z"}

	testCases := []struct {
		a    *HTTPRoute
		b    *HTTPRoute
		want *HTTPRoute
	}{
		{path1, path1, path1},
		{prefix1, prefix1, prefix1},
		{path1, path2, nil},
		{path1, path3, nil},
		{prefix1, prefix2, prefix1},
		{prefix1, prefix3, nil},
		{prefix2, prefix3, nil},
		{path1, prefix1, path1},
		{path1, prefix2, path1},
		{path1, prefix3, nil},
		{path2, prefix1, nil},
		{path2, prefix2, path2},
		{path2, prefix3, nil},
		{path3, prefix3, path3},
		{path3, prefix1, nil},
		{path3, prefix2, nil},
		{path3, prefix3, path3},
	}

	for _, test := range testCases {
		a := *test.a
		got := a.CombinePathPrefix(test.b.Path, test.b.Prefix)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s.CombinePathPrefix(%s) => got %v, want %v", spew.Sdump(test.a), spew.Sdump(test.b), got, test.want)
		}
		b := *test.b
		got = b.CombinePathPrefix(test.a.Path, test.a.Prefix)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s.CombinePathPrefix(%s) => got %v, want %v", spew.Sdump(test.b), spew.Sdump(test.a), got, test.want)
		}
	}
}
