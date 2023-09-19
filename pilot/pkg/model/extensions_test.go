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
		desc string
		in   *extensions.WasmPlugin
		out  bool
	}{
		{
			desc: "close",
			in: &extensions.WasmPlugin{
				Url:          "file://fake.wasm",
				FailStrategy: extensions.FailStrategy_FAIL_CLOSE,
			},
			out: false,
		},
		{
			desc: "open",
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
				t.Fatalf("must not get nil")
			}
			filter := out.BuildHTTPWasmFilter()
			if out == nil {
				t.Fatalf("filter can not be nil")
			}
			if got := filter.Config.FailOpen; got != tc.out {
				t.Errorf("got %t, want %t", got, tc.out)
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
			opts := WorkloadSelectionOpts{
				RootNamespace:  "root",
				Namespace:      "ns",
				WorkloadLabels: tc.proxyLabels,
				IsWaypoint:     false,
			}
			got := tc.wasmPlugin.MatchListener(opts, tc.listenerInfo)
			if tc.want != got {
				t.Errorf("MatchListener got %v want %v", got, tc.want)
			}
		})
	}
}
