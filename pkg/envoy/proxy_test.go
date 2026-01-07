// Copyright Istio Authors
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

package envoy

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/model"
)

func TestEnvoyArgs(t *testing.T) {
	proxyConfig := (*model.NodeMetaProxyConfig)(mesh.DefaultProxyConfig())
	proxyConfig.ClusterName = &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: "my-cluster"}
	proxyConfig.Concurrency = &wrapperspb.Int32Value{Value: 8}

	cases := []struct {
		name      string
		cfg       ProxyConfig
		extraArgs []string
		want      []string
	}{
		{
			name: "default",
			cfg: ProxyConfig{
				LogLevel:          "trace",
				ComponentLogLevel: "misc:error",
				NodeIPs:           []string{"10.75.2.9", "192.168.11.18"},
				BinaryPath:        proxyConfig.BinaryPath,
				ConfigPath:        proxyConfig.ConfigPath,
				ConfigCleanup:     true,
				AdminPort:         proxyConfig.ProxyAdminPort,
				DrainDuration:     proxyConfig.DrainDuration,
				Concurrency:       8,
			},
			extraArgs: []string{"-l", "trace", "--component-log-level", "misc:error"},
			want: []string{
				"-c", "test.json",
				"--drain-time-s", "45",
				"--drain-strategy", "immediate",
				"--local-address-ip-version", "v4",
				"--file-flush-interval-msec", "1000",
				"--disable-hot-restart",
				"--allow-unknown-static-fields",
				"-l", "trace",
				"--component-log-level", "misc:error",
				"--config-yaml", `{"key":"value"}`,
				"--concurrency", "8",
			},
		},
		{
			name: "deprecated-logs",
			cfg: ProxyConfig{
				LogLevel:           "trace",
				ComponentLogLevel:  "misc:error",
				NodeIPs:            []string{"10.75.2.9", "192.168.11.18"},
				BinaryPath:         proxyConfig.BinaryPath,
				ConfigPath:         proxyConfig.ConfigPath,
				ConfigCleanup:      true,
				AdminPort:          proxyConfig.ProxyAdminPort,
				DrainDuration:      proxyConfig.DrainDuration,
				Concurrency:        8,
				SkipDeprecatedLogs: true,
			},
			extraArgs: []string{"-l", "trace", "--component-log-level", "misc:error", "--skip-deprecated-logs"},
			want: []string{
				"-c", "test.json",
				"--drain-time-s", "45",
				"--drain-strategy", "immediate",
				"--local-address-ip-version", "v4",
				"--file-flush-interval-msec", "1000",
				"--disable-hot-restart",
				"--allow-unknown-static-fields",
				"-l", "trace",
				"--component-log-level", "misc:error",
				"--skip-deprecated-logs",
				"--config-yaml", `{"key":"value"}`,
				"--concurrency", "8",
			},
		},
		{
			name: "file-flush",
			cfg: ProxyConfig{
				LogLevel:           "trace",
				ComponentLogLevel:  "misc:error",
				NodeIPs:            []string{"10.75.2.9", "192.168.11.18"},
				BinaryPath:         proxyConfig.BinaryPath,
				ConfigPath:         proxyConfig.ConfigPath,
				ConfigCleanup:      true,
				AdminPort:          proxyConfig.ProxyAdminPort,
				DrainDuration:      proxyConfig.DrainDuration,
				Concurrency:        8,
				FileFlushMinSizeKB: 128,
				FileFlushInterval:  durationpb.New(time.Second * 10),
			},
			extraArgs: []string{"-l", "trace", "--component-log-level", "misc:error"},
			want: []string{
				"-c", "test.json",
				"--drain-time-s", "45",
				"--drain-strategy", "immediate",
				"--local-address-ip-version", "v4",
				"--file-flush-interval-msec", "10000",
				"--disable-hot-restart",
				"--allow-unknown-static-fields",
				"--file-flush-min-size-kb", "128",
				"-l", "trace",
				"--component-log-level", "misc:error",
				"--config-yaml", `{"key":"value"}`,
				"--concurrency", "8",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			test := &envoy{
				ProxyConfig: tc.cfg,
				extraArgs:   tc.extraArgs,
			}

			testProxy := NewProxy(tc.cfg)
			if !reflect.DeepEqual(testProxy, test) {
				t.Errorf("unexpected struct got\n%v\nwant\n%v", testProxy, test)
			}

			got := test.args("test.json", "testdata/bootstrap.json")
			if d := cmp.Diff(got, tc.want); d != "" {
				t.Errorf("envoyArgs() => (-want +got):\n%s", d)
			}
		})
	}
}

func TestReadToJSONIPv4(t *testing.T) {
	err := os.Setenv("HOST_IP", "169.254.169.254")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	got, err := readBootstrapToJSON("testdata/bootstrap.yaml")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	want := `{"ip":"[169.254.169.254]:8126","ip2":"169.254.169.254:8126","key":"value"}`
	if got != want {
		t.Errorf("readBootstrapToJSON() => got:\n%v,\nwant:\n%v", got, want)
	}
}

func TestReadToJSONIPv6(t *testing.T) {
	err := os.Setenv("HOST_IP", "dead::beef")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	got, err := readBootstrapToJSON("testdata/bootstrap.yaml")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	want := `{"ip":"[dead::beef]:8126","ip2":"[dead::beef]:8126","key":"value"}`
	if got != want {
		t.Errorf("readBootstrapToJSON() => got:\n%v,\nwant:\n%v", got, want)
	}
}

func TestSplitComponentLog(t *testing.T) {
	cases := []struct {
		input      string
		log        string
		components []string
	}{
		{"info", "info", nil},
		// A bit odd, but istio logging behaves this way so might as well be consistent
		{"info,warn", "warn", nil},
		{"info,misc:warn", "info", []string{"misc:warn"}},
		{"misc:warn,info", "info", []string{"misc:warn"}},
		{"misc:warn,info,upstream:debug", "info", []string{"misc:warn", "upstream:debug"}},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			l, c := splitComponentLog(tt.input)
			if l != tt.log {
				t.Errorf("expected log level %v, got %v", tt.log, l)
			}
			if !reflect.DeepEqual(c, tt.components) {
				t.Errorf("expected component log level %v, got %v", tt.components, c)
			}
		})
	}
}
