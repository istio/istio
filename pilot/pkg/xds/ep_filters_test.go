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

package xds

import (
	"sort"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

type LbEpInfo struct {
	address string
	// nolint: structcheck
	weight uint32
}

type LocLbEpInfo struct {
	lbEps  []LbEpInfo
	weight uint32
}

var networkFiltered = []networkFilterCase{
	{
		name: "from_network1",
		conn: xdsConnection("network1"),
		want: []LocLbEpInfo{
			{
				lbEps: []LbEpInfo{
					// 2 local endpoints
					{address: "10.0.0.1", weight: 2},
					{address: "10.0.0.2", weight: 2},
					// 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
					{address: "2.2.2.2", weight: 1},
					{address: "2.2.2.20", weight: 1},
					// network4 has no gateway, which means it can be accessed from network1
					{address: "40.0.0.1", weight: 2},
				},
				weight: 8,
			},
		},
	},
	{
		name: "from_network2",
		conn: xdsConnection("network2"),
		want: []LocLbEpInfo{
			{
				lbEps: []LbEpInfo{
					// 1 local endpoint
					{address: "20.0.0.1", weight: 2},
					// 1 endpoint to gateway of network1 with weight 4 because it has 2 endpoints
					{address: "1.1.1.1", weight: 4},
					{address: "40.0.0.1", weight: 2},
				},
				weight: 8,
			},
		},
	},
	{
		name: "from_network3",
		conn: xdsConnection("network3"),
		want: []LocLbEpInfo{
			{
				lbEps: []LbEpInfo{
					// 1 endpoint to gateway of network1 with weight 4 because it has 2 endpoints
					{address: "1.1.1.1", weight: 4},
					// 1 endpoint to gateway of network2 with weight 2 because it has 1 endpoint
					{address: "2.2.2.2", weight: 1},
					{address: "2.2.2.20", weight: 1},
					{address: "40.0.0.1", weight: 2},
				},
				weight: 8,
			},
		},
	},
	{
		name: "from_network4",
		conn: xdsConnection("network4"),
		want: []LocLbEpInfo{
			{
				lbEps: []LbEpInfo{
					// 1 local endpoint
					{address: "40.0.0.1", weight: 2},
					// 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
					{address: "1.1.1.1", weight: 4},
					// 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
					{address: "2.2.2.2", weight: 1},
					{address: "2.2.2.20", weight: 1},
				},
				weight: 8,
			},
		},
	},
}

func TestEndpointsByNetworkFilter(t *testing.T) {
	env := environment()
	env.Init()
	// The tests below are calling the endpoints filter from each one of the
	// networks and examines the returned filtered endpoints

	runNetworkFilterTest(t, env, networkFiltered)
}

func TestEndpointsByNetworkFilter_WithConfig(t *testing.T) {
	noCrossNetwork := []networkFilterCase{
		{
			name: "from_network1",
			conn: xdsConnection("network1"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 2 local endpoints
						{address: "10.0.0.1", weight: 2},
						{address: "10.0.0.2", weight: 2},
						// network4 has no gateway, which means it can be accessed from network1
						{address: "40.0.0.1", weight: 2},
					},
					weight: 6,
				},
			},
		},
		{
			name: "from_network2",
			conn: xdsConnection("network2"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 local endpoint
						{address: "20.0.0.1", weight: 2},
						// network4 has no gateway, which means it can be accessed from network2
						{address: "40.0.0.1", weight: 2},
					},
					weight: 4,
				},
			},
		},
		{
			name: "from_network3",
			conn: xdsConnection("network3"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// network4 has no gateway, which means it can be accessed from network3
						{address: "40.0.0.1", weight: 2},
					},
					weight: 2,
				},
			},
		},
		{
			name: "from_network4",
			conn: xdsConnection("network4"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 local endpoint
						{address: "40.0.0.1", weight: 2},
					},
					weight: 2,
				},
			},
		},
	}

	cases := map[string]map[string]struct {
		Config  config.Config
		Configs []config.Config
		Tests   []networkFilterCase
	}{
		gvk.PeerAuthentication.String(): {
			"mtls-off-ineffective": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-partial",
						Namespace:        "istio-system",
					},
					Spec: &security.PeerAuthentication{
						Selector: &v1beta1.WorkloadSelector{
							// shouldn't affect our test workload
							MatchLabels: map[string]string{"app": "b"},
						},
						Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
					},
				},
				Tests: networkFiltered,
			},
			"mtls-on-strict": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-on",
						Namespace:        "istio-system",
					},
					Spec: &security.PeerAuthentication{
						Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_STRICT},
					},
				},
				Tests: networkFiltered,
			},
			"mtls-off-global": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-off",
						Namespace:        "istio-system",
					},
					Spec: &security.PeerAuthentication{
						Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
					},
				},
				Tests: noCrossNetwork,
			},
			"mtls-off-namespace": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &security.PeerAuthentication{
						Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
					},
				},
				Tests: noCrossNetwork,
			},
			"mtls-off-workload": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &security.PeerAuthentication{
						Selector: &v1beta1.WorkloadSelector{
							MatchLabels: map[string]string{"app": "example"},
						},
						Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
					},
				},
				Tests: noCrossNetwork,
			},
			"mtls-off-port": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &security.PeerAuthentication{
						Selector: &v1beta1.WorkloadSelector{
							MatchLabels: map[string]string{"app": "example"},
						},
						PortLevelMtls: map[uint32]*security.PeerAuthentication_MutualTLS{
							8080: {Mode: security.PeerAuthentication_MutualTLS_DISABLE},
						},
					},
				},
				Tests: noCrossNetwork,
			},
		},
		gvk.DestinationRule.String(): {
			"mtls-on-override-pa": {
				Configs: []config.Config{
					{
						Meta: config.Meta{
							GroupVersionKind: gvk.PeerAuthentication,
							Name:             "mtls-off",
							Namespace:        "ns",
						},
						Spec: &security.PeerAuthentication{
							Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
						},
					},
					{
						Meta: config.Meta{
							GroupVersionKind: gvk.DestinationRule,
							Name:             "mtls-on",
							Namespace:        "ns",
						},
						Spec: &networking.DestinationRule{
							Host: "example.ns.svc.cluster.local",
							TrafficPolicy: &networking.TrafficPolicy{
								Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
							},
						},
					},
				},
				Tests: networkFiltered,
			},
			"mtls-off-innefective": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "other.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
						},
					},
				},
				Tests: networkFiltered,
			},
			"mtls-on-destination-level": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-on",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						},
					},
				},
				Tests: networkFiltered,
			},
			"mtls-on-port-level": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-on",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
								Port: &networking.PortSelector{Number: 80},
								Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
							}},
						},
					},
				},
				Tests: networkFiltered,
			},
			"mtls-off-destination-level": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
						},
					},
				},
				Tests: noCrossNetwork,
			},
			"mtls-off-port-level": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
								Port: &networking.PortSelector{Number: 80},
								Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
							}},
						},
					},
				},
				Tests: noCrossNetwork,
			},
			"mtls-off-subset-level": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							// should be overridden by subset
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						},
						Subsets: []*networking.Subset{{
							Name:   "disable-tls",
							Labels: map[string]string{"app": "example"},
							TrafficPolicy: &networking.TrafficPolicy{
								Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
							},
						}},
					},
				},
				Tests: noCrossNetwork,
			},
			"mtls-on-subset-level": {
				Config: config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-on",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							// should be overridden by subset
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
						},
						Subsets: []*networking.Subset{{
							Name:   "disable-tls",
							Labels: map[string]string{"app": "example"},
							TrafficPolicy: &networking.TrafficPolicy{
								Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
							},
						}},
					},
				},
				Tests: networkFiltered,
			},
		},
	}

	for configType, cases := range cases {
		t.Run(configType, func(t *testing.T) {
			for name, pa := range cases {
				t.Run(name, func(t *testing.T) {
					env := environment()
					cfgs := pa.Configs
					if pa.Config.Name != "" {
						cfgs = append(cfgs, pa.Config)
					}
					for _, cfg := range cfgs {
						_, err := env.IstioConfigStore.Create(cfg)
						if err != nil {
							t.Fatalf("failed creating %s: %v", cfg.Name, err)
						}
					}
					env.Init()
					runNetworkFilterTest(t, env, pa.Tests)
				})
			}
		})
	}
}

