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

package model

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasmextensions "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/test/util/assert"
)

func TestBuildDataSource(t *testing.T) {
	cases := []struct {
		url        string
		wasmPlugin *extensions.WasmPlugin

		expected *core.AsyncDataSource
	}{
		{
			url: "file://fake.wasm",
			wasmPlugin: &extensions.WasmPlugin{
				Url: "file://fake.wasm",
			},
			expected: &core.AsyncDataSource{
				Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: "fake.wasm",
						},
					},
				},
			},
		},
		{
			url: "oci://ghcr.io/istio/fake-wasm:latest",
			wasmPlugin: &extensions.WasmPlugin{
				Sha256: "fake-sha256",
			},
			expected: &core.AsyncDataSource{
				Specifier: &core.AsyncDataSource_Remote{
					Remote: &core.RemoteDataSource{
						HttpUri: &core.HttpUri{
							Uri:     "oci://ghcr.io/istio/fake-wasm:latest",
							Timeout: durationpb.New(30 * time.Second),
							HttpUpstreamType: &core.HttpUri_Cluster{
								Cluster: "_",
							},
						},
						Sha256: "fake-sha256",
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			u, err := url.Parse(tc.url)
			assert.NoError(t, err)
			got := buildDataSource(u, tc.wasmPlugin)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestBuildVMConfig(t *testing.T) {
	cases := []struct {
		desc     string
		vm       *extensions.VmConfig
		policy   extensions.PullPolicy
		expected *wasmextensions.PluginConfig_VmConfig
	}{
		{
			desc:   "Build VMConfig without a base VMConfig",
			vm:     nil,
			policy: extensions.PullPolicy_UNSPECIFIED_POLICY,
			expected: &wasmextensions.PluginConfig_VmConfig{
				VmConfig: &wasmextensions.VmConfig{
					Runtime: defaultRuntime,
					EnvironmentVariables: &wasmextensions.EnvironmentVariables{
						KeyValues: map[string]string{
							WasmSecretEnv:          "secret-name",
							WasmResourceVersionEnv: "dummy-resource-version",
						},
					},
				},
			},
		},
		{
			desc: "Build VMConfig on top of a base VMConfig",
			vm: &extensions.VmConfig{
				Env: []*extensions.EnvVar{
					{
						Name:      "POD_NAME",
						ValueFrom: extensions.EnvValueSource_HOST,
					},
					{
						Name:  "ENV1",
						Value: "VAL1",
					},
				},
			},
			policy: extensions.PullPolicy_UNSPECIFIED_POLICY,
			expected: &wasmextensions.PluginConfig_VmConfig{
				VmConfig: &wasmextensions.VmConfig{
					Runtime: defaultRuntime,
					EnvironmentVariables: &wasmextensions.EnvironmentVariables{
						HostEnvKeys: []string{"POD_NAME"},
						KeyValues: map[string]string{
							"ENV1":                 "VAL1",
							WasmSecretEnv:          "secret-name",
							WasmResourceVersionEnv: "dummy-resource-version",
						},
					},
				},
			},
		},
		{
			desc:   "Build VMConfig with if-not-present pull policy",
			vm:     nil,
			policy: extensions.PullPolicy_IfNotPresent,
			expected: &wasmextensions.PluginConfig_VmConfig{
				VmConfig: &wasmextensions.VmConfig{
					Runtime: defaultRuntime,
					EnvironmentVariables: &wasmextensions.EnvironmentVariables{
						KeyValues: map[string]string{
							WasmSecretEnv:          "secret-name",
							WasmPolicyEnv:          extensions.PullPolicy_name[int32(extensions.PullPolicy_IfNotPresent)],
							WasmResourceVersionEnv: "dummy-resource-version",
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := buildVMConfig(nil, "dummy-resource-version", &extensions.WasmPlugin{
				VmConfig:        tc.vm,
				ImagePullSecret: "secret-name",
				ImagePullPolicy: tc.policy,
			})
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestToSecretName(t *testing.T) {
	cases := []struct {
		name                  string
		namespace             string
		want                  string
		wantResourceName      string
		wantResourceNamespace string
	}{
		{
			name:                  "sec",
			namespace:             "nm",
			want:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			wantResourceName:      "sec",
			wantResourceNamespace: "nm",
		},
		{
			name:                  "nm/sec",
			namespace:             "nm",
			want:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			wantResourceName:      "sec",
			wantResourceNamespace: "nm",
		},
		{
			name:      "nm2/sec",
			namespace: "nm",
			// Makes sure we won't search namespace outside of nm (which is the WasmPlugin namespace).
			want:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			wantResourceName:      "sec",
			wantResourceNamespace: "nm",
		},
		{
			name:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			namespace:             "nm",
			want:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			wantResourceName:      "sec",
			wantResourceNamespace: "nm",
		},
		{
			name:                  "kubernetes://nm2/sec",
			namespace:             "nm",
			want:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			wantResourceName:      "sec",
			wantResourceNamespace: "nm",
		},
		{
			name:                  "kubernetes://sec",
			namespace:             "nm",
			want:                  credentials.KubernetesSecretTypeURI + "nm/sec",
			wantResourceName:      "sec",
			wantResourceNamespace: "nm",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := toSecretResourceName(tt.name, tt.namespace)
			if got != tt.want {
				t.Errorf("got secret name %q, want %q", got, tt.want)
			}
			sr, err := credentials.ParseResourceName(got, tt.namespace, cluster.ID("cluster"), cluster.ID("cluster"))
			if err != nil {
				t.Error(err)
			}
			if sr.Name != tt.wantResourceName {
				t.Errorf("parse secret name got %v want %v", sr.Name, tt.name)
			}
			if sr.Namespace != tt.wantResourceNamespace {
				t.Errorf("parse secret name got %v want %v", sr.Name, tt.name)
			}
		})
	}
}

func TestFailStrategy(t *testing.T) {
	cases := []struct {
		desc  string
		proxy *Proxy
		in    *extensions.WasmPlugin
		out   bool
	}{
		{
			desc: "close",
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 24, Patch: 0},
			},
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_CLOSE,
			},
			out: false,
		},
		{
			desc: "open",
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 23, Patch: 0},
			},
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_OPEN,
			},
			out: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			out := convertToWasmPluginWrapper(config.Config{Spec: tc.in})
			if out == nil {
				t.Fatal("must not get nil")
			}
			filter := out.BuildHTTPWasmFilter(tc.proxy)
			if out == nil {
				t.Fatal("filter can not be nil")
			}

			// nolint: staticcheck // FailOpen deprecated
			if got := filter.Config.FailOpen; got != tc.out {
				t.Errorf("got %t, want %t", got, tc.out)
			}
		})
	}
}

func TestFailurePolicy(t *testing.T) {
	cases := []struct {
		desc  string
		proxy *Proxy
		in    *extensions.WasmPlugin
		out   wasmextensions.FailurePolicy
	}{
		{
			desc: "UNSPECIFIED",
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 24, Patch: 0},
			},
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_CLOSE,
			},
			out: wasmextensions.FailurePolicy_UNSPECIFIED,
		},
		{
			desc: "CLOSED",
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 25, Patch: 0},
			},
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_CLOSE,
			},
			out: wasmextensions.FailurePolicy_FAIL_CLOSED,
		},
		{
			desc: "OPEN",
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 25, Patch: 0},
			},
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_OPEN,
			},
			out: wasmextensions.FailurePolicy_FAIL_OPEN,
		},
		{
			desc: "RELOAD",
			proxy: &Proxy{
				IstioVersion: &IstioVersion{Major: 1, Minor: 25, Patch: 0},
			},
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_RELOAD,
			},
			out: wasmextensions.FailurePolicy_FAIL_RELOAD,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			out := convertToWasmPluginWrapper(config.Config{Spec: tc.in})
			if out == nil {
				t.Fatal("must not get nil")
			}
			filter := out.BuildHTTPWasmFilter(tc.proxy)
			if out == nil {
				t.Fatal("filter can not be nil")
			}

			if got := filter.Config.FailurePolicy; got != tc.out {
				t.Errorf("got %v, want %v", got, tc.out)
			}
		})
	}
}

