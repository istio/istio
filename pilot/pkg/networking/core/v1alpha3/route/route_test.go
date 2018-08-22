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
	"reflect"
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

	node := &model.Proxy{
		Type:      model.Sidecar,
		IPAddress: "1.1.1.1",
		ID:        "someID",
		Domain:    "foo.com",
		Metadata:  map[string]string{"ISTIO_PROXY_VERSION": "1.0"},
	}
	gatewayNames := map[string]bool{"some-gateway": true}

	t.Run("for virtual service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServicePlain, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("destination rule nil traffic policy", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		configStore := &fakes.IstioConfigStore{}
		rule := networkingDestinationRule
		rule.TrafficPolicy = nil
		cnfg := &model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.DestinationRule.Type,
				Version: model.DestinationRule.Version,
				Name:    "acme",
			},
			Spec: rule,
		}

		configStore.DestinationRuleReturns(cnfg)

		_, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServicePlain, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("for virtual service with ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		ttl := time.Duration(time.Nanosecond * 100)
		push := &model.PushContext{}
		push.SetDestinationRules([]model.Config{
			{
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
		})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualServicePlain, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
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

	t.Run("for virtual service with subsets with ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		virtualService := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		push := &model.PushContext{}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					Type:    model.DestinationRule.Type,
					Version: model.DestinationRule.Version,
					Name:    "acme",
				},
				Spec: &networking.DestinationRule{
					Host:    "*.example.org",
					Subsets: []*networking.Subset{networkingSubset},
				},
			},
		})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
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

	t.Run("for virtual service with subsets with port level settings with ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		virtualService := model.Config{ConfigMeta: model.ConfigMeta{Type: model.VirtualService.Type,
			Version: model.VirtualService.Version,
			Name:    "acme",
		},
			Spec: virtualServiceWithSubsetWithPortLevelSettings,
		}

		push := &model.PushContext{}
		push.SetDestinationRules([]model.Config{
			{

				ConfigMeta: model.ConfigMeta{
					Type:    model.DestinationRule.Type,
					Version: model.DestinationRule.Version,
					Name:    "acme",
				},
				Spec: portLevelDestinationRuleWithSubsetPolicy,
			}})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "port-level-settings-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})

	t.Run("for virtual service with subsets and top level traffic policy with ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		virtualService := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.VirtualService.Type,
				Version: model.VirtualService.Version,
				Name:    "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		cnfg := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:    model.DestinationRule.Type,
				Version: model.DestinationRule.Version,
				Name:    "acme",
			},
		}
		rule := networkingDestinationRule
		rule.Subsets = []*networking.Subset{networkingSubset}
		cnfg.Spec = networkingDestinationRule

		push := &model.PushContext{}
		push.SetDestinationRules([]model.Config{
			cnfg})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
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

	t.Run("port selector based traffic policy", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		push := &model.PushContext{}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					Type:    model.DestinationRule.Type,
					Version: model.DestinationRule.Version,
					Name:    "acme",
				},
				Spec: portLevelDestinationRule,
			}})

		gatewayNames := map[string]bool{"some-gateway": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualServicePlain, serviceRegistry, 8080, model.LabelsCollection{}, gatewayNames)
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

func loadBalancerPolicy(name string) *networking.LoadBalancerSettings_ConsistentHash {
	return &networking.LoadBalancerSettings_ConsistentHash{
		ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
			HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
				HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
					Name: name,
				},
			},
		},
	}
}

var virtualServiceWithSubset = &networking.VirtualService{
	Hosts:    []string{},
	Gateways: []string{"some-gateway"},
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

var virtualServiceWithSubsetWithPortLevelSettings = &networking.VirtualService{
	Hosts:    []string{},
	Gateways: []string{"some-gateway"},
	Http: []*networking.HTTPRoute{
		{
			Route: []*networking.DestinationWeight{
				{
					Destination: &networking.Destination{
						Subset: "port-level-settings-subset",
						Host:   "*.example.org",
						Port: &networking.PortSelector{
							Port: &networking.PortSelector_Number{
								Number: 8484,
							},
						},
					},
					Weight: 100,
				},
			},
		},
	},
}

var virtualServicePlain = model.Config{
	ConfigMeta: model.ConfigMeta{
		Type:    model.VirtualService.Type,
		Version: model.VirtualService.Version,
		Name:    "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.DestinationWeight{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Port: &networking.PortSelector_Number{
									Number: 8484,
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

var portLevelDestinationRule = &networking.DestinationRule{
	Host:    "*.example.org",
	Subsets: []*networking.Subset{},
	TrafficPolicy: &networking.TrafficPolicy{
		PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
			{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: loadBalancerPolicy("hash-cookie"),
				},
				Port: &networking.PortSelector{
					Port: &networking.PortSelector_Number{
						Number: 8484,
					},
				},
			},
		},
	},
}

var portLevelDestinationRuleWithSubsetPolicy = &networking.DestinationRule{
	Host:    "*.example.org",
	Subsets: []*networking.Subset{networkingSubsetWithPortLevelSettings},
	TrafficPolicy: &networking.TrafficPolicy{
		PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
			{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: loadBalancerPolicy("hash-cookie"),
				},
				Port: &networking.PortSelector{
					Port: &networking.PortSelector_Number{
						Number: 8484,
					},
				},
			},
		},
	},
}

var networkingDestinationRule = &networking.DestinationRule{
	Host:    "*.example.org",
	Subsets: []*networking.Subset{},
	TrafficPolicy: &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			LbPolicy: loadBalancerPolicy("hash-cookie"),
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

var networkingSubsetWithPortLevelSettings = &networking.Subset{
	Name:   "port-level-settings-subset",
	Labels: map[string]string{},
	TrafficPolicy: &networking.TrafficPolicy{
		PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
			{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: loadBalancerPolicy("port-level-settings-cookie"),
				},
				Port: &networking.PortSelector{
					Port: &networking.PortSelector_Number{
						Number: 8484,
					},
				},
			},
		},
	},
}

func TestCombineVHostRoutes(t *testing.T) {
	first := []envoyroute.Route{
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: ".*?regex1"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
	}
	second := []envoyroute.Route{
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path12"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix12"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: ".*?regex12"}}},
		{Match: envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: "*"},
			Headers: []*envoyroute.HeaderMatcher{
				{
					Name:                 "foo",
					HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{ExactMatch: "bar"},
					InvertMatch:          false,
				},
			},
		}},
	}

	want := []envoyroute.Route{
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path12"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix12"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: ".*?regex12"}}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: ".*?regex1"}}},
		{Match: envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: "*"},
			Headers: []*envoyroute.HeaderMatcher{
				{
					Name:                 "foo",
					HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{ExactMatch: "bar"},
					InvertMatch:          false,
				},
			},
		}},
		{Match: envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
	}

	got := route.CombineVHostRoutes(first, second)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("CombineVHostRoutes: \n")
		t.Errorf("got: \n")
		for _, g := range got {
			t.Errorf("%v\n", g.Match.PathSpecifier)
		}
		t.Errorf("want: \n")
		for _, g := range want {
			t.Errorf("%v\n", g.Match.PathSpecifier)
		}
	}
}
