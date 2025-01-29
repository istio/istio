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

package endpoints

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
)

// MockDiscovery is an in-memory ServiceDiscover with mock services
type localServiceDiscovery struct {
	services         []*model.Service
	serviceInstances []*model.ServiceInstance

	model.NoopAmbientIndexes
	model.NetworkGatewaysHandler
}

var _ model.ServiceDiscovery = &localServiceDiscovery{}

func (l *localServiceDiscovery) Services() []*model.Service {
	return l.services
}

func (l *localServiceDiscovery) GetService(host.Name) *model.Service {
	panic("implement me")
}

func (l *localServiceDiscovery) GetProxyServiceTargets(*model.Proxy) []model.ServiceTarget {
	var svcTS []model.ServiceTarget
	for _, svc := range l.services {
		var svcT model.ServiceTarget
		svcT.Service = svc
		svcTS = append(svcTS, svcT)
	}
	return svcTS
}

func (l *localServiceDiscovery) GetProxyWorkloadLabels(*model.Proxy) labels.Instance {
	panic("implement me")
}

func (l *localServiceDiscovery) GetIstioServiceAccounts(*model.Service) []string {
	return nil
}

func (l *localServiceDiscovery) NetworkGateways() []model.NetworkGateway {
	// TODO implement fromRegistry logic from kube controller if needed
	return nil
}

func (l *localServiceDiscovery) MCSServices() []model.MCSServiceInfo {
	return nil
}

func TestPopulateFailoverPriorityLabels(t *testing.T) {
	tests := []struct {
		name           string
		dr             *config.Config
		mesh           *meshconfig.MeshConfig
		expectedLabels []byte
	}{
		{
			name:           "no dr",
			expectedLabels: nil,
		},
		{
			name: "simple",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
						LoadBalancer: &networking.LoadBalancerSettings{
							LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
								FailoverPriority: []string{
									"a",
									"b",
								},
							},
						},
					},
				},
			},
			expectedLabels: []byte("a:a b:b "),
		},
		{
			name: "no outlier detection",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
								FailoverPriority: []string{
									"a",
									"b",
								},
							},
						},
					},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "no failover priority",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
					},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "failover priority disabled",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
						LoadBalancer: &networking.LoadBalancerSettings{
							LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
								FailoverPriority: []string{
									"a",
									"b",
								},
								Enabled: &wrapperspb.BoolValue{Value: false},
							},
						},
					},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "mesh LocalityLoadBalancerSetting",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
					},
				},
			},
			mesh: &meshconfig.MeshConfig{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					FailoverPriority: []string{
						"a",
						"b",
					},
				},
			},
			expectedLabels: []byte("a:a b:b "),
		},
		{
			name: "mesh LocalityLoadBalancerSetting(no outlier detection)",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{},
				},
			},
			mesh: &meshconfig.MeshConfig{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					FailoverPriority: []string{
						"a",
						"b",
					},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "mesh LocalityLoadBalancerSetting(no failover priority)",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
					},
				},
			},
			mesh: &meshconfig.MeshConfig{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{},
			},
			expectedLabels: nil,
		},
		{
			name: "mesh LocalityLoadBalancerSetting(failover priority disabled)",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
					},
				},
			},
			mesh: &meshconfig.MeshConfig{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					FailoverPriority: []string{
						"a",
						"b",
					},
					Enabled: &wrapperspb.BoolValue{Value: false},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "both dr and mesh LocalityLoadBalancerSetting",
			dr: &config.Config{
				Spec: &networking.DestinationRule{
					TrafficPolicy: &networking.TrafficPolicy{
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 5,
						},
						LoadBalancer: &networking.LoadBalancerSettings{
							LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
								FailoverPriority: []string{
									"a",
									"b",
								},
							},
						},
					},
				},
			},
			mesh: &meshconfig.MeshConfig{
				LocalityLbSetting: &networking.LocalityLoadBalancerSetting{
					FailoverPriority: []string{
						"c",
					},
				},
			},
			expectedLabels: []byte("a:a b:b "),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := EndpointBuilder{
				proxy: &model.Proxy{
					Metadata: &model.NodeMetadata{},
					Labels: map[string]string{
						"app": "foo",
						"a":   "a",
						"b":   "b",
					},
				},
				push: &model.PushContext{
					Mesh: tt.mesh,
				},
			}
			if tt.dr != nil {
				b.destinationRule = model.ConvertConsolidatedDestRule(tt.dr, nil)
			}
			b.populateFailoverPriorityLabels()
			if !reflect.DeepEqual(b.failoverPriorityLabels, tt.expectedLabels) {
				t.Fatalf("expected priorityLabels %v but got %v", tt.expectedLabels, b.failoverPriorityLabels)
			}
		})
	}
}

