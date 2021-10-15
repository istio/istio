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
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1beta1"
	istioTypes "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

const istioRootNamespace = "istio-system"

func TestConvertToMeshConfigProxyConfig(t *testing.T) {
	cases := []struct {
		name     string
		pc       *v1beta1.ProxyConfig
		expected *meshconfig.ProxyConfig
	}{
		{
			name: "concurrency",
			pc: &v1beta1.ProxyConfig{
				Concurrency: &types.Int32Value{Value: 3},
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: &types.Int32Value{Value: 3},
			},
		},
		{
			name: "environment variables",
			pc: &v1beta1.ProxyConfig{
				EnvironmentVariables: map[string]string{
					"a": "b",
					"c": "d",
				},
			},
			expected: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{
					"a": "b",
					"c": "d",
				},
			},
		},
	}

	for _, tc := range cases {
		converted := toMeshConfigProxyConfig(tc.pc)
		if diff := cmp.Diff(converted, tc.expected); diff != "" {
			t.Fatalf("expected and received not the same: %s", diff)
		}
	}
}

func TestMergeWithPrecedence(t *testing.T) {
	cases := []struct {
		name     string
		first    *meshconfig.ProxyConfig
		second   *meshconfig.ProxyConfig
		expected *meshconfig.ProxyConfig
	}{
		{
			name: "concurrency",
			first: &meshconfig.ProxyConfig{
				Concurrency: v(1),
			},
			second: &meshconfig.ProxyConfig{
				Concurrency: v(2),
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: v(1),
			},
		},
		{
			name: "concurrency value 0",
			first: &meshconfig.ProxyConfig{
				Concurrency: v(0),
			},
			second: &meshconfig.ProxyConfig{
				Concurrency: v(2),
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: v(0),
			},
		},
		{
			name: "envvars",
			first: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{
					"a": "x",
					"b": "y",
				},
			},
			second: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{
					"a": "z",
					"b": "y",
					"c": "d",
				},
			},
			expected: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{
					"a": "x",
					"b": "y",
					"c": "d",
				},
			},
		},
	}

	for _, tc := range cases {
		merged := mergeWithPrecedence(tc.first, tc.second)
		if diff := cmp.Diff(merged, tc.expected); diff != "" {
			t.Fatalf("expected and received not the same: %s", diff)
		}
	}
}