func TestParseWasmPluginUrl(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *url.URL
		wantErr  bool
	}{
		{
			name:  "file scheme",
			input: "file:///path/to/plugin.wasm",
			expected: &url.URL{
				Scheme: "file",
				Path:   "/path/to/plugin.wasm",
			},
			wantErr: false,
		},
		{
			name:  "oci scheme",
			input: "oci://ghcr.io/istio/wasm-plugin:latest",
			expected: &url.URL{
				Scheme: "oci",
				Host:   "ghcr.io",
				Path:   "/istio/wasm-plugin:latest",
			},
			wantErr: false,
		},
		{
			name:  "http scheme",
			input: "http://example.com/plugin.wasm",
			expected: &url.URL{
				Scheme: "http",
				Host:   "example.com",
				Path:   "/plugin.wasm",
			},
			wantErr: false,
		},
		{
			name:  "https scheme",
			input: "https://example.com/plugin.wasm",
			expected: &url.URL{
				Scheme: "https",
				Host:   "example.com",
				Path:   "/plugin.wasm",
			},
			wantErr: false,
		},
		{
			name:  "no scheme - defaults to oci",
			input: "ghcr.io/istio/wasm-plugin:latest",
			expected: &url.URL{
				Scheme: "oci",
				Host:   "ghcr.io",
				Path:   "/istio/wasm-plugin:latest",
			},
			wantErr: false,
		},
		{
			name:  "image with port number - defaults to oci",
			input: "example.com:5000/wasm-plugin:latest",
			expected: &url.URL{
				Scheme: "oci",
				Host:   "example.com:5000",
				Path:   "/wasm-plugin:latest",
			},
			wantErr: false,
		},
		{
			name:     "invalid URL",
			input:    ":invalid",
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWasmPluginURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseWasmPluginURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if !reflect.DeepEqual(got.Scheme, tt.expected.Scheme) {
				t.Errorf("parseWasmPluginURL() scheme got = %v, want %v", got.Scheme, tt.expected.Scheme)
			}
			if !reflect.DeepEqual(got.Host, tt.expected.Host) {
				t.Errorf("parseWasmPluginURL() host got = %v, want %v", got.Host, tt.expected.Host)
			}
			if !reflect.DeepEqual(got.Path, tt.expected.Path) {
				t.Errorf("parseWasmPluginURL() path got = %v, want %v", got.Path, tt.expected.Path)
			}
		})
	}
}