func TestEndpointsByNetworkFilter_SkipLBWithHostname(t *testing.T) {
	//  - 1 IP gateway for network1
	//  - 1 DNS gateway for network2
	//  - 1 IP gateway for network3
	//  - 0 gateways for network4
	env := environment()
	delete(env.Networks().Networks, "network2")
	origServices, _ := env.Services()
	serviceDiscovery := memregistry.NewServiceDiscovery(append([]*model.Service{{
		Hostname: "istio-ingressgateway.istio-system.svc.cluster.local",
		Attributes: model.ServiceAttributes{
			ClusterExternalAddresses: map[string][]string{
				"cluster2": {""},
			},
		},
	}}, origServices...))
	serviceDiscovery.SetGatewaysForNetwork("network2", &model.Gateway{Addr: "aeiou.scooby.do", Port: 80})

	env.ServiceDiscovery = serviceDiscovery
	env.Init()

	// The tests below are calling the endpoints filter from each one of the
	// networks and examines the returned filtered endpoints
	tests := []networkFilterCase{
		{
			name: "from_network1",
			conn: xdsConnection("network1"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 2 local endpoints
						{address: "10.0.0.1", weight: 1},
						{address: "10.0.0.2", weight: 1},
						// 0 endpoint to gateway of network2 a its a dns name instead of IP
						{address: "40.0.0.1", weight: 1},
					},
					weight: 3,
				},
			},
		},
		{
			name: "from_network2",
			conn: xdsConnection("network2"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 local endpoint
						{address: "20.0.0.1", weight: 1},
						// 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
						{address: "1.1.1.1", weight: 2},
						{address: "40.0.0.1", weight: 1},
					},
					weight: 4,
				},
			},
		},
		{
			name: "from_network3",
			conn: xdsConnection("network3"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
						{address: "1.1.1.1", weight: 2},
						// 0 endpoint to gateway of network2 as its a DNS gateway
						{address: "40.0.0.1", weight: 1},
					},
					weight: 3,
				},
			},
		},
		{
			name: "from_network4",
			conn: xdsConnection("network4"),
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 local endpoint
						{address: "40.0.0.1", weight: 1},
						// 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
						{address: "1.1.1.1", weight: 2},
						// 0 endpoint to gateway of network2 as its a dns gateway
					},
					weight: 3,
				},
			},
		},
	}
	runNetworkFilterTest(t, env, tests)
}