func TestFilterIstioEndpoint(t *testing.T) {
	servicePort := &model.Port{
		Name:     "default",
		Port:     80,
		Protocol: protocol.HTTP,
	}
	svc := &model.Service{
		Hostname: "example.ns.svc.cluster.local",
		Attributes: model.ServiceAttributes{
			Name:      "example",
			Namespace: "ns",
			K8sAttributes: model.K8sAttributes{
				NodeLocal: false,
			},
		},
		Resolution: model.DNSLB,
		Ports:      model.PortList{{Port: 80, Protocol: protocol.HTTP, Name: "http"}},
	}
	proxy := &model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"111.111.111.111", "1111:2222::1"},
		ID:          "v0.default",
		DNSDomain:   "example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
			NodeName:  "example",
		},
		ConfigNamespace: "not-default",
	}
	ep0 := &model.IstioEndpoint{
		Addresses:       []string{"1.1.1.1"},
		NodeName:        "example",
		ServicePortName: "not-default",
	}
	ep1 := &model.IstioEndpoint{
		Addresses:       []string{"1.1.1.1"},
		NodeName:        "example",
		ServicePortName: "default",
	}
	ep2 := &model.IstioEndpoint{
		Addresses:       []string{"2001:1::1"},
		NodeName:        "example",
		ServicePortName: "default",
	}
	ep3 := &model.IstioEndpoint{
		Addresses:       []string{"1.1.1.1", "2001:1::1"},
		NodeName:        "example",
		ServicePortName: "default",
	}
	ep4 := &model.IstioEndpoint{
		Addresses:       []string{},
		NodeName:        "example",
		ServicePortName: "default",
	}

	tests := []struct {
		name     string
		ep       *model.IstioEndpoint
		p        *model.Port
		expected bool
	}{
		{
			name:     "test endpoint with different service port name",
			ep:       ep0,
			p:        servicePort,
			expected: true,
		},
		{
			name:     "test endpoint with ipv4 address",
			ep:       ep1,
			p:        servicePort,
			expected: true,
		},
		{
			name:     "test endpoint with ipv6 address",
			ep:       ep2,
			p:        servicePort,
			expected: true,
		},
		{
			name:     "test endpoint with both ipv4 and ipv6 addresses",
			ep:       ep3,
			p:        servicePort,
			expected: true,
		},
		{
			name:     "test endpoint without address",
			ep:       ep4,
			p:        servicePort,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := model.NewEnvironment()
			env.ConfigStore = model.NewFakeStore()
			env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})
			meshNetworks := meshwatcher.NewFixedNetworksWatcher(nil)
			env.NetworksWatcher = meshNetworks
			env.ServiceDiscovery = memory.NewServiceDiscovery()
			xdsUpdater := xdsfake.NewFakeXDS()
			if err := env.InitNetworksManager(xdsUpdater); err != nil {
				t.Fatal(err)
			}
			env.ServiceDiscovery = &localServiceDiscovery{
				services: []*model.Service{svc},
				serviceInstances: []*model.ServiceInstance{{
					Endpoint: tt.ep,
				}},
			}
			env.Init()

			// Init a new push context
			push := model.NewPushContext()
			push.InitContext(env, nil, nil)
			env.SetPushContext(push)
			if push.NetworkManager() == nil {
				t.Fatal("error: NetworkManager should not be nil!")
			}

			builder := NewCDSEndpointBuilder(
				proxy, push,
				"outbound||example.ns.svc.cluster.local",
				model.TrafficDirectionOutbound, "", "example.ns.svc.cluster.local", 80,
				svc, nil)
			expected := builder.filterIstioEndpoint(tt.ep)
			if !reflect.DeepEqual(tt.expected, expected) {
				t.Fatalf("expected  %v but got %v", tt.expected, expected)
			}
		})
	}
}
