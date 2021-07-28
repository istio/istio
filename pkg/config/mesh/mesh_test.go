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
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/pkg/util/protomarshal"
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
		if err != nil {
			t.Fatal(err)
		}
		// Ensure we didn't modify the passed in mesh config
		if !reflect.DeepEqual(mc.DefaultConfig.ProxyMetadata, map[string]string{

			"merged":   "override",
			"default":  "foo",
			"override": "bar",
		}) {
			t.Fatalf("unexpected proxy metadata: %+v", mc.DefaultConfig.ProxyMetadata)
		}
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
		if !reflect.DeepEqual(mc.DefaultConfig.ProxyMetadata, map[string]string{

			"merged":   "override",
			"default":  "foo",
			"override": "bar",
		}) {
			t.Fatalf("unexpected proxy metadata: %+v", mc.DefaultConfig.ProxyMetadata)
		}
	})
}

func TestDefaultProxyConfig(t *testing.T) {
	proxyConfig := mesh.DefaultProxyConfig()
	if err := validation.ValidateProxyConfig(&proxyConfig); err != nil {
		t.Errorf("validation of default proxy config failed with %v", err)
	}
}

func TestDefaultMeshConfig(t *testing.T) {
	m := mesh.DefaultMeshConfig()
	if err := validation.ValidateMeshConfig(&m); err != nil {
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
	if !reflect.DeepEqual(got, &want) {
		t.Fatalf("Wrong default values:\n got %#v \nwant %#v", got, &want)
	}
	// Verify overrides
	got, err = mesh.ApplyMeshConfigDefaults(`
serviceSettings: 
  - settings:
      clusterLocal: true
    host:
      - "*.myns.svc.cluster.local"
ingressClass: foo
enableTracing: false
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
	if len(got.ExtensionProviders) != 2 {
		t.Errorf("extension providers deep merge failed")
	}

	gotY, err := gogoprotomarshal.ToYAML(got)
	t.Log("Result: \n", gotY, err)
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
			res, err := mesh.ApplyMeshConfig(tt.in, mc)
			if err != nil {
				t.Fatal(err)
			}
			// Just extract fields we are testing
			minimal := &meshconfig.MeshConfig{}
			minimal.DefaultProviders = res.DefaultProviders
			minimal.ExtensionProviders = res.ExtensionProviders

			want := &meshconfig.MeshConfig{}
			protomarshal.ApplyYAML(tt.out, want)
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
	if !reflect.DeepEqual(got, &want) {
		t.Fatalf("Wrong values:\n got %#v \nwant %#v", got, &want)
	}
}

func TestResolveHostsInNetworksConfig(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		modified bool
	}{
		{
			"Gateway with IP address",
			"9.142.3.1",
			false,
		},
		{
			"Gateway with localhost address",
			"localhost",
			true,
		},
		{
			"Gateway with empty address",
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"network": {
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_Address{
									Address: tt.address,
								},
							},
						},
					},
				},
			}
			mesh.ResolveHostsInNetworksConfig(config)
			addrAfter := config.Networks["network"].Gateways[0].GetAddress()
			if addrAfter == tt.address && tt.modified {
				t.Fatalf("Expected network address to be modified but it's the same as before calling the function")
			}
			if addrAfter != tt.address && !tt.modified {
				t.Fatalf("Expected network address not to be modified after calling the function")
			}
		})
	}
}
