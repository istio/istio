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

// Generate configs for the default configs used by istio.
// If the template is updated, copy the new golden files from out:
// cp $TOP/out/linux_amd64/release/bootstrap/all/envoy-rev0.json pkg/bootstrap/testdata/all_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/auth/envoy-rev0.json pkg/bootstrap/testdata/auth_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/default/envoy-rev0.json pkg/bootstrap/testdata/default_golden.json
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
			fn, err := WriteBootstrap(cfg, "sidecar~1.2.3.4~foo~bar", 0, []string{
				"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"}, nil)
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
	cfg.CustomConfigFile = gobase + "/tools/deb/envoy_bootstrap_v2.json"
	return cfg, nil
}

func TestGetHostPort(t *testing.T) {
	var testCases = []struct {
		name         string
		addr         string
		expectedHost string
		expectedPort string
		errStr       string
	}{
		{
			name:         "Valid IPv4 host/port",
			addr:         "127.0.0.1:5000",
			expectedHost: "127.0.0.1",
			expectedPort: "5000",
			errStr:       "",
		},
		{
			name:         "Valid IPv6 host/port",
			addr:         "[2001:db8::100]:5000",
			expectedHost: "2001:db8::100",
			expectedPort: "5000",
			errStr:       "",
		},
		{
			name:         "Valid host/port",
			addr:         "istio-pilot:15005",
			expectedHost: "istio-pilot",
			expectedPort: "15005",
			errStr:       "",
		},
		{
			name:         "No port specified",
			addr:         "127.0.0.1:",
			expectedHost: "127.0.0.1",
			expectedPort: "",
			errStr:       "",
		},
		{
			name:         "Missing port",
			addr:         "127.0.0.1",
			expectedHost: "",
			expectedPort: "",
			errStr:       "unable to parse test address \"127.0.0.1\": address 127.0.0.1: missing port in address",
		},
		{
			name:         "Missing brackets for IPv6",
			addr:         "2001:db8::100:5000",
			expectedHost: "",
			expectedPort: "",
			errStr:       "unable to parse test address \"2001:db8::100:5000\": address 2001:db8::100:5000: too many colons in address",
		},
		{
			name:         "No address provided",
			addr:         "",
			expectedHost: "",
			expectedPort: "",
			errStr:       "unable to parse test address \"\": missing port in address",
		},
	}
	for _, tc := range testCases {
		h, p, err := GetHostPort("test", tc.addr)
		if err == nil {
			if tc.errStr != "" {
				t.Errorf("[%s] expected error %q, but no error seen", tc.name, tc.errStr)
			} else if h != tc.expectedHost || p != tc.expectedPort {
				t.Errorf("[%s] expected %s:%s, got %s:%s", tc.name, tc.expectedHost, tc.expectedPort, h, p)
			}
		} else {
			if tc.errStr == "" {
				t.Errorf("[%s] expected no error but got %q", tc.name, err.Error())
			} else if err.Error() != tc.errStr {
				t.Errorf("[%s] expected error message %q, got %v", tc.name, tc.errStr, err)
			}
		}
	}
}

func TestStoreHostPort(t *testing.T) {
	opts := map[string]interface{}{}
	StoreHostPort("istio-pilot", "15005", "foo", opts)
	actual, ok := opts["foo"]
	if !ok {
		t.Fatalf("expected to have map entry foo populated")
	}
	expected := "{\"address\": \"istio-pilot\", \"port_value\": 15005}"
	if actual != expected {
		t.Errorf("expected value %q, got %q", expected, actual)
	}
}