type networkFilterCase struct {
	name string
	conn *Connection
	want []LocLbEpInfo
}

// runNetworkFilterTest calls the endpoints filter from each one of the
// networks and examines the returned filtered endpoints
func runNetworkFilterTest(t *testing.T, env *model.Environment, tests []networkFilterCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			push := model.NewPushContext()
			_ = push.InitContext(env, nil, nil)
			b := NewEndpointBuilder("outbound|80||example.ns.svc.cluster.local", tt.conn.proxy, push)
			testEndpoints := b.buildLocalityLbEndpointsFromShards(testShards(), &model.Port{Name: "http", Port: 80, Protocol: protocol.HTTP})
			filtered := b.EndpointsByNetworkFilter(testEndpoints)
			for _, e := range testEndpoints {
				e.AssertInvarianceInTest()
			}
			if len(filtered) != len(tt.want) {
				t.Errorf("Unexpected number of filtered endpoints: got %v, want %v", len(filtered), len(tt.want))
				return
			}

			sort.Slice(filtered, func(i, j int) bool {
				addrI := filtered[i].llbEndpoints.LbEndpoints[0].GetEndpoint().Address.GetSocketAddress().Address
				addrJ := filtered[j].llbEndpoints.LbEndpoints[0].GetEndpoint().Address.GetSocketAddress().Address
				return addrI < addrJ
			})

			for i, ep := range filtered {
				if len(ep.llbEndpoints.LbEndpoints) != len(tt.want[i].lbEps) {
					t.Errorf("Unexpected number of LB endpoints within endpoint %d: %v, want %v", i, len(ep.llbEndpoints.LbEndpoints), len(tt.want[i].lbEps))
				}

				if ep.llbEndpoints.LoadBalancingWeight.GetValue() != tt.want[i].weight {
					t.Errorf("Unexpected weight for endpoint %d: got %v, want %v", i, ep.llbEndpoints.LoadBalancingWeight.GetValue(), tt.want[i].weight)
				}

				for _, lbEp := range ep.llbEndpoints.LbEndpoints {
					addr := lbEp.GetEndpoint().Address.GetSocketAddress().Address
					found := false
					for _, wantLbEp := range tt.want[i].lbEps {
						if addr == wantLbEp.address {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Unexpected address for endpoint %d: %v", i, addr)
					}
				}
			}
		})
	}
}

