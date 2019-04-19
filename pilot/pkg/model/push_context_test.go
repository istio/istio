// Copyright 2019 Istio Authors
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
	"fmt"
	"reflect"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
)

func createTestDestinationRuleConfig(name string, namespace string, host string) *Config {
	return &Config{
		ConfigMeta: ConfigMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &networking.DestinationRule{
			Host: host,
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
			},
		},
	}
}

func createTestDestionationRule(name Hostname, config *Config) map[Hostname]*Config {
	return map[Hostname]*Config{name: config}
}

var (
	pushContext1     = NewPushContext()
	drConfig1        = createTestDestinationRuleConfig("test", "test-namespace", "test-host-1")
	drConfig2        = createTestDestinationRuleConfig("test", "test-namespace", "test-host-2")
	drConfig3        = createTestDestinationRuleConfig("test", "test-namespace", "test-host-3")
	drConfig4        = createTestDestinationRuleConfig("test", "test-namespace", "test-host-4")
	drConfig5        = createTestDestinationRuleConfig("test", "test-namespace", "test-host-5")
	dr1              = createTestDestionationRule("test-host-1", drConfig1)
	sidecarTypeProxy = &Proxy{
		SidecarScope: &SidecarScope{
			Config:           drConfig1,
			destinationRules: dr1,
		},
		Type: SidecarProxy,
	}

	nonSidecarTypeProxy = &Proxy{
		Type:            Router,
		ConfigNamespace: "test-namespace",
	}

	pushContext2 = &PushContext{
		allExportedDestRules: &processedDestRules{
			hosts: []Hostname{"test-host-2"},
			destRule: map[Hostname]*combinedDestinationRule{
				"test-host-2": &combinedDestinationRule{
					config: drConfig2,
				},
			},
		},
	}

	pushContext3 = &PushContext{
		Env: &Environment{
			Mesh: &meshconfig.MeshConfig{
				RootNamespace: "istio-config",
			},
		},
		namespaceLocalDestRules: map[string]*processedDestRules{
			"test-namespace": &processedDestRules{
				hosts: []Hostname{"test-host-3"},
				destRule: map[Hostname]*combinedDestinationRule{
					"test-host-3": &combinedDestinationRule{
						config: drConfig3,
					},
				},
			},
		},
	}

	pushContext4 = &PushContext{
		Env: &Environment{
			Mesh: &meshconfig.MeshConfig{
				RootNamespace: "istio-config",
			},
		},
		namespaceLocalDestRules: map[string]*processedDestRules{
			"test-namespace": &processedDestRules{
				hosts: []Hostname{"non-matching"},
				destRule: map[Hostname]*combinedDestinationRule{
					"non-matching": &combinedDestinationRule{
						config: createTestDestinationRuleConfig("test", "test-namespace", "non-matching"),
					},
				},
			},
		},
		namespaceExportedDestRules: map[string]*processedDestRules{
			"test-namespace": &processedDestRules{
				hosts: []Hostname{"test-host-4"},
				destRule: map[Hostname]*combinedDestinationRule{
					"test-host-4": &combinedDestinationRule{
						config: drConfig4,
					},
				},
			},
		},
	}

	pushContext5 = &PushContext{
		Env: &Environment{
			Mesh: &meshconfig.MeshConfig{
				RootNamespace: "istio-config",
			},
		},
		namespaceExportedDestRules: map[string]*processedDestRules{
			"istio-config": &processedDestRules{
				hosts: []Hostname{"test-host-5"},
				destRule: map[Hostname]*combinedDestinationRule{
					"test-host-5": &combinedDestinationRule{
						config: drConfig5,
					},
				},
			},
		},
	}

	pushContext6 = &PushContext{
		Env: &Environment{
			Mesh: &meshconfig.MeshConfig{
				RootNamespace: "istio-config",
			},
		},
		namespaceLocalDestRules: map[string]*processedDestRules{
			"ns1": &processedDestRules{
				hosts: []Hostname{"non-matching1"},
				destRule: map[Hostname]*combinedDestinationRule{
					"non-matching1": &combinedDestinationRule{
						config: createTestDestinationRuleConfig("test", "test-namespace", "non-matching1"),
					},
				},
			},
		},
		namespaceExportedDestRules: map[string]*processedDestRules{
			"ns2": &processedDestRules{
				hosts: []Hostname{"non-matching2"},
				destRule: map[Hostname]*combinedDestinationRule{
					"non-matching2": &combinedDestinationRule{
						config: createTestDestinationRuleConfig("test", "test-namespace", "non-matching2"),
					},
				},
			},
		},
	}
)

func TestDestinationRule(t *testing.T) {
	tests := []struct {
		name           string
		pushContext    *PushContext
		proxy          *Proxy
		service        *Service
		expectedConfig *Config
	}{
		{
			name:        "when proxy has a sidecar scope, then return a matching rule from the sidecar configuration",
			pushContext: pushContext1,
			proxy:       sidecarTypeProxy,
			service: &Service{
				Hostname: "test-host-1",
			},
			expectedConfig: drConfig1,
		},
		{
			name: "when proxy is nil, check for a public rule in all namespaces, then return a matching rule else " +
				"return nil",
			pushContext: pushContext2,
			service: &Service{
				Hostname: "test-host-2",
			},
			expectedConfig: drConfig2,
		},
		{
			name: "when proxy is not nil, and does not have a sidecar scope, and if proxy's namespace is different " +
				"from root namespace, then return a matching rule in the proxy's namespace",
			pushContext: pushContext3,
			proxy:       nonSidecarTypeProxy,
			service: &Service{
				Hostname: "test-host-3",
			},
			expectedConfig: drConfig3,
		},
		{
			name: "when proxy is not nil, and does not have a sidecar scope and there are no private/public rules " +
				"matched in the calling proxy's namespace, then return matching rule in the target's namespace",
			pushContext: pushContext4,
			proxy:       nonSidecarTypeProxy,
			service: &Service{
				Hostname: "test-host-4",
				Attributes: ServiceAttributes{
					Namespace: "test-namespace",
				},
			},
			expectedConfig: drConfig4,
		},
		{
			name: "when proxy is not nil, and does not have a sidecar scope, and there are no public or private rule " +
				"in the calling proxy's namespace, then return a matching public rule in the config root's namespace",
			pushContext: pushContext5,
			proxy:       nonSidecarTypeProxy,
			service: &Service{
				Hostname: "test-host-5",
			},
			expectedConfig: drConfig5,
		},
		{
			name: "when proxy is not nil, and does not have a sidecar scope, and there are no public or private rule" +
				"in the calling proxy's namespace, and no matching public rule in the config root's namespace then return nil",
			pushContext: pushContext6,
			proxy:       nonSidecarTypeProxy,
			service: &Service{
				Hostname: "test-host-6",
			},
			expectedConfig: nil,
		},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			config := tt.pushContext.DestinationRule(tt.proxy, tt.service)
			if !reflect.DeepEqual(config, tt.expectedConfig) {
				t.Errorf("DestinationRule(%s): expected config (%v), got (%v)", tt.name, tt.expectedConfig, config)
			}
		})
	}
}
