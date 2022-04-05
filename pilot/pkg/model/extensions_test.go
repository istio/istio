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

	envoyCoreV3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyExtensionsWasmV3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/test/util/assert"
)

func TestBuildDataSource(t *testing.T) {
	cases := []struct {
		url        string
		wasmPlugin *extensions.WasmPlugin

		expected *envoyCoreV3.AsyncDataSource
	}{
		{
			url: "file://fake.wasm",
			wasmPlugin: &extensions.WasmPlugin{
				Url: "file://fake.wasm",
			},
			expected: &envoyCoreV3.AsyncDataSource{
				Specifier: &envoyCoreV3.AsyncDataSource_Local{
					Local: &envoyCoreV3.DataSource{
						Specifier: &envoyCoreV3.DataSource_Filename{
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
			expected: &envoyCoreV3.AsyncDataSource{
				Specifier: &envoyCoreV3.AsyncDataSource_Remote{
					Remote: &envoyCoreV3.RemoteDataSource{
						HttpUri: &envoyCoreV3.HttpUri{
							Uri:     "oci://ghcr.io/istio/fake-wasm:latest",
							Timeout: durationpb.New(30 * time.Second),
							HttpUpstreamType: &envoyCoreV3.HttpUri_Cluster{
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
		vm       *extensions.VmConfig
		expected *envoyExtensionsWasmV3.PluginConfig_VmConfig
	}{
		{
			vm: nil,
			expected: &envoyExtensionsWasmV3.PluginConfig_VmConfig{
				VmConfig: &envoyExtensionsWasmV3.VmConfig{
					Runtime: defaultRuntime,
					EnvironmentVariables: &envoyExtensionsWasmV3.EnvironmentVariables{
						KeyValues: map[string]string{
							WasmSecretEnv: "secret-name",
						},
					},
				},
			},
		},
		{
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
			expected: &envoyExtensionsWasmV3.PluginConfig_VmConfig{
				VmConfig: &envoyExtensionsWasmV3.VmConfig{
					Runtime: defaultRuntime,
					EnvironmentVariables: &envoyExtensionsWasmV3.EnvironmentVariables{
						HostEnvKeys: []string{"POD_NAME"},
						KeyValues: map[string]string{
							"ENV1":        "VAL1",
							WasmSecretEnv: "secret-name",
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			got := buildVMConfig(nil, tc.vm, "secret-name")
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
