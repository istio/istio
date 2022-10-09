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
	"reflect"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
)

func TestEnvoyArgs(t *testing.T) {
	proxyConfig := (*model.NodeMetaProxyConfig)(mesh.DefaultProxyConfig())
	proxyConfig.ClusterName = &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: "my-cluster"}
	proxyConfig.Concurrency = &wrapperspb.Int32Value{Value: 8}

	cfg := ProxyConfig{
		LogLevel:               "trace",
		ComponentLogLevel:      "misc:error",
		NodeIPs:                []string{"10.75.2.9", "192.168.11.18"},
		BinaryPath:             proxyConfig.BinaryPath,
		ConfigPath:             proxyConfig.ConfigPath,
		ConfigCleanup:          true,
		AdminPort:              proxyConfig.ProxyAdminPort,
		DrainDuration:          proxyConfig.DrainDuration,
		ParentShutdownDuration: proxyConfig.ParentShutdownDuration,
		Concurrency:            8,
	}

	test := &envoy{
		ProxyConfig: cfg,
		extraArgs:   []string{"-l", "trace", "--component-log-level", "misc:error"},
	}

	testProxy := NewProxy(cfg)
	if !reflect.DeepEqual(testProxy, test) {
		t.Errorf("unexpected struct got\n%v\nwant\n%v", testProxy, test)
	}

	got := test.args("test.json", "testdata/bootstrap.json")
	want := []string{
		"-c", "test.json",
		"--drain-time-s", "45",
		"--drain-strategy", "immediate",
		"--parent-shutdown-time-s", "60",
		"--local-address-ip-version", "v4",
		"--file-flush-interval-msec", "1000",
		"--disable-hot-restart",
		"--log-format", "%Y-%m-%dT%T.%fZ\t%l\tenvoy %n\t%v",
		"-l", "trace",
		"--component-log-level", "misc:error",
		"--config-yaml", `{"key": "value"}`,
		"--concurrency", "8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("envoyArgs() => got:\n%v,\nwant:\n%v", got, want)
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
