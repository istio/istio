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
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasmextensions "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/test/util/assert"
)

func TestBuildDataSource(t *testing.T) {
	cases := []struct {
		url       string
		urlString string
		sha256    string
		expected  *core.AsyncDataSource
	}{
		{
			url:       "file://fake.wasm",
			urlString: "file://fake.wasm",
			sha256:    "",
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
			url:       "oci://ghcr.io/istio/fake-wasm:latest",
			urlString: "oci://ghcr.io/istio/fake-wasm:latest",
			sha256:    "fake-sha256",
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
			got := buildDataSource(u, tc.urlString, tc.sha256)
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
			got := buildVMConfig(nil, "dummy-resource-version",
				"secret-name", tc.policy, tc.vm)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestFailurePolicy(t *testing.T) {
	cases := []struct {
		desc  string
		proxy *Proxy
		in    *extensions.ExtensionFilter
		out   wasmextensions.FailurePolicy
	}{
		{
			desc: "UNSPECIFIED",
			in: &extensions.ExtensionFilter{
				Wasm: &extensions.WasmConfig{
					Url: "file://fake.wasm",
					// FailStrategy not set (zero value) defaults to FAIL_CLOSE, which maps to FAIL_CLOSED
				},
			},
			out: wasmextensions.FailurePolicy_FAIL_CLOSED,
		},
		{
			desc: "CLOSED",
			in: &extensions.ExtensionFilter{
				Wasm: &extensions.WasmConfig{
					Url:          "file://fake.wasm",
					FailStrategy: extensions.FailStrategy_FAIL_CLOSE,
				},
			},
			out: wasmextensions.FailurePolicy_FAIL_CLOSED,
		},
		{
			desc: "OPEN",
			in: &extensions.ExtensionFilter{
				Wasm: &extensions.WasmConfig{
					Url:          "file://fake.wasm",
					FailStrategy: extensions.FailStrategy_FAIL_OPEN,
				},
			},
			out: wasmextensions.FailurePolicy_FAIL_OPEN,
		},
		{
			desc: "RELOAD",
			in: &extensions.ExtensionFilter{
				Wasm: &extensions.WasmConfig{
					Url:          "file://fake.wasm",
					FailStrategy: extensions.FailStrategy_FAIL_RELOAD,
				},
			},
			out: wasmextensions.FailurePolicy_FAIL_RELOAD,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			out := convertToExtensionFilterWrapper(config.Config{Spec: tc.in})
			if out == nil {
				t.Fatal("must not get nil")
			}
			filter := out.BuildHTTPWasmFilter()
			if filter == nil {
				t.Fatal("filter can not be nil")
			}

			if got := filter.Config.FailurePolicy; got != tc.out {
				t.Errorf("got %v, want %v", got, tc.out)
			}
		})
	}
}

func TestConvertToExtensionFilterWrapper(t *testing.T) {
	cases := []struct {
		desc       string
		config     config.Config
		wantType   FilterType
		wantNil    bool
		wantErrLog string
	}{
		{
			desc: "valid lua config",
			config: config.Config{
				Meta: config.Meta{
					Name:            "test-lua",
					Namespace:       "default",
					ResourceVersion: "v1",
				},
				Spec: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request(request_handle)\nend",
					},
				},
			},
			wantType: FilterTypeLua,
			wantNil:  false,
		},
		{
			desc: "valid wasm config",
			config: config.Config{
				Meta: config.Meta{
					Name:            "test-wasm",
					Namespace:       "default",
					ResourceVersion: "v1",
				},
				Spec: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url: "oci://example.com/wasm:latest",
					},
				},
			},
			wantType: FilterTypeWasm,
			wantNil:  false,
		},
		{
			desc: "both wasm and lua set",
			config: config.Config{
				Meta: config.Meta{
					Name:      "test-both",
					Namespace: "default",
				},
				Spec: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url: "oci://example.com/wasm:latest",
					},
					Lua: &extensions.LuaConfig{
						InlineCode: "function envoy_on_request(request_handle)\nend",
					},
				},
			},
			wantNil:    true,
			wantErrLog: "both wasm and lua are set",
		},
		{
			desc: "neither wasm nor lua set",
			config: config.Config{
				Meta: config.Meta{
					Name:      "test-neither",
					Namespace: "default",
				},
				Spec: &extensions.ExtensionFilter{},
			},
			wantNil:    true,
			wantErrLog: "neither wasm nor lua is set",
		},
		{
			desc: "lua code empty",
			config: config.Config{
				Meta: config.Meta{
					Name:      "test-empty-lua",
					Namespace: "default",
				},
				Spec: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: "",
					},
				},
			},
			wantNil:    true,
			wantErrLog: "inlineCode cannot be empty",
		},
		{
			desc: "lua code too large",
			config: config.Config{
				Meta: config.Meta{
					Name:      "test-large-lua",
					Namespace: "default",
				},
				Spec: &extensions.ExtensionFilter{
					Lua: &extensions.LuaConfig{
						InlineCode: string(make([]byte, 65537)),
					},
				},
			},
			wantNil:    true,
			wantErrLog: "exceeds maximum size",
		},
		{
			desc: "wasm url empty",
			config: config.Config{
				Meta: config.Meta{
					Name:      "test-empty-url",
					Namespace: "default",
				},
				Spec: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Url: "",
					},
				},
			},
			wantNil:    true,
			wantErrLog: "wasm.url is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := convertToExtensionFilterWrapper(tc.config)
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
			} else {
				if got == nil {
					t.Fatalf("expected non-nil wrapper")
				}
				if got.FilterType != tc.wantType {
					t.Errorf("got FilterType %v, want %v", got.FilterType, tc.wantType)
				}
				if got.Name != tc.config.Name {
					t.Errorf("got Name %v, want %v", got.Name, tc.config.Name)
				}
				if got.Namespace != tc.config.Namespace {
					t.Errorf("got Namespace %v, want %v", got.Namespace, tc.config.Namespace)
				}
				expectedResourceName := ExtensionFilterResourceNamePrefix + tc.config.Namespace + "." + tc.config.Name
				if got.ResourceName != expectedResourceName {
					t.Errorf("got ResourceName %v, want %v", got.ResourceName, expectedResourceName)
				}
			}
		})
	}
}

