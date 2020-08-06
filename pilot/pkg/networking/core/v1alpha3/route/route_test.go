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

package route_test

import (
	"os"
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/onsi/gomega"

	"istio.io/istio/pkg/util/gogo"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestBuildHTTPRoutes(t *testing.T) {
	serviceRegistry := map[host.Name]*model.Service{
		"*.example.org": {
			Hostname:    "*.example.org",
			Address:     "1.1.1.1",
			ClusterVIPs: make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "default",
					Port:     8080,
					Protocol: protocol.HTTP,
				},
			},
		},
	}

	node := &model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "someID",
		DNSDomain:   "foo.com",
		Metadata:    &model.NodeMetadata{},
	}
	node16 := &model.Proxy{
		Type:         model.SidecarProxy,
		IPAddresses:  []string{"1.1.1.1"},
		ID:           "someID",
		DNSDomain:    "foo.com",
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 6},
	}

	gatewayNames := map[string]bool{"some-gateway": true}

	t.Run("for virtual service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		os.Setenv("ISTIO_DEFAULT_REQUEST_TIMEOUT", "0ms")
		defer os.Unsetenv("ISTIO_DEFAULT_REQUEST_TIMEOUT")

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServicePlain, serviceRegistry, 8080, gatewayNames)

		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		// Validate that when timeout is not specified, we disable it based on default value of flag.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(0)))
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(gomega.Equal(int64(0)))
	})

	t.Run("for virtual service with changed default timeout", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		dt := features.DefaultRequestTimeout
		features.DefaultRequestTimeout = ptypes.DurationProto(1 * time.Second)
		defer func() { features.DefaultRequestTimeout = dt }()

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServicePlain, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		// Validate that when timeout is not specified, we send what is set in the timeout flag.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(1)))
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(gomega.Equal(int64(1)))
	})

	t.Run("for virtual service with timeout", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithTimeout, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		// Validate that when timeout specified, we send the configured timeout to Envoys.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(10)))
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(gomega.Equal(int64(10)))
	})

	t.Run("for virtual service with disabled timeout", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithTimeoutDisabled, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(0)))
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(gomega.Equal(int64(0)))
	})

	t.Run("for virtual service with catch all route", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithCatchAllRoute, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("for virtual service with top level catch all route", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithCatchAllRouteWeightedDestination, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("for virtual service with multi prefix catch all route", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithCatchAllMultiPrefixRoute, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("for virtual service with regex matching on URI", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithRegexMatchingOnURI, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetSafeRegex().GetRegex()).To(gomega.Equal("\\/(.?)\\/status"))
		// nolint: staticcheck
		g.Expect(routes[0].GetMatch().GetSafeRegex().GetGoogleRe2().GetMaxProgramSize().GetValue()).To(gomega.Equal(uint32(1024)))
	})

	t.Run("for virtual service with regex matching on URI with 1.6 proxy", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node16, nil, virtualServiceWithRegexMatchingOnURI, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetSafeRegex().GetRegex()).To(gomega.Equal("\\/(.?)\\/status"))
		// nolint: staticcheck
		g.Expect(routes[0].GetMatch().GetSafeRegex().GetGoogleRe2().GetMaxProgramSize().GetValue()).To(gomega.Equal(uint32(1024)))
	})

	t.Run("for virtual service with regex matching on header", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithRegexMatchingOnHeader, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetSafeRegexMatch().GetRegex()).To(gomega.Equal("Bearer .+?\\..+?\\..+?"))
	})

	t.Run("for virtual service with regex matching on without_header", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithRegexMatchingOnWithoutHeader, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetSafeRegexMatch().GetRegex()).To(gomega.Equal("BAR .+?\\..+?\\..+?"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(true))
	})

	t.Run("for virtual service with presence matching on header", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithPresentMatchingOnHeader, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(gomega.Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(gomega.Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(false))
	})

	t.Run("for virtual service with presence matching on header and without_header", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithPresentMatchingOnWithoutHeader, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(gomega.Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(gomega.Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(true))
	})

	t.Run("for virtual service with regex matching for all cases on header", func(t *testing.T) {

		cset := createVirtualServiceWithRegexMatchingForAllCasesOnHeader()

		for _, c := range cset {
			g := gomega.NewGomegaWithT(t)
			routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, *c, serviceRegistry, 8080, gatewayNames)
			// Valiate routes.
			for _, r := range routes {
				if err := r.Validate(); err != nil {
					t.Fatalf("Route %s validation failed with error %v", r.Name, err)
				}
			}
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(len(routes)).To(gomega.Equal(1))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(gomega.Equal("FOO-HEADER"))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(gomega.Equal(true))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(false))
		}
	})

	t.Run("for virtual service with source namespace matching", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		fooNode := *node
		fooNode.Metadata = &model.NodeMetadata{
			Namespace: "foo",
		}
		barNode := *node
		barNode.Metadata = &model.NodeMetadata{
			Namespace: "bar",
		}

		routes, err := route.BuildHTTPRoutesForVirtualService(&fooNode, nil, virtualServiceMatchingOnSourceNamespace, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetName()).To(gomega.Equal("foo"))

		routes, err = route.BuildHTTPRoutesForVirtualService(&barNode, nil, virtualServiceMatchingOnSourceNamespace, serviceRegistry, 8080, gatewayNames)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetName()).To(gomega.Equal("bar"))
	})

	t.Run("for virtual service with ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		ttl := types.Duration{Nanos: 100}
		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
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

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualServicePlain, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie",
					Ttl:  gogo.DurationToProtoDuration(&ttl),
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})

	t.Run("for virtual service with query param based ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
				},
				Spec: &networking.DestinationRule{
					Host: "*.example.org",
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
								ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
									HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpQueryParameterName{
										HttpQueryParameterName: "query",
									},
								},
							},
						},
					},
				},
			},
		})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualServicePlain, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_QueryParameter_{
				QueryParameter: &envoyroute.RouteAction_HashPolicy_QueryParameter{
					Name: "query",
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(gomega.ConsistOf(hashPolicy))
	})

	t.Run("for virtual service with subsets with ring hash", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		virtualService := model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
				Name:             "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
				},
				Spec: &networking.DestinationRule{
					Host:    "*.example.org",
					Subsets: []*networking.Subset{networkingSubset},
				},
			},
		})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
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

		virtualService := model.Config{ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme",
		},
			Spec: virtualServiceWithSubsetWithPortLevelSettings,
		}

		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}

		push.SetDestinationRules([]model.Config{
			{

				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
				},
				Spec: portLevelDestinationRuleWithSubsetPolicy,
			}})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
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
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
				Name:             "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		cnfg := model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
				Name:             "acme",
			},
		}
		rule := networkingDestinationRule
		rule.Subsets = []*networking.Subset{networkingSubset}
		cnfg.Spec = networkingDestinationRule

		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}

		push.SetDestinationRules([]model.Config{
			cnfg})

		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualService, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
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

		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}

		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
				},
				Spec: portLevelDestinationRule,
			}})

		gatewayNames := map[string]bool{"some-gateway": true}
		routes, err := route.BuildHTTPRoutesForVirtualService(node, push, virtualServicePlain, serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
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

	t.Run("for header operations", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithHeaderOperations,
			serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		r := routes[0]
		g.Expect(len(r.RequestHeadersToAdd)).To(gomega.Equal(4))
		g.Expect(len(r.ResponseHeadersToAdd)).To(gomega.Equal(4))
		g.Expect(len(r.RequestHeadersToRemove)).To(gomega.Equal(2))
		g.Expect(len(r.ResponseHeadersToRemove)).To(gomega.Equal(2))

		g.Expect(r.RequestHeadersToAdd).To(gomega.Equal([]*core.HeaderValueOption{
			{
				Header: &core.HeaderValue{
					Key:   "x-req-set",
					Value: "v1",
				},
				Append: &wrappers.BoolValue{Value: false},
			},
			{
				Header: &core.HeaderValue{
					Key:   "x-req-add",
					Value: "v2",
				},
				Append: &wrappers.BoolValue{Value: true},
			},
			{
				Header: &core.HeaderValue{
					Key:   "x-route-req-set",
					Value: "v1",
				},
				Append: &wrappers.BoolValue{Value: false},
			},
			{
				Header: &core.HeaderValue{
					Key:   "x-route-req-add",
					Value: "v2",
				},
				Append: &wrappers.BoolValue{Value: true},
			},
		}))
		g.Expect(r.RequestHeadersToRemove).To(gomega.Equal([]string{"x-req-remove", "x-route-req-remove"}))

		g.Expect(r.ResponseHeadersToAdd).To(gomega.Equal([]*core.HeaderValueOption{
			{
				Header: &core.HeaderValue{
					Key:   "x-resp-set",
					Value: "v1",
				},
				Append: &wrappers.BoolValue{Value: false},
			},
			{
				Header: &core.HeaderValue{
					Key:   "x-resp-add",
					Value: "v2",
				},
				Append: &wrappers.BoolValue{Value: true},
			},
			{
				Header: &core.HeaderValue{
					Key:   "x-route-resp-set",
					Value: "v1",
				},
				Append: &wrappers.BoolValue{Value: false},
			},
			{
				Header: &core.HeaderValue{
					Key:   "x-route-resp-add",
					Value: "v2",
				},
				Append: &wrappers.BoolValue{Value: true},
			},
		}))
		g.Expect(r.ResponseHeadersToRemove).To(gomega.Equal([]string{"x-resp-remove", "x-route-resp-remove"}))
	})

	t.Run("for redirect code", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithRedirect,
			serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(gomega.BeFalse())
		g.Expect(redirectAction.Redirect.ResponseCode).To(gomega.Equal(envoyroute.RedirectAction_PERMANENT_REDIRECT))
	})

	t.Run("for redirect and header manipulation", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		routes, err := route.BuildHTTPRoutesForVirtualService(node, nil, virtualServiceWithRedirectAndSetHeader,
			serviceRegistry, 8080, gatewayNames)
		// Valiate routes.
		for _, r := range routes {
			if err := r.Validate(); err != nil {
				t.Fatalf("Route %s validation failed with error %v", r.Name, err)
			}
		}
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(gomega.BeFalse())
		g.Expect(redirectAction.Redirect.ResponseCode).To(gomega.Equal(envoyroute.RedirectAction_PERMANENT_REDIRECT))
		g.Expect(len(routes[0].ResponseHeadersToAdd)).To(gomega.Equal(1))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Append.Value).To(gomega.BeFalse())
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Key).To(gomega.Equal("Strict-Transport-Security"))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Value).To(gomega.Equal("max-age=31536000; includeSubDomains; preload"))

	})

	t.Run("for no virtualservice but has destinationrule with consistentHash loadbalancer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
				},
				Spec: networkingDestinationRule,
			}})
		vhosts := route.BuildSidecarVirtualHostsFromConfigAndRegistry(node, push, serviceRegistry, []model.Config{}, 8080)
		g.Expect(vhosts[0].Routes[0].Action.(*envoyroute.Route_Route).Route.HashPolicy).NotTo(gomega.BeNil())
	})
	t.Run("for no virtualservice but has destinationrule with portLevel consistentHash loadbalancer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		meshConfig := mesh.DefaultMeshConfig()
		push := &model.PushContext{
			Mesh: &meshConfig,
		}
		push.SetDestinationRules([]model.Config{
			{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
					Name:             "acme",
				},
				Spec: networkingDestinationRuleWithPortLevelTrafficPolicy,
			}})

		vhosts := route.BuildSidecarVirtualHostsFromConfigAndRegistry(node, push, serviceRegistry, []model.Config{}, 8080)

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie-1",
				},
			},
		}
		g.Expect(vhosts[0].Routes[0].Action.(*envoyroute.Route_Route).Route.HashPolicy).To(gomega.ConsistOf(hashPolicy))
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
			Route: []*networking.HTTPRouteDestination{
				{
					Destination: &networking.Destination{
						Subset: "some-subset",
						Host:   "*.example.org",
						Port: &networking.PortSelector{
							Number: 65000,
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
			Route: []*networking.HTTPRouteDestination{
				{
					Destination: &networking.Destination{
						Subset: "port-level-settings-subset",
						Host:   "*.example.org",
						Port: &networking.PortSelector{
							Number: 8484,
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
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithTimeout = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
				Timeout: &types.Duration{
					Seconds: 10,
				},
			},
		},
	},
}

var virtualServiceWithTimeoutDisabled = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
				Timeout: &types.Duration{
					Seconds: 0,
				},
			},
		},
	},
}