func TestMatchListener(t *testing.T) {
	cases := []struct {
		desc         string
		wasmPlugin   *WasmPluginWrapper
		proxyLabels  map[string]string
		listenerInfo WasmPluginListenerInfo
		want         bool
	}{
		{
			desc:        "match and selector are nil",
			wasmPlugin:  &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{Selector: nil, Match: nil}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "only the workload selector is given",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{"a": "b"},
				},
				Match: nil,
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "mismatched selector",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{"e": "f"},
				},
				Match: nil,
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: false,
		},
		{
			desc: "default traffic selector value is matched with all the traffics",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "only workloadMode of the traffic selector is given",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: nil,
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "workloadMode of the traffic selector and empty list of ports are given",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "workloadMode of the traffic selector and numbered port are given",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "workloadMode of the traffic selector and mismatched ports are given",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1235}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: false,
		},
		{
			desc: "traffic selector is matched, but workload selector is not matched",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{"e": "f"},
				},
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: false,
		},
		{
			desc: "outbound traffic is matched with workloadMode CLIENT",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_CLIENT,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarOutbound,
			},
			want: true,
		},
		{
			desc: "any traffic is matched with workloadMode CLIENT_AND_SERVER",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_CLIENT_AND_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassUndefined,
			},
			want: true,
		},
		{
			desc: "gateway is matched with workloadMode CLIENT",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_CLIENT,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassGateway,
			},
			want: true,
		},
		{
			desc: "gateway is not matched with workloadMode SERVER",
			wasmPlugin: &WasmPluginWrapper{WasmPlugin: &extensions.WasmPlugin{
				Selector: nil,
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassGateway,
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			opts := WorkloadPolicyMatcher{
				WorkloadNamespace: "ns",
				WorkloadLabels:    tc.proxyLabels,
				IsWaypoint:        false,
				RootNamespace:     "istio-system",
			}
			got := tc.wasmPlugin.MatchListener(opts, tc.listenerInfo)
			if tc.want != got {
				t.Errorf("MatchListener got %v want %v", got, tc.want)
			}
		})
	}
}