func TestExtensionFilterWrapper_MatchType(t *testing.T) {
	cases := []struct {
		desc      string
		wrapper   *ExtensionFilterWrapper
		chainType FilterChainType
		want      bool
	}{
		{
			desc: "lua matches HTTP",
			wrapper: &ExtensionFilterWrapper{
				FilterType: FilterTypeLua,
			},
			chainType: FilterChainTypeHTTP,
			want:      true,
		},
		{
			desc: "lua matches Any",
			wrapper: &ExtensionFilterWrapper{
				FilterType: FilterTypeLua,
			},
			chainType: FilterChainTypeAny,
			want:      true,
		},
		{
			desc: "lua does not match Network",
			wrapper: &ExtensionFilterWrapper{
				FilterType: FilterTypeLua,
			},
			chainType: FilterChainTypeNetwork,
			want:      false,
		},
		{
			desc: "wasm HTTP matches HTTP",
			wrapper: &ExtensionFilterWrapper{
				FilterType: FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Type: extensions.PluginType_HTTP,
					},
				},
			},
			chainType: FilterChainTypeHTTP,
			want:      true,
		},
		{
			desc: "wasm Network matches Network",
			wrapper: &ExtensionFilterWrapper{
				FilterType: FilterTypeWasm,
				ExtensionFilter: &extensions.ExtensionFilter{
					Wasm: &extensions.WasmConfig{
						Type: extensions.PluginType_NETWORK,
					},
				},
			},
			chainType: FilterChainTypeNetwork,
			want:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := tc.wrapper.MatchType(tc.chainType)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchListener(t *testing.T) {
	cases := []struct {
		desc         string
		extensionFilter   *ExtensionFilterWrapper
		proxyLabels  map[string]string
		listenerInfo ListenerInfo
		want         bool
	}{
		{
			desc:        "match and selector are nil",
			extensionFilter:  &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{Selector: nil, Match: nil}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "only the workload selector is given",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{"a": "b"},
				},
				Match: nil,
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "mismatched selector",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{"e": "f"},
				},
				Match: nil,
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: false,
		},
		{
			desc: "default traffic selector value is matched with all the traffics",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "only workloadMode of the traffic selector is given",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: nil,
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "workloadMode of the traffic selector and empty list of ports are given",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "workloadMode of the traffic selector and numbered port are given",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: true,
		},
		{
			desc: "workloadMode of the traffic selector and mismatched ports are given",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1235}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: false,
		},
		{
			desc: "traffic selector is matched, but workload selector is not matched",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: map[string]string{"e": "f"},
				},
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarInbound,
			},
			want: false,
		},
		{
			desc: "outbound traffic is matched with workloadMode CLIENT",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_CLIENT,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassSidecarOutbound,
			},
			want: true,
		},
		{
			desc: "any traffic is matched with workloadMode CLIENT_AND_SERVER",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_CLIENT_AND_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassUndefined,
			},
			want: true,
		},
		{
			desc: "gateway is matched with workloadMode CLIENT",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_CLIENT,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
				Port:  1234,
				Class: networking.ListenerClassGateway,
			},
			want: true,
		},
		{
			desc: "gateway is not matched with workloadMode SERVER",
			extensionFilter: &ExtensionFilterWrapper{ExtensionFilter: &extensions.ExtensionFilter{
				Selector: nil,
				Match: []*extensions.TrafficSelector{
					{
						Mode:  v1beta1.WorkloadMode_SERVER,
						Ports: []*v1beta1.PortSelector{{Number: 1234}},
					},
				},
			}},
			proxyLabels: map[string]string{"a": "b", "c": "d"},
			listenerInfo: ListenerInfo{
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
			got := tc.extensionFilter.MatchListener(opts, tc.listenerInfo)
			if tc.want != got {
				t.Errorf("MatchListener got %v want %v", got, tc.want)
			}
		})
	}
}
