package bootstrap

import (
	"testing"
	meshconfig "istio.io/api/mesh/v1alpha1"

	"io/ioutil"
	"github.com/golang/protobuf/proto"
	"os"
)

func TestGolden(t *testing.T) {
	cases := []struct {
		base string
	}{
		// Original pilot tests only has those 2 configs
		{
			"default",
		},
		{
			"auth",
		},
		{
			// Specify zipkin/statsd address, similar with the default config in v1 tests
			"all",
		},
	}

	out := os.Getenv("OUT") // defined in the makefile
	if out == "" {
		out = "/tmp"
	}

	for _, c := range cases {
		t.Run("Bootrap-" + c.base, func(t *testing.T) {
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
			golden, err := ioutil.ReadFile("testdata/" + c.base + ".golden")
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

	// TODO: move to base dir
	cfg.ConfigPath = out + "/bootstraptest/" + base
	cfg.CustomConfigFile = "envoy_bootstrap_tmpl.yaml"
	return cfg, nil;
}
