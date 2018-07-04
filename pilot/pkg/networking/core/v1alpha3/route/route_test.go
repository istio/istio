// Copyright 2018 Istio Authors
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

package route_test

import (
	"testing"
	"time"

	envoyroute "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
)

func TestBuildHTTPRoutes(t *testing.T) {
	virtualService := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:    model.VirtualService.Type,
			Version: model.VirtualService.Version,
			Name:    "acme",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"wotan"},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.DestinationWeight{
						{
							Destination: &networking.Destination{
								Host: "*.example.org",
								Port: &networking.PortSelector{
									Port: &networking.PortSelector_Name{
										Name: "foo",
									},
								},
							},
							Weight: 100,
						},
					},
				},
			},
		},
	}

	serviceRegistry := map[model.Hostname]*model.Service{
		"*.example.org": {
			Hostname:    "*.example.org",
			Address:     "1.1.1.1",
			ClusterVIPs: make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "default",
					Port:     8080,
					Protocol: model.ProtocolHTTP,
				},
			},
		},
	}

	t.Run("ForVirtualService", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		gatewayNames := map[string]bool{"wotan": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("ForVirtualServiceWithRingHash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		ttl := time.Duration(time.Nanosecond * 100)
		configStore := &fakes.IstioConfigStore{}
		configStore.DestinationRuleReturns(
			&model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:    model.DestinationRule.Type,
					Version: model.DestinationRule.Version,
					Name:    "acme",
				},
				Spec: &networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
								ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
									HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
										HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
											Name: "hash-cookie",
											Ttl:  &ttl,
										},
									},
								},
							},
						},
					},
				},
			},
		)

		gatewayNames := map[string]bool{"wotan": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames, configStore)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie",
					Ttl:  &ttl,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})

	t.Run("ForVirtualServiceWithSubsetsWithRingHash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		virtualService := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		configStore := &fakes.IstioConfigStore{}
		configStore.DestinationRuleReturns(
			&model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:    model.DestinationRule.Type,
					Version: model.DestinationRule.Version,
					Name:    "acme",
				},
				Spec: &networking.DestinationRule{
					Host:    "*.example.org",
					Subsets: []*networking.Subset{networkingSubset},
				},
			})

		gatewayNames := map[string]bool{"wotan": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames, configStore)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "other-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})

	t.Run("ForVirtualServiceWithSubsetsAndTopLevelTrafficPolicyWithRingHash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		virtualService := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		configStore := &fakes.IstioConfigStore{}
		cnfg := &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.DestinationRule.Type,
				Version: model.DestinationRule.Version,
				Name:    "acme",
			},
		}
		rule := networkingDestinationRule
		rule.Subsets = []*networking.Subset{networkingSubset}
		cnfg.Spec = networkingDestinationRule
		configStore.DestinationRuleReturns(cnfg)

		gatewayNames := map[string]bool{"wotan": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames, configStore)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "other-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})

	t.Run("Port Selector Based Traffic Policy", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		configStore := &fakes.IstioConfigStore{}
		cnfg := &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.DestinationRule.Type,
				Version: model.DestinationRule.Version,
				Name:    "acme",
			},
			Spec: portLevelDestinationRule,
		}
		configStore.DestinationRuleReturns(cnfg)

		gatewayNames := map[string]bool{"wotan": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames, configStore)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})
}

var virtualServiceWithSubset = &networking.VirtualService{
	Hosts:    []string{},
	Gateways: []string{"wotan"},
	Http: []*networking.HTTPRoute{
		{
			Route: []*networking.DestinationWeight{
				{
					Destination: &networking.Destination{
						Subset: "some-subset",
						Host:   "*.example.org",
						Port: &networking.PortSelector{
							Port: &networking.PortSelector_Number{
								Number: 65000,
							},
						},
					},
					Weight: 100,
				},
			},
		},
	},
}

var portLevelDestinationRule = &networking.DestinationRule{
	Host:    "*.example.org",
	Subsets: []*networking.Subset{},
	TrafficPolicy: &networking.TrafficPolicy{
		PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
			{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: loadBalancerPolicy,
				},
				Port: &networking.PortSelector{
					Port: &networking.PortSelector_Name{
						Name: "foo",
					},
				},
			},
		},
	},
}

var loadBalancerPolicy = &networking.LoadBalancerSettings_ConsistentHash{
	ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
		HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
			HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
				Name: "hash-cookie",
			},
		},
	},
}

var networkingDestinationRule = &networking.DestinationRule{
	Host:    "*.example.org",
	Subsets: []*networking.Subset{},
	TrafficPolicy: &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			LbPolicy: loadBalancerPolicy,
		},
	},
}

var networkingSubset = &networking.Subset{
	Name:   "some-subset",
	Labels: map[string]string{},
	TrafficPolicy: &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
				ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
					HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
						HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
							Name: "other-cookie",
						},
					},
				},
			},
		},
	},
}