var virtualServiceWithCatchAllRoute = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "non-catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/route/v1",
							},
						},
					},
					{
						Name: "catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithCatchAllMultiPrefixRoute = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/",
							},
						},
						SourceLabels: map[string]string{
							"matchingNoSrc": "xxx",
						},
					},
					{
						Name: "specific match",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/a",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithCatchAllRouteWeightedDestination = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{"headers.test.istio.io"},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "headers-only",
						Headers: map[string]*networking.StringMatch{
							"version": {
								MatchType: &networking.StringMatch_Exact{
									Exact: "v2",
								},
							},
						},
						SourceLabels: map[string]string{
							"version": "v1",
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host:   "c-weighted.extsvc.com",
							Subset: "v2",
						},
						Weight: 100,
					},
				},
			},
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host:   "c-weighted.extsvc.com",
							Subset: "v1",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithHeaderOperations = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{"headers.test.istio.io"},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host:   "c-weighted.extsvc.com",
							Subset: "v1",
						},
						Headers: &networking.Headers{
							Request: &networking.Headers_HeaderOperations{
								Set:    map[string]string{"x-route-req-set": "v1"},
								Add:    map[string]string{"x-route-req-add": "v2"},
								Remove: []string{"x-route-req-remove"},
							},
							Response: &networking.Headers_HeaderOperations{
								Set:    map[string]string{"x-route-resp-set": "v1"},
								Add:    map[string]string{"x-route-resp-add": "v2"},
								Remove: []string{"x-route-resp-remove"},
							},
						},
						Weight: 100,
					},
				},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Set:    map[string]string{"x-req-set": "v1"},
						Add:    map[string]string{"x-req-add": "v2"},
						Remove: []string{"x-req-remove"},
					},
					Response: &networking.Headers_HeaderOperations{
						Set:    map[string]string{"x-resp-set": "v1"},
						Add:    map[string]string{"x-resp-add": "v2"},
						Remove: []string{"x-resp-remove"},
					},
				},
			},
		},
	},
}