func xdsConnection(network string) *Connection {
	return &Connection{
		proxy: &model.Proxy{
			Metadata: &model.NodeMetadata{Network: network},
		},
	}
}

// environment defines the networks with:
//  - 1 gateway for network1
//  - 2 gateway for network2
//  - 1 gateway for network3
//  - 0 gateways for network4
func environment() *model.Environment {
	return &model.Environment{
		ServiceDiscovery: memregistry.NewServiceDiscovery([]*model.Service{
			{
				Hostname:   "example.ns.svc.cluster.local",
				Attributes: model.ServiceAttributes{Name: "example", Namespace: "ns"},
			},
		}),
		IstioConfigStore: model.MakeIstioStore(memory.Make(collections.Pilot)),
		Watcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
		NetworksWatcher: mesh.NewFixedNetworksWatcher(&meshconfig.MeshNetworks{
			Networks: map[string]*meshconfig.Network{
				"network1": {
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
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "2.2.2.2",
							},
							Port: 80,
						},
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "2.2.2.20",
							},
							Port: 80,
						},
					},
				},
				"network3": {
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "3.3.3.3",
							},
							Port: 443,
						},
					},
				},
				"network4": {
					Gateways: []*meshconfig.Network_IstioNetworkGateway{},
				},
			},
		}),
	}
}

// testShards creates endpoints to be handed to the filter:
//  - 2 endpoints in network1
//  - 1 endpoints in network2
//  - 0 endpoints in network3
//  - 1 endpoints in network4
//
// All endpoints are part of service example.ns.svc.cluster.local on port 80 (http) on cluster-0
func testShards() *EndpointShards {
	shards := &EndpointShards{Shards: map[string][]*model.IstioEndpoint{
		"cluster-0": {
			{Network: "network1", Address: "10.0.0.1"},
			{Network: "network1", Address: "10.0.0.2"},
			{Network: "network2", Address: "20.0.0.1"},
			{Network: "network4", Address: "40.0.0.1"},
		},
	}}
	// apply common properties
	for i, ep := range shards.Shards["cluster-0"] {
		ep.ServicePortName = "http"
		ep.Namespace = "ns"
		ep.HostName = "example.ns.svc.cluster.local"
		ep.EndpointPort = 8080
		ep.TLSMode = "istio"
		ep.Labels = map[string]string{"app": "example"}
		shards.Shards["cluster-0"][i] = ep
	}
	return shards
}
