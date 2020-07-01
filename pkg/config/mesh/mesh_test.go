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

	"istio.io/istio/pkg/util/gogoprotomarshal"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation"
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
reportBatchMaxTime: 10s
enableTracing: false
defaultServiceExportTo: 
- "foo"
outboundTrafficPolicy:
  mode: REGISTRY_ONLY
clusterLocalNamespaces: 
- "foons"
defaultConfig:
  tracing: {}
  concurrency: 4`)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultConfig.Tracing.GetZipkin() != nil {
		t.Error("Failed to override tracing")
	}

	gotY, err := gogoprotomarshal.ToYAML(got)
	t.Log("Result: \n", gotY, err)
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