var virtualServiceWithRedirect = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithRedirectAndSetHeader = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
						},
					},
				},
			},
		},
	},
}

var virtualServiceWithRegexMatchingOnURI = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "status",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Regex{
								Regex: "\\/(.?)\\/status",
							},
						},
					},
				},
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithRegexMatchingOnHeader = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "auth",
						Headers: map[string]*networking.StringMatch{
							"Authentication": {
								MatchType: &networking.StringMatch_Regex{
									Regex: "Bearer .+?\\..+?\\..+?",
								},
							},
						},
					},
				},
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

func createVirtualServiceWithRegexMatchingForAllCasesOnHeader() []*model.Config {
	ret := []*model.Config{}
	regex := "*"
	ret = append(ret, &model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"some-gateway"},
			Http: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Name: "presence",
							Headers: map[string]*networking.StringMatch{
								"FOO-HEADER": {
									MatchType: &networking.StringMatch_Regex{
										Regex: regex,
									},
								},
							},
						},
					},
					Redirect: &networking.HTTPRedirect{
						Uri:          "example.org",
						Authority:    "some-authority.default.svc.cluster.local",
						RedirectCode: 308,
					},
				},
			},
		},
	})

	return ret
}

var virtualServiceWithRegexMatchingOnWithoutHeader = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "without-test",
						WithoutHeaders: map[string]*networking.StringMatch{
							"FOO-HEADER": {
								MatchType: &networking.StringMatch_Regex{
									Regex: "BAR .+?\\..+?\\..+?",
								},
							},
						},
					},
				},
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithPresentMatchingOnHeader = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "presence",
						Headers: map[string]*networking.StringMatch{
							"FOO-HEADER": nil,
						},
					},
				},
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithPresentMatchingOnWithoutHeader = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "presence",
						WithoutHeaders: map[string]*networking.StringMatch{
							"FOO-HEADER": nil,
						},
					},
				},
				Redirect: &networking.HTTPRedirect{
					Uri:          "example.org",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceMatchingOnSourceNamespace = model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts: []string{},
		Http: []*networking.HTTPRoute{
			{
				Name: "foo",
				Match: []*networking.HTTPMatchRequest{
					{
						SourceNamespace: "foo",
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			{
				Name: "bar",
				Match: []*networking.HTTPMatchRequest{
					{
						SourceNamespace: "bar",
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "bar.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
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
					Number: 8484,
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
					Number: 8484,
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
var networkingDestinationRuleWithPortLevelTrafficPolicy = &networking.DestinationRule{
	Host: "*.example.org",
	TrafficPolicy: &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			LbPolicy: loadBalancerPolicy("hash-cookie"),
		},
		PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
			{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: loadBalancerPolicy("hash-cookie-1"),
				},
				Port: &networking.PortSelector{
					Number: 8080,
				},
			},
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
					Number: 8484,
				},
			},
		},
	},
}

func TestCombineVHostRoutes(t *testing.T) {
	// nolint: staticcheck
	regexEngine := &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{
		MaxProgramSize: &wrappers.UInt32Value{
			Value: uint32(10),
		},
	}}
	first := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: regexEngine,
				Regex:      ".*?regex1",
			},
		}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
	}
	second := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: regexEngine,
				Regex:      ".*?regex12",
			},
		}}},
		{Match: &envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
				SafeRegex: &matcher.RegexMatcher{
					EngineType: regexEngine,
					Regex:      "*",
				},
			},
			Headers: []*envoyroute.HeaderMatcher{
				{
					Name:                 "foo",
					HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{ExactMatch: "bar"},
					InvertMatch:          false,
				},
			},
		}},
	}

	want := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: regexEngine,
				Regex:      ".*?regex1",
			},
		}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: regexEngine,
				Regex:      ".*?regex12",
			},
		}}},
		{Match: &envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
				SafeRegex: &matcher.RegexMatcher{
					EngineType: regexEngine,
					Regex:      "*",
				},
			},
			Headers: []*envoyroute.HeaderMatcher{
				{
					Name:                 "foo",
					HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{ExactMatch: "bar"},
					InvertMatch:          false,
				},
			},
		}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
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
