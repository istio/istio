package bootstrap

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/golang/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestGolden(t *testing.T) {
	cases := []struct {
		base string
	}{
		{
			"auth",
		},
		{
			"default",
		},
		{
			// Specify zipkin/statsd address, similar with the default config in v1 tests
			"all",
		},
	}

	out := os.Getenv("ISTIO_OUT") // defined in the makefile
	if out == "" {
		out = "/tmp"
	}

	for _, c := range cases {
		t.Run("Bootrap-"+c.base, func(t *testing.T) {
			cfg, err := loadProxyConfig(c.base, out, t)
			if err != nil {
				t.Fatal(err)
			}
			fn, err := WriteBootstrap(cfg, 0)
			if err != nil {
				t.Fatal(err)
			}
			real, err := ioutil.ReadFile(fn)
			if err != nil {
				t.Error("Error reading generated file ", err)
				return
			}
			golden, err := ioutil.ReadFile("testdata/" + c.base + "_golden.json")
			if err != nil {
				golden = []byte{}
			}
			if string(real) != string(golden) {
				t.Error("Generated incorrect config, want:\n" + string(golden) + "\ngot:\n" + string(real))
			}
		})
	}

}

func loadProxyConfig(base, out string, t *testing.T) (*meshconfig.ProxyConfig, error) {
	content, err := ioutil.ReadFile("testdata/" + base + ".proto")
	if err != nil {
		return nil, err
	}
	cfg := &meshconfig.ProxyConfig{}
	err = proto.UnmarshalText(string(content), cfg)
	if err != nil {
		return nil, err
	}

	// Exported from makefile or env
	cfg.ConfigPath = out + "/bootstrap/" + base
	gobase := os.Getenv("ISTIO_GO")
	if gobase == "" {
		gobase = "../.."
	}
	cfg.CustomConfigFile = gobase + "/tools/deb/envoy_bootstrap_tmpl.json"
	return cfg, nil
}