func TestEffectiveProxyConfig(t *testing.T) {
	cases := []struct {
		name          string
		configs       []config.Config
		defaultConfig *meshconfig.ProxyConfig
		target        *ProxyConfigTarget
		expected      *meshconfig.ProxyConfig
	}{
		{
			name: "CR applies to matching namespace",
			configs: []config.Config{
				newProxyConfig("ns", "test-ns",
					&v1beta1.ProxyConfig{
						Concurrency: v(3),
					}),
			},
			target: &ProxyConfigTarget{
				Namespace: "test-ns",
			},
			expected: &meshconfig.ProxyConfig{Concurrency: v(3)},
		},
		{
			name: "CR takes precedence over meshConfig.defaultConfig",
			configs: []config.Config{
				newProxyConfig("ns", istioRootNamespace,
					&v1beta1.ProxyConfig{
						Concurrency: v(3),
					}),
			},
			defaultConfig: &meshconfig.ProxyConfig{Concurrency: v(2)},
			target: &ProxyConfigTarget{
				Namespace: "bar",
			},
			expected: &meshconfig.ProxyConfig{Concurrency: v(3)},
		},
		{
			name: "workload matching CR takes precedence over namespace matching CR",
			configs: []config.Config{
				newProxyConfig("workload", "test-ns",
					&v1beta1.ProxyConfig{
						Selector: selector(map[string]string{
							"test": "selector",
						}),
						Concurrency: v(3),
					}),
				newProxyConfig("ns", "test-ns",
					&v1beta1.ProxyConfig{
						Concurrency: v(2),
					}),
			},
			target: &ProxyConfigTarget{
				Namespace: "test-ns",
				Labels: map[string]string{
					"test": "selector",
				},
			},
			expected: &meshconfig.ProxyConfig{Concurrency: v(3)},
		},
		{
			name: "matching workload CR takes precedence over annotation",
			configs: []config.Config{
				newProxyConfig("workload", "test-ns",
					&v1beta1.ProxyConfig{
						Selector: selector(map[string]string{
							"test": "selector",
						}),
						Concurrency: v(3),
					}),
			},
			target: &ProxyConfigTarget{
				Namespace: "test-ns",
				Annotations: map[string]string{
					annotation.ProxyConfig.Name: "{ \"concurrency\": 5 }",
				},
				Labels: map[string]string{
					"test": "selector",
				},
			},
			expected: &meshconfig.ProxyConfig{Concurrency: v(3)},
		},
		{
			name: "CR in other namespaces get ignored",
			configs: []config.Config{
				newProxyConfig("ns", "wrong-ns",
					&v1beta1.ProxyConfig{
						Concurrency: v(1),
					}),
				newProxyConfig("workload", "wrong-ns",
					&v1beta1.ProxyConfig{
						Selector: selector(map[string]string{
							"test": "selector",
						}),
						Concurrency: v(2),
					}),
				newProxyConfig("global", istioRootNamespace,
					&v1beta1.ProxyConfig{
						Concurrency: v(3),
					}),
			},
			target: &ProxyConfigTarget{
				Namespace: "test-ns",
			},
			expected: &meshconfig.ProxyConfig{Concurrency: v(3)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newProxyConfigStore(t, tc.configs)
			m := &meshconfig.MeshConfig{
				RootNamespace: istioRootNamespace,
				DefaultConfig: tc.defaultConfig,
			}
			pcs, err := GetProxyConfigs(store, m)
			if err != nil {
				t.Fatalf("failed to list proxyconfigs: %v", err)
			}
			merged := pcs.EffectiveProxyConfig(
				tc.target,
				&meshconfig.MeshConfig{
					RootNamespace: istioRootNamespace,
					DefaultConfig: tc.defaultConfig,
				})
			pc := mesh.DefaultProxyConfig()
			proto.Merge(&pc, tc.expected)
			if diff := cmp.Diff(merged, &pc); diff != "" {
				t.Fatalf("merged did not equal expected: %s", diff)
			}
		})
	}
}

func newProxyConfig(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.K8SNetworkingIstioIoV1Beta1Proxyconfigs.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        ns,
		},
		Spec: spec,
	}
}

func newProxyConfigStore(t *testing.T, configs []config.Config) IstioConfigStore {
	t.Helper()

	store := &proxyconfigStore{}
	for _, cfg := range configs {
		store.add(cfg)
	}

	return MakeIstioStore(store)
}

type proxyconfigStore struct {
	ConfigStore
	resources []struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}
}

func (pcs *proxyconfigStore) add(cfg config.Config) {
	pcs.resources = append(pcs.resources, struct {
		typ config.GroupVersionKind
		ns  string
		cfg config.Config
	}{
		typ: cfg.GroupVersionKind,
		ns:  cfg.Namespace,
		cfg: cfg,
	})
}

func (pcs *proxyconfigStore) Schemas() collection.Schemas {
	return collection.SchemasFor()
}

func (pcs *proxyconfigStore) Get(_ config.GroupVersionKind, _, _ string) *config.Config {
	return nil
}

func (pcs *proxyconfigStore) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	var configs []config.Config
	for _, data := range pcs.resources {
		if data.typ == typ {
			if namespace != "" && data.ns == namespace {
				continue
			}
			configs = append(configs, data.cfg)
		}
	}
	return configs, nil
}

func v(x int32) *types.Int32Value {
	return &types.Int32Value{Value: x}
}

func selector(l map[string]string) *istioTypes.WorkloadSelector {
	return &istioTypes.WorkloadSelector{MatchLabels: l}
}
