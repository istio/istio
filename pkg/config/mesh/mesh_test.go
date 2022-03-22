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

package mesh_test

import (
	"fmt"
	"reflect"
	"testing"

	ptypes "github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

func TestApplyProxyConfig(t *testing.T) {
	config := mesh.DefaultMeshConfig()
	defaultDiscovery := config.DefaultConfig.DiscoveryAddress

	t.Run("apply single", func(t *testing.T) {
		mc, err := mesh.ApplyProxyConfig("discoveryAddress: foo", config)
		if err != nil {
			t.Fatal(err)
		}
		if mc.DefaultConfig.DiscoveryAddress != "foo" {
			t.Fatalf("expected discoveryAddress: foo, got %q", mc.DefaultConfig.DiscoveryAddress)
		}
	})

	t.Run("apply again", func(t *testing.T) {
		mc, err := mesh.ApplyProxyConfig("drainDuration: 5s", config)
		if err != nil {
			t.Fatal(err)
		}
		// Ensure we didn't modify the passed in mesh config
		if mc.DefaultConfig.DiscoveryAddress != defaultDiscovery {
			t.Fatalf("expected discoveryAddress: %q, got %q", defaultDiscovery, mc.DefaultConfig.DiscoveryAddress)
		}
		if mc.DefaultConfig.DrainDuration.Seconds != 5 {
			t.Fatalf("expected drainDuration: 5s, got %q", mc.DefaultConfig.DrainDuration.Seconds)
		}
	})

	t.Run("apply proxy metadata", func(t *testing.T) {
		config := mesh.DefaultMeshConfig()
		config.DefaultConfig.ProxyMetadata = map[string]string{
			"merged":  "original",
			"default": "foo",
		}
		mc, err := mesh.ApplyProxyConfig(`proxyMetadata: {"merged":"override","override":"bar"}`, config)
		assert.NoError(t, err)
		// Ensure we didn't modify the passed in mesh config
		assert.Equal(t, mc.DefaultConfig.ProxyMetadata, map[string]string{
			"merged":   "override",
			"default":  "foo",
			"override": "bar",
		}, "unexpected proxy metadata")
	})
	t.Run("apply proxy metadata to mesh config", func(t *testing.T) {
		config := mesh.DefaultMeshConfig()
		config.DefaultConfig.ProxyMetadata = map[string]string{
			"merged":  "original",
			"default": "foo",
		}
		mc, err := mesh.ApplyMeshConfig(`defaultConfig:
  proxyMetadata: {"merged":"override","override":"bar"}`, config)
		if err != nil {
			t.Fatal(err)
		}
		// Ensure we didn't modify the passed in mesh config
		assert.Equal(t, mc.DefaultConfig.ProxyMetadata, map[string]string{
			"merged":   "override",
			"default":  "foo",
			"override": "bar",
		}, "unexpected proxy metadata")
	})
	t.Run("apply should not modify", func(t *testing.T) {
		config := mesh.DefaultMeshConfig()
		config.DefaultConfig.ProxyMetadata = map[string]string{
			"foo": "bar",
		}
		orig, err := gogoprotomarshal.ToYAML(config)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := mesh.ApplyProxyConfig(`proxyMetadata: {"merged":"override","override":"bar"}`, config); err != nil {
			t.Fatal(err)
		}
		after, err := gogoprotomarshal.ToYAML(config)
		if err != nil {
			t.Fatal(err)
		}
		if orig != after {
			t.Fatalf("Changed before and after. Expected %v, got %v", orig, after)
		}
	})
}

func TestDefaultProxyConfig(t *testing.T) {
	proxyConfig := mesh.DefaultProxyConfig()
	if err := validation.ValidateMeshConfigProxyConfig(&proxyConfig); err != nil {
		t.Errorf("validation of default proxy config failed with %v", err)
	}
}

func TestDefaultMeshConfig(t *testing.T) {
	if err := validation.ValidateMeshConfig(mesh.DefaultMeshConfig()); err != nil {
		t.Errorf("validation of default mesh config failed with %v", err)
	}
}

func TestApplyMeshConfigDefaults(t *testing.T) {
	configPath := "/test/config/patch"
	yaml := fmt.Sprintf(`
defaultConfig:
  configPath: %s
`, configPath)

	want := mesh.DefaultMeshConfig()
	want.DefaultConfig.ConfigPath = configPath

	got, err := mesh.ApplyMeshConfigDefaults(yaml)
	if err != nil {
		t.Fatalf("ApplyMeshConfigDefaults() failed: %v", err)
	}
	assert.Equal(t, got, want)
	// Verify overrides
	got, err = mesh.ApplyMeshConfigDefaults(`
serviceSettings: 
  - settings:
      clusterLocal: true
    host:
      - "*.myns.svc.cluster.local"
ingressClass: foo
enableTracing: false
trustDomainAliases: ["default", "both"]
defaultServiceExportTo: 
- "foo"
outboundTrafficPolicy:
  mode: REGISTRY_ONLY
clusterLocalNamespaces: 
- "foons"
defaultProviders:
  tracing: [foo]
extensionProviders:
- name: sd
  stackdriver: {}
defaultConfig:
  tracing: {}
  concurrency: 4`)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultConfig.Tracing.GetZipkin() != nil {
		t.Error("Failed to override tracing")
	}
	if len(got.DefaultProviders.GetMetrics()) != 0 {
		t.Errorf("default providers deep merge failed, got %v", got.DefaultProviders.GetMetrics())
	}
	if !reflect.DeepEqual(getExtensionProviders(got.ExtensionProviders), []string{"prometheus", "stackdriver", "envoy", "sd"}) {
		t.Errorf("extension providers deep merge failed, got %v", getExtensionProviders(got.ExtensionProviders))
	}
	if len(got.TrustDomainAliases) != 2 {
		t.Errorf("trust domain aliases deep merge failed")
	}

	gotY, err := gogoprotomarshal.ToYAML(got)
	t.Log("Result: \n", gotY, err)
}

func getExtensionProviders(eps []*meshconfig.MeshConfig_ExtensionProvider) []string {
	got := []string{}
	for _, ep := range eps {
		got = append(got, ep.Name)
	}
	return got
}

func TestDeepMerge(t *testing.T) {
	cases := []struct {
		name string
		in   string
		out  string
	}{
		{
			name: "set other default provider",
			in: `
defaultProviders:
  tracing: [foo]`,
			out: `defaultProviders:
  metrics:
  - stackdriver
  tracing:
  - foo
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAttributes: 3
trustDomainAliases: ["both", "default"]
`,
		},
		{
			name: "override default provider",
			in: `
defaultProviders:
  metrics: [foo]`,
			out: `defaultProviders:
  metrics:
  - foo
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAttributes: 3
trustDomainAliases: ["both", "default"]
`,
		},
		{
			name: "replace builtin provider",
			in: `
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAnnotations: 5`,
			out: `defaultProviders:
  metrics:
  - stackdriver
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAnnotations: 5
trustDomainAliases: ["both", "default"]
`,
		},
		{
			name: "add provider with existing type",
			in: `
extensionProviders:
- name: stackdriver-annotations
  stackdriver:
    maxNumberOfAnnotations: 5`,
			out: `defaultProviders:
  metrics:
  - stackdriver
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAttributes: 3
- name: stackdriver-annotations
  stackdriver:
    maxNumberOfAnnotations: 5
trustDomainAliases: ["both", "default"]
`,
		},
		{
			name: "add provider",
			in: `
extensionProviders:
- name: prometheus
  prometheus: {}`,
			out: `defaultProviders:
  metrics:
  - stackdriver
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAttributes: 3
- name: prometheus
  prometheus: {}
trustDomainAliases: ["both", "default"]
`,
		},
		{
			name: "add trust domain aliases",
			in: `
trustDomainAliases: ["added", "both"]`,
			out: `defaultProviders:
  metrics:
  - stackdriver
extensionProviders:
- name: stackdriver
  stackdriver:
    maxNumberOfAttributes: 3
trustDomainAliases:
- added
- both
- default
`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mc := mesh.DefaultMeshConfig()
			mc.DefaultProviders = &meshconfig.MeshConfig_DefaultProviders{
				Metrics: []string{"stackdriver"},
			}
			mc.ExtensionProviders = []*meshconfig.MeshConfig_ExtensionProvider{{
				Name: "stackdriver",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_Stackdriver{
					Stackdriver: &meshconfig.MeshConfig_ExtensionProvider_StackdriverProvider{
						MaxNumberOfAttributes: &ptypes.Int64Value{Value: 3},
					},
				},
			}}
			mc.TrustDomainAliases = []string{"default", "both"}
			res, err := mesh.ApplyMeshConfig(tt.in, mc)
			if err != nil {
				t.Fatal(err)
			}
			// Just extract fields we are testing
			minimal := &meshconfig.MeshConfig{}
			minimal.DefaultProviders = res.DefaultProviders
			minimal.ExtensionProviders = res.ExtensionProviders
			minimal.TrustDomainAliases = res.TrustDomainAliases

			want := &meshconfig.MeshConfig{}
			gogoprotomarshal.ApplyYAML(tt.out, want)
			if d := cmp.Diff(want, minimal, protocmp.Transform()); d != "" {
				t.Fatalf("got diff %v", d)
			}
		})
	}
}

func TestApplyMeshNetworksDefaults(t *testing.T) {
	yml := `
networks:
  network1:
    endpoints:
    - fromCidr: "192.168.0.1/24"
    gateways:
    - address: 1.1.1.1
      port: 80
  network2:
    endpoints:
    - fromRegistry: reg1
    gateways:
    - registryServiceName: reg1
      port: 443
`

	want := mesh.EmptyMeshNetworks()
	want.Networks = map[string]*meshconfig.Network{
		"network1": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{
				{
					Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
						FromCidr: "192.168.0.1/24",
					},
				},
			},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{
				{
					Gw: &meshconfig.Network_IstioNetworkGateway_Address{
						Address: "1.1.1.1",
					},
					Port: 80,
				},
			},
		},
		"network2": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{
				{
					Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
						FromRegistry: "reg1",
					},
				},
			},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{
				{
					Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
						RegistryServiceName: "reg1",
					},
					Port: 443,
				},
			},
		},
	}

	got, err := mesh.ParseMeshNetworks(yml)
	if err != nil {
		t.Fatalf("ApplyMeshNetworksDefaults() failed: %v", err)
	}
	assert.Equal(t, got, &want)
}
