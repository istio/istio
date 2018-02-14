// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
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
			fn, err := WriteBootstrap(cfg, 0, []string{
				"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"})
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
