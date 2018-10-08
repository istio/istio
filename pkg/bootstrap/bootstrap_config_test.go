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
	"encoding/base64"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	diff "gopkg.in/d4l3k/messagediff.v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Generate configs for the default configs used by istio.
// If the template is updated, copy the new golden files from out:
// cp $TOP/out/linux_amd64/release/bootstrap/all/envoy-rev0.json pkg/bootstrap/testdata/all_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/auth/envoy-rev0.json pkg/bootstrap/testdata/auth_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/default/envoy-rev0.json pkg/bootstrap/testdata/default_golden.json
func TestGolden(t *testing.T) {
	cases := []struct {
		base        string
		labels      map[string]string
		annotations map[string]string
	}{
		{
			base: "auth",
		},
		{
			base: "default",
		},
		{
			base: "running",
			labels: map[string]string{
				"ISTIO_PROXY_SHA":     "istio-proxy:sha",
				"INTERCEPTION_MODE":   "REDIRECT",
				"ISTIO_PROXY_VERSION": "istio-proxy:version",
				"ISTIO_VERSION":       "release-3.1",
				"POD_NAME":            "svc-0-0-0-6944fb884d-4pgx8",
			},
			annotations: map[string]string{
				"istio.io/insecurepath": "{\"paths\":[\"/metrics\",\"/live\"]}",
			},
		},
		{
			// Specify zipkin/statsd address, similar with the default config in v1 tests
			base: "all",
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

			_, localEnv := createEnv(c.labels, c.annotations)
			fn, err := WriteBootstrap(cfg, "sidecar~1.2.3.4~foo~bar", 0, []string{
				"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"}, nil, localEnv)
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

			realM := v2.Bootstrap{}
			goldenM := v2.Bootstrap{}

			jgolden, err := yaml.YAMLToJSON(golden)

			if err != nil {
				t.Fatalf("unable to convert: %s %v", c.base, err)
			}

			if err = jsonpb.UnmarshalString(string(jgolden), &goldenM); err != nil {
				t.Fatalf("invalid json %s %s\n%v", c.base, err, string(jgolden))
			}

			if err = goldenM.Validate(); err != nil {
				t.Fatalf("invalid golder: %v", err)
			}

			jreal, err := yaml.YAMLToJSON(real)

			if err != nil {
				t.Fatalf("unable to convert: %v", err)
			}

			if err = jsonpb.UnmarshalString(string(jreal), &realM); err != nil {
				t.Fatalf("invalid json %v\n%s", err, string(real))
			}

			if !reflect.DeepEqual(realM, goldenM) {
				s, _ := diff.PrettyDiff(realM, goldenM)
				t.Logf("difference: %s", s)
				t.Fatalf("\n got: %v\nwant: %v", realM, goldenM)
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

type encodeFn func(string) string

func envEncode(m map[string]string, prefix string, encode encodeFn, out []string) []string {
	for k, v := range m {
		out = append(out, prefix+encode(k)+"="+encode(v))
	}
	return out
}

// createEnv takes labels and annotations are returns environment in go format.
func createEnv(labels map[string]string, anno map[string]string) (map[string]string, []string) {
	merged := map[string]string{}
	mergeMap(merged, labels)
	mergeMap(merged, anno)

	envs := make([]string, 0)
	envs = envEncode(labels, IstioMetaPrefix, func(s string) string {
		return s
	}, envs)
	envs = envEncode(anno, IstioMetaB64Prefix, func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	}, envs)

	return merged, envs
}

func TestNodeMetadata(t *testing.T) {
	labels := map[string]string{
		"l1":    "v1",
		"l2":    "v2",
		"istio": "sidecar",
	}
	anno := map[string]string{
		"istio.io/enable": "{\"abc\": 20}",
	}

	_, envs := createEnv(labels, nil)
	nm := getNodeMetaData(envs)

	if !reflect.DeepEqual(nm, labels) {
		t.Fatalf("Maps are not equal.\ngot: %v\nwant: %v", nm, labels)
	}

	merged, envs := createEnv(labels, anno)

	nm = getNodeMetaData(envs)
	if !reflect.DeepEqual(nm, merged) {
		t.Fatalf("Maps are not equal.\ngot: %v\nwant: %v", nm, merged)
	}

	t.Logf("envs => %v\nnm=> %v", envs, nm)

	// encode string incorrectly,
	// a warning is logged, but everything else works.
	envs = envEncode(anno, IstioMetaB64Prefix, func(s string) string {
		return s
	}, envs)

	nm = getNodeMetaData(envs)
	if !reflect.DeepEqual(nm, merged) {
		t.Fatalf("Maps are not equal.\ngot: %v\nwant: %v", nm, merged)
	}

}

func mergeMap(to map[string]string, from map[string]string) {
	for k, v := range from {
		to[k] = v
	}
}
