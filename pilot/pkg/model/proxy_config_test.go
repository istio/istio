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
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1beta1"
	istioTypes "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
)

var now = time.Now()

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
				Concurrency: &wrappers.Int32Value{Value: 3},
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: &wrappers.Int32Value{Value: 3},
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
		assert.Equal(t, converted, tc.expected)
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
			name: "source concurrency nil",
			first: &meshconfig.ProxyConfig{
				Concurrency: nil,
			},
			second: &meshconfig.ProxyConfig{
				Concurrency: v(2),
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: v(2),
			},
		},
		{
			name: "dest concurrency nil",
			first: &meshconfig.ProxyConfig{
				Concurrency: v(2),
			},
			second: &meshconfig.ProxyConfig{
				Concurrency: nil,
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: v(2),
			},
		},
		{
			name: "both concurrency nil",
			first: &meshconfig.ProxyConfig{
				Concurrency: nil,
			},
			second: &meshconfig.ProxyConfig{
				Concurrency: nil,
			},
			expected: &meshconfig.ProxyConfig{
				Concurrency: nil,
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
		{
			name: "empty envars merge with populated",
			first: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{},
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
					"a": "z",
					"b": "y",
					"c": "d",
				},
			},
		},
		{
			name:  "nil proxyconfig",
			first: nil,
			second: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{
					"a": "z",
					"b": "y",
					"c": "d",
				},
			},
			expected: &meshconfig.ProxyConfig{
				ProxyMetadata: map[string]string{
					"a": "z",
					"b": "y",
					"c": "d",
				},
			},
		},
	}

	for _, tc := range cases {
		merged := mergeWithPrecedence(tc.first, tc.second)
		assert.Equal(t, merged, tc.expected)
	}
}

func TestEffectiveProxyConfig(t *testing.T) {
	cases := []struct {
		name          string
		configs       []config.Config
		defaultConfig *meshconfig.ProxyConfig
		proxy         *NodeMetadata
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
			proxy:    newMeta("test-ns", nil, nil),
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
			proxy:         newMeta("bar", nil, nil),
			expected:      &meshconfig.ProxyConfig{Concurrency: v(3)},
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
			proxy:    newMeta("test-ns", map[string]string{"test": "selector"}, nil),
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
			proxy: newMeta(
				"test-ns",
				map[string]string{
					"test": "selector",
				}, map[string]string{
					annotation.ProxyConfig.Name: "{ \"concurrency\": 5 }",
				}),
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
			proxy:    newMeta("test-ns", map[string]string{"test": "selector"}, nil),
			expected: &meshconfig.ProxyConfig{Concurrency: v(3)},
		},
		{
			name: "multiple matching workload CRs, oldest applies",
			configs: []config.Config{
				setCreationTimestamp(newProxyConfig("workload-a", "test-ns",
					&v1beta1.ProxyConfig{
						Selector: selector(map[string]string{
							"test": "selector",
						}),
						EnvironmentVariables: map[string]string{
							"A": "1",
						},
					}), now),
				setCreationTimestamp(newProxyConfig("workload-b", "test-ns",
					&v1beta1.ProxyConfig{
						Selector: selector(map[string]string{
							"test": "selector",
						}),
						EnvironmentVariables: map[string]string{
							"B": "2",
						},
					}), now.Add(time.Hour)),
				setCreationTimestamp(newProxyConfig("workload-c", "test-ns",
					&v1beta1.ProxyConfig{
						Selector: selector(map[string]string{
							"test": "selector",
						}),
						EnvironmentVariables: map[string]string{
							"C": "3",
						},
					}), now.Add(time.Hour)),
			},
			proxy: newMeta(
				"test-ns",
				map[string]string{
					"test": "selector",
				}, map[string]string{}),
			expected: &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{
				"A": "1",
			}},
		},
		{
			name: "multiple matching namespace CRs, oldest applies",
			configs: []config.Config{
				setCreationTimestamp(newProxyConfig("workload-a", "test-ns",
					&v1beta1.ProxyConfig{
						EnvironmentVariables: map[string]string{
							"A": "1",
						},
					}), now),
				setCreationTimestamp(newProxyConfig("workload-b", "test-ns",
					&v1beta1.ProxyConfig{
						EnvironmentVariables: map[string]string{
							"B": "2",
						},
					}), now.Add(time.Hour)),
				setCreationTimestamp(newProxyConfig("workload-c", "test-ns",
					&v1beta1.ProxyConfig{
						EnvironmentVariables: map[string]string{
							"C": "3",
						},
					}), now.Add(time.Hour)),
			},
			proxy: newMeta(
				"test-ns",
				map[string]string{}, map[string]string{}),
			expected: &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{
				"A": "1",
			}},
		},
		{
			name:  "no configured CR or default config",
			proxy: newMeta("ns", nil, nil),
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
				tc.proxy,
				&meshconfig.MeshConfig{
					RootNamespace: istioRootNamespace,
					DefaultConfig: tc.defaultConfig,
				})
			pc := mesh.DefaultProxyConfig()
			proto.Merge(pc, tc.expected)

			assert.Equal(t, merged, pc)
		})
	}
}

func newProxyConfig(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.ProxyConfig,
			Name:             name,
			Namespace:        ns,
		},
		Spec: spec,
	}
}

func newProxyConfigStore(t *testing.T, configs []config.Config) ConfigStore {
	t.Helper()

	store := NewFakeStore()
	for _, cfg := range configs {
		store.Create(cfg)
	}

	return MakeIstioStore(store)
}

func setCreationTimestamp(c config.Config, t time.Time) config.Config {
	c.Meta.CreationTimestamp = t
	return c
}

func newMeta(ns string, labels, annotations map[string]string) *NodeMetadata {
	return &NodeMetadata{
		Namespace:   ns,
		Labels:      labels,
		Annotations: annotations,
	}
}

func v(x int32) *wrappers.Int32Value {
	return &wrappers.Int32Value{Value: x}
}

func selector(l map[string]string) *istioTypes.WorkloadSelector {
	return &istioTypes.WorkloadSelector{MatchLabels: l}
}
