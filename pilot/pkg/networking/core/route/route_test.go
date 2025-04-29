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
	"log"
	"reflect"
	"testing"

	envoycore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	extproc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

func buildRouteOpts(sr map[host.Name]*model.Service, hash route.DestinationHashMap) route.RouteOptions {
	return route.RouteOptions{
		IsTLS:                     false,
		IsHTTP3AltSvcHeaderNeeded: false,
		Mesh:                      nil,
		LookupService: func(name host.Name) *model.Service {
			return sr[name]
		},
		LookupDestinationCluster: route.GetDestinationCluster,
		LookupHash: func(destination *networking.HTTPRouteDestination) *networking.LoadBalancerSettings_ConsistentHashLB {
			return hash[destination]
		},
	}
}

func TestBuildHTTPRoutes(t *testing.T) {
	serviceRegistry := map[host.Name]*model.Service{
		"*.example.org": {
			Hostname:       "*.example.org",
			DefaultAddress: "1.1.1.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "default",
					Port:     8080,
					Protocol: protocol.HTTP,
				},
			},
		},
	}
	routeOpts := buildRouteOpts(serviceRegistry, nil)

	node := func(cg *core.ConfigGenTest) *model.Proxy {
		return cg.SetupProxy(&model.Proxy{
			Type:        model.SidecarProxy,
			IPAddresses: []string{"1.1.1.1"},
			ID:          "someID",
			DNSDomain:   "foo.com",
			IstioVersion: &model.IstioVersion{
				Major: 1,
				Minor: 20,
			},
		})
	}

	nodeWithExtended := func(cg *core.ConfigGenTest) *model.Proxy {
		out := node(cg)
		out.IstioVersion.Minor = 21
		return out
	}

	gatewayNames := sets.New("some-gateway")

	t.Run("for virtual service", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		t.Setenv("ISTIO_DEFAULT_REQUEST_TIMEOUT", "0ms")

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServicePlain, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		// Validate that when timeout is not specified, we disable it based on default value of flag.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(Equal(int64(0)))
		// nolint: staticcheck
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(Equal(int64(0)))
	})

	t.Run("for virtual service with HTTP/3 discovery enabled", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routeOpts := buildRouteOpts(serviceRegistry, nil)
		routeOpts.IsHTTP3AltSvcHeaderNeeded = true
		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServicePlain, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(routes[0].GetResponseHeadersToAdd()).To(Equal([]*envoycore.HeaderValueOption{
			{
				Header: &envoycore.HeaderValue{
					Key:   util.AltSvcHeader,
					Value: `h3=":8080"; ma=86400`,
				},
				AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
		}))
	})

	t.Run("for virtual service with timeout", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithTimeout, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		// Validate that when timeout specified, we send the configured timeout to Envoys.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(Equal(int64(10)))
		// nolint: staticcheck
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(Equal(int64(10)))
	})

	t.Run("for virtual service with disabled timeout", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithTimeoutDisabled, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(Equal(int64(0)))
		// nolint: staticcheck
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(Equal(int64(0)))
	})

	t.Run("for virtual service with catch all route", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})
		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllRoute,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(2))
		g.Expect(routes[0].Name).To(Equal("route.non-catch-all"))
		g.Expect(routes[1].Name).To(Equal("route.catch-all"))
	})

	t.Run("for virtual service with catch all route：port match", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})
		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllPort,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].Name).To(Equal("route 1.catch-all for 8080"))
	})

	t.Run("for internally generated virtual service with ingress semantics", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		vs := virtualServiceWithCatchAllRoute
		if vs.Annotations == nil {
			vs.Annotations = make(map[string]string)
		}
		vs.Annotations[constants.InternalRouteSemantics] = constants.RouteSemanticsIngress

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), vs,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(routes[0].Match.PathSpecifier).To(Equal(&envoyroute.RouteMatch_PathSeparatedPrefix{
			PathSeparatedPrefix: "/route/v1",
		}))
		g.Expect(routes[1].Match.PathSpecifier).To(Equal(&envoyroute.RouteMatch_Prefix{
			Prefix: "/",
		}))
	})

	t.Run("for internally generated virtual service with gateway semantics", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		vs := virtualServiceWithCatchAllRoute
		if vs.Annotations == nil {
			vs.Annotations = make(map[string]string)
		}
		vs.Annotations[constants.InternalRouteSemantics] = constants.RouteSemanticsGateway

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), vs,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(routes[0].Match.PathSpecifier).To(Equal(&envoyroute.RouteMatch_PathSeparatedPrefix{
			PathSeparatedPrefix: "/route/v1",
		}))
		g.Expect(routes[0].Action.(*envoyroute.Route_Route).Route.ClusterNotFoundResponseCode).
			To(Equal(envoyroute.RouteAction_INTERNAL_SERVER_ERROR))
		g.Expect(routes[1].Match.PathSpecifier).To(Equal(&envoyroute.RouteMatch_Prefix{
			Prefix: "/",
		}))
		g.Expect(routes[1].Action.(*envoyroute.Route_Route).Route.ClusterNotFoundResponseCode).
			To(Equal(envoyroute.RouteAction_INTERNAL_SERVER_ERROR))
	})

	t.Run("for virtual service with top level catch all route", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllRouteWeightedDestination,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
	})

	t.Run("for virtual service with multi prefix catch all route", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllMultiPrefixRoute,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
	})

	t.Run("for virtual service with regex matching on URI", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnURI,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetSafeRegex().GetRegex()).To(Equal("\\/(.?)\\/status"))
	})

	t.Run("for virtual service with stat_prefix set for a match on URI", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithStatPrefix,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(3))
		g.Expect(routes[0].GetMatch().GetPrefix()).To(Equal("/foo"))
		g.Expect(routes[0].StatPrefix).To(Equal("foo"))
		g.Expect(routes[1].GetMatch().GetPrefix()).To(Equal("/baz"))
		g.Expect(routes[1].StatPrefix).To(Equal(""))
		g.Expect(routes[2].GetMatch().GetPrefix()).To(Equal("/bar"))
		g.Expect(routes[2].StatPrefix).To(Equal(""))
		g.Expect(len(routes[0].GetRoute().GetRetryPolicy().RetryHostPredicate)).To(Equal(1))
	})

	t.Run("for virtual service with exact matching on JWT claims with extended", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(nodeWithExtended(cg), virtualServiceWithExactMatchingOnHeaderForJWTClaims,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(len(routes[0].GetMatch().GetHeaders())).To(Equal(0))
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[0].GetFilter()).To(Equal(filters.EnvoyJwtFilterName))
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[0].GetInvert()).To(BeFalse())
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[1].GetFilter()).To(Equal(filters.EnvoyJwtFilterName))
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[1].GetInvert()).To(BeTrue())
	})

	t.Run("for virtual service with regex matching on header", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnHeader,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetStringMatch().GetSafeRegex().GetRegex()).To(Equal("Bearer .+?\\..+?\\..+?"))
	})

	t.Run("for virtual service with regex matching on without_header", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnWithoutHeader,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetStringMatch().GetSafeRegex().GetRegex()).To(Equal("BAR .+?\\..+?\\..+?"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetTreatMissingHeaderAsEmpty()).To(Equal(true))
	})

	t.Run("for virtual service with presence matching on header", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithPresentMatchingOnHeader,
			8080, gatewayNames, routeOpts)
		g.Expect(err).NotTo(HaveOccurred())
		xdstest.ValidateRoutes(t, routes)
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(Equal(false))

		routes, err = route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithPresentMatchingOnHeader2,
			8080, gatewayNames, routeOpts)
		g.Expect(err).NotTo(HaveOccurred())
		xdstest.ValidateRoutes(t, routes)
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(Equal(false))
	})

	t.Run("for virtual service with presence matching on header and without_header", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithPresentMatchingOnWithoutHeader,
			8080, gatewayNames, routeOpts)
		g.Expect(err).NotTo(HaveOccurred())
		xdstest.ValidateRoutes(t, routes)
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(Equal(true))
	})

	t.Run("for virtual service with regex matching for all cases on header", func(t *testing.T) {
		cset := createVirtualServiceWithRegexMatchingForAllCasesOnHeader()

		for _, c := range cset {
			g := NewWithT(t)
			cg := core.NewConfigGenTest(t, core.TestOptions{})
			routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), *c,
				8080, gatewayNames, routeOpts)
			xdstest.ValidateRoutes(t, routes)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(routes)).To(Equal(1))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(Equal("FOO-HEADER"))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(Equal(true))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(Equal(false))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetTreatMissingHeaderAsEmpty()).To(Equal(false))
		}
	})

	t.Run("for virtual service with exact matching on query parameter", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithExactMatchingOnQueryParameter,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetQueryParameters()[0].GetStringMatch().GetExact()).To(Equal("foo"))
	})

	t.Run("for virtual service with prefix matching on query parameter", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithPrefixMatchingOnQueryParameter,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetQueryParameters()[0].GetStringMatch().GetPrefix()).To(Equal("foo-"))
	})

	t.Run("for virtual service with regex matching on query parameter", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnQueryParameter,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetMatch().GetQueryParameters()[0].GetStringMatch().GetSafeRegex().GetRegex()).To(Equal("BAR .+?\\..+?\\..+?"))
	})

	t.Run("for virtual service with regex matching for all cases on query parameter", func(t *testing.T) {
		cset := createVirtualServiceWithRegexMatchingForAllCasesOnQueryParameter()

		for _, c := range cset {
			g := NewWithT(t)
			cg := core.NewConfigGenTest(t, core.TestOptions{})
			routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), *c,
				8080, gatewayNames, routeOpts)
			xdstest.ValidateRoutes(t, routes)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(routes)).To(Equal(1))
			g.Expect(routes[0].GetMatch().GetQueryParameters()[0].GetName()).To(Equal("token"))
			g.Expect(routes[0].GetMatch().GetQueryParameters()[0].GetPresentMatch()).To(Equal(true))
		}
	})

	t.Run("for virtual service with source namespace matching", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		fooNode := cg.SetupProxy(&model.Proxy{
			ConfigNamespace: "foo",
		})

		routes, err := route.BuildHTTPRoutesForVirtualService(fooNode, virtualServiceMatchingOnSourceNamespace,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetName()).To(Equal("foo"))

		barNode := cg.SetupProxy(&model.Proxy{
			ConfigNamespace: "bar",
		})

		routes, err = route.BuildHTTPRoutesForVirtualService(barNode, virtualServiceMatchingOnSourceNamespace,
			8080, gatewayNames, routeOpts)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))
		g.Expect(routes[0].GetName()).To(Equal("bar"))
	})

	t.Run("for virtual service with ring hash", func(t *testing.T) {
		g := NewWithT(t)
		ttl := durationpb.Duration{Nanos: 100}
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Services: exampleService,
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "istio-system",
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
			},
		})

		proxy := node(cg)
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualServicePlain)
		routeOpts := buildRouteOpts(serviceRegistry, hashByDestination)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualServicePlain, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie",
					Ttl:  &ttl,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(ConsistOf(hashPolicy))
		g.Expect(len(routes[0].GetRoute().GetRetryPolicy().RetryHostPredicate)).To(Equal(0))
	})

	t.Run("for virtual service with query param based ring hash", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Services: exampleService,
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "istio-system",
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
			},
		})

		proxy := node(cg)
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualServicePlain)
		routeOpts := buildRouteOpts(serviceRegistry, hashByDestination)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualServicePlain, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_QueryParameter_{
				QueryParameter: &envoyroute.RouteAction_HashPolicy_QueryParameter{
					Name: "query",
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(ConsistOf(hashPolicy))
		g.Expect(len(routes[0].GetRoute().GetRetryPolicy().RetryHostPredicate)).To(Equal(0))
	})

	t.Run("for virtual service with subsets with ring hash", func(t *testing.T) {
		g := NewWithT(t)
		virtualService := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "acme",
			},
			Spec: virtualServiceWithSubset,
		}
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Services: exampleService,
			Configs: []config.Config{
				virtualService,
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "istio-system",
					},
					Spec: &networking.DestinationRule{
						Host:    "*.example.org",
						Subsets: []*networking.Subset{networkingSubset},
					},
				},
			},
		})

		proxy := node(cg)
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualService)
		routeOpts := buildRouteOpts(serviceRegistry, hashByDestination)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualService, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "other-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(ConsistOf(hashPolicy))
	})

	t.Run("for virtual service with subsets with port level settings with ring hash", func(t *testing.T) {
		g := NewWithT(t)
		virtualService := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "acme",
			},
			Spec: virtualServiceWithSubsetWithPortLevelSettings,
		}
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Services: exampleService,
			Configs: []config.Config{
				virtualService,
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "istio-system",
					},
					Spec: portLevelDestinationRuleWithSubsetPolicy,
				},
			},
		})

		proxy := node(cg)
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualService)
		routeOpts := buildRouteOpts(serviceRegistry, hashByDestination)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualService, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "port-level-settings-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(ConsistOf(hashPolicy))
	})

	t.Run("for virtual service with subsets and top level traffic policy with ring hash", func(t *testing.T) {
		g := NewWithT(t)

		virtualService := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "acme",
			},
			Spec: virtualServiceWithSubset,
		}

		cnfg := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
				Name:             "acme",
				Namespace:        "istio-system",
			},
		}
		rule := networkingDestinationRule
		rule.Subsets = []*networking.Subset{networkingSubset}
		cnfg.Spec = networkingDestinationRule

		cg := core.NewConfigGenTest(t, core.TestOptions{
			Services: exampleService,
			Configs:  []config.Config{cnfg, virtualService},
		})

		proxy := node(cg)
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualService)
		routeOpts := buildRouteOpts(serviceRegistry, hashByDestination)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualService, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "other-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(ConsistOf(hashPolicy))
	})

	t.Run("port selector based traffic policy", func(t *testing.T) {
		g := NewWithT(t)

		cg := core.NewConfigGenTest(t, core.TestOptions{
			Services: exampleService,
			Configs: []config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "acme",
					Namespace:        "istio-system",
				},
				Spec: portLevelDestinationRule,
			}},
		})

		proxy := node(cg)
		gatewayNames := sets.New("some-gateway")
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualServicePlain)
		routeOpts := buildRouteOpts(serviceRegistry, hashByDestination)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualServicePlain, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie",
					Ttl:  nil,
				},
			},
		}
		g.Expect(routes[0].GetRoute().GetHashPolicy()).To(ConsistOf(hashPolicy))
	})

	t.Run("for header operations for single cluster", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithHeaderOperationsForSingleCluster,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		r := routes[0]
		g.Expect(len(r.RequestHeadersToAdd)).To(Equal(4))
		g.Expect(len(r.ResponseHeadersToAdd)).To(Equal(4))
		g.Expect(len(r.RequestHeadersToRemove)).To(Equal(2))
		g.Expect(len(r.ResponseHeadersToRemove)).To(Equal(2))

		g.Expect(r.RequestHeadersToAdd).To(Equal([]*envoycore.HeaderValueOption{
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-req-set",
					Value: "v1",
				},
				AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-req-add",
					Value: "v2",
				},
				AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-route-req-set",
					Value: "v1",
				},
				AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-route-req-add",
					Value: "v2",
				},
				AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
		}))
		g.Expect(r.RequestHeadersToRemove).To(Equal([]string{"x-req-remove", "x-route-req-remove"}))

		g.Expect(r.ResponseHeadersToAdd).To(Equal([]*envoycore.HeaderValueOption{
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-resp-set",
					Value: "v1",
				},
				AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-resp-add",
					Value: "v2",
				},
				AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-route-resp-set",
					Value: "v1",
				},
				AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
			{
				Header: &envoycore.HeaderValue{
					Key:   "x-route-resp-add",
					Value: "v2",
				},
				AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
		}))
		g.Expect(r.ResponseHeadersToRemove).To(Equal([]string{"x-resp-remove", "x-route-resp-remove"}))

		routeAction, ok := r.GetAction().(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(routeAction.Route.GetHostRewriteLiteral()).To(Equal("foo.extsvc.com"))
	})

	t.Run("for header operations for weighted cluster", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithHeaderOperationsForWeightedCluster,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		r := routes[0]
		routeAction, ok := r.GetAction().(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())

		weightedCluster := routeAction.Route.GetWeightedClusters()
		g.Expect(weightedCluster).NotTo(BeNil())
		g.Expect(len(weightedCluster.GetClusters())).To(Equal(2))

		expectResults := []struct {
			reqAdd     []*envoycore.HeaderValueOption
			reqRemove  []string
			respAdd    []*envoycore.HeaderValueOption
			respRemove []string
			authority  string
		}{
			{
				reqAdd: []*envoycore.HeaderValueOption{
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-req-set-blue",
							Value: "v1",
						},
						AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
					},
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-req-add-blue",
							Value: "v2",
						},
						AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
					},
				},
				reqRemove: []string{"x-route-req-remove-blue"},
				respAdd: []*envoycore.HeaderValueOption{
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-resp-set-blue",
							Value: "v1",
						},
						AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
					},
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-resp-add-blue",
							Value: "v2",
						},
						AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
					},
				},
				respRemove: []string{"x-route-resp-remove-blue"},
				authority:  "blue.foo.extsvc.com",
			},
			{
				reqAdd: []*envoycore.HeaderValueOption{
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-req-set-green",
							Value: "v1",
						},
						AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
					},
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-req-add-green",
							Value: "v2",
						},
						AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
					},
				},
				reqRemove: []string{"x-route-req-remove-green"},
				respAdd: []*envoycore.HeaderValueOption{
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-resp-set-green",
							Value: "v1",
						},
						AppendAction: envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
					},
					{
						Header: &envoycore.HeaderValue{
							Key:   "x-route-resp-add-green",
							Value: "v2",
						},
						AppendAction: envoycore.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
					},
				},
				respRemove: []string{"x-route-resp-remove-green"},
				authority:  "green.foo.extsvc.com",
			},
		}

		for i, expectResult := range expectResults {
			cluster := weightedCluster.GetClusters()[i]
			g.Expect(cluster.RequestHeadersToAdd).To(Equal(expectResult.reqAdd))
			g.Expect(cluster.RequestHeadersToRemove).To(Equal(expectResult.reqRemove))
			g.Expect(cluster.ResponseHeadersToAdd).To(Equal(expectResult.respAdd))
			g.Expect(cluster.RequestHeadersToRemove).To(Equal(expectResult.reqRemove))
			g.Expect(cluster.GetHostRewriteLiteral()).To(Equal(expectResult.authority))
		}
	})

	t.Run("for redirect code", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRedirect, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(redirectAction.Redirect.ResponseCode).To(Equal(envoyroute.RedirectAction_PERMANENT_REDIRECT))
	})

	t.Run("for invalid redirect code", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithInvalidRedirect, 8080, gatewayNames, routeOpts)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		_, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).To(BeFalse())
	})

	t.Run("for path prefix redirect", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRedirectPathPrefix, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(redirectAction.Redirect.PathRewriteSpecifier).To(Equal(&envoyroute.RedirectAction_PrefixRewrite{
			PrefixRewrite: "/replace-prefix",
		}))
	})

	t.Run("for host rewrite", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRewriteHost, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		routeAction, ok := routes[0].Action.(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(routeAction.Route.HostRewriteSpecifier).To(Equal(&envoyroute.RouteAction_HostRewriteLiteral{
			HostRewriteLiteral: "bar.example.org",
		}))
	})

	t.Run("for full path rewrite", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRewriteFullPath, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		routeAction, ok := routes[0].Action.(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())

		g.Expect(routeAction.Route.RegexRewrite).To(Equal(&matcher.RegexMatchAndSubstitute{
			Pattern: &matcher.RegexMatcher{
				Regex: "/.*",
			},
			Substitution: "/replace-full",
		}))
	})

	t.Run("for prefix path rewrite", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRewritePrefixPath, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		routeAction, ok := routes[0].Action.(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(routeAction.Route.PrefixRewrite).To(Equal("/replace-prefix"))
	})

	t.Run("for empty prefix path rewrite", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithEmptyRewritePrefixPath, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		routeAction, ok := routes[0].Action.(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(routeAction.Route.RegexRewrite).To(Equal(&matcher.RegexMatchAndSubstitute{
			Pattern: &matcher.RegexMatcher{
				Regex: `^/prefix-to-be-removed(/?)(.*)`,
			},
			Substitution: `/\2`,
		}))
	})

	t.Run("for path and host rewrite", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg),
			virtualServiceWithRewriteFullPathAndHost,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		routeAction, ok := routes[0].Action.(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(routeAction.Route.HostRewriteSpecifier).To(Equal(&envoyroute.RouteAction_HostRewriteLiteral{
			HostRewriteLiteral: "bar.example.org",
		}))
		g.Expect(routeAction.Route.RegexRewrite).To(Equal(&matcher.RegexMatchAndSubstitute{
			Pattern: &matcher.RegexMatcher{
				Regex: "/.*",
			},
			Substitution: "/replace-full",
		}))
	})

	t.Run("for path regex match with regex rewrite", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg),
			virtualServiceWithPathRegexMatchRegexRewrite,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		routeAction, ok := routes[0].Action.(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(routeAction.Route.RegexRewrite).To(Equal(&matcher.RegexMatchAndSubstitute{
			Pattern: &matcher.RegexMatcher{
				Regex: "^/service/([^/]+)(/.*)$",
			},
			Substitution: "\\2/instance/\\1",
		}))
	})

	t.Run("for redirect uri prefix '%PREFIX()%' that is without gateway semantics", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg),
			virtualServiceWithRedirectPathPrefixNoGatewaySematics,
			8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(redirectAction.Redirect.PathRewriteSpecifier).To(Equal(&envoyroute.RedirectAction_PathRedirect{
			PathRedirect: "%PREFIX()%/replace-full",
		}))
	})

	t.Run("for full path redirect", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRedirectFullPath, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(redirectAction.Redirect.PathRewriteSpecifier).To(Equal(&envoyroute.RedirectAction_PathRedirect{
			PathRedirect: "/replace-full-path",
		}))
	})

	t.Run("for redirect and header manipulation", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRedirectAndSetHeader, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(redirectAction.Redirect.ResponseCode).To(Equal(envoyroute.RedirectAction_PERMANENT_REDIRECT))
		g.Expect(len(routes[0].ResponseHeadersToAdd)).To(Equal(1))
		g.Expect(routes[0].ResponseHeadersToAdd[0].AppendAction).To(Equal(envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Key).To(Equal("Strict-Transport-Security"))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Value).To(Equal("max-age=31536000; includeSubDomains; preload"))
	})

	t.Run("for direct response code", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithDirectResponse, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		directResponseAction, ok := routes[0].Action.(*envoyroute.Route_DirectResponse)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(directResponseAction.DirectResponse.Status).To(Equal(uint32(200)))
		g.Expect(directResponseAction.DirectResponse.Body.Specifier.(*envoycore.DataSource_InlineString).InlineString).To(Equal("hello"))
	})

	t.Run("for direct response code and header manipulation", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithDirectResponseAndSetHeader, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(routes)).To(Equal(1))

		directResponseAction, ok := routes[0].Action.(*envoyroute.Route_DirectResponse)
		g.Expect(ok).NotTo(BeFalse())
		g.Expect(directResponseAction.DirectResponse.Status).To(Equal(uint32(200)))
		g.Expect(directResponseAction.DirectResponse.Body.Specifier.(*envoycore.DataSource_InlineString).InlineString).To(Equal("hello"))
		g.Expect(len(routes[0].ResponseHeadersToAdd)).To(Equal(1))
		g.Expect(routes[0].ResponseHeadersToAdd[0].AppendAction).To(Equal(envoycore.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Key).To(Equal("Strict-Transport-Security"))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Value).To(Equal("max-age=31536000; includeSubDomains; preload"))
	})

	t.Run("for no virtualservice but has destinationrule with consistentHash loadbalancer", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "istio-system",
					},
					Spec: networkingDestinationRule,
				},
			},
			Services: exampleService,
		})
		vhosts := route.BuildSidecarVirtualHostWrapper(nil, node(cg), cg.PushContext(), serviceRegistry,
			[]config.Config{}, 8080, map[host.Name]types.NamespacedName{},
		)
		g.Expect(vhosts[0].Routes[0].Action.(*envoyroute.Route_Route).Route.HashPolicy).NotTo(BeNil())
	})
	t.Run("for no virtualservice but has destinationrule with portLevel consistentHash loadbalancer", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "acme",
						Namespace:        "istio-system",
					},
					Spec: networkingDestinationRuleWithPortLevelTrafficPolicy,
				},
			},
			Services: exampleService,
		})
		vhosts := route.BuildSidecarVirtualHostWrapper(nil, node(cg), cg.PushContext(), serviceRegistry,
			[]config.Config{}, 8080, map[host.Name]types.NamespacedName{},
		)

		hashPolicy := &envoyroute.RouteAction_HashPolicy{
			PolicySpecifier: &envoyroute.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoyroute.RouteAction_HashPolicy_Cookie{
					Name: "hash-cookie-1",
				},
			},
		}
		g.Expect(vhosts[0].Routes[0].Action.(*envoyroute.Route_Route).Route.HashPolicy).To(ConsistOf(hashPolicy))
	})

	t.Run("for virtualservices and services with overlapping wildcard hosts", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Configs: []config.Config{
				virtualServiceWithWildcardHost,
				virtualServiceWithNestedWildcardHost,
				virtualServiceWithGoogleWildcardHost,
			},
			Services: []*model.Service{exampleWildcardService, exampleNestedWildcardService},
		})

		// Redefine the service registry for this test
		serviceRegistry := map[host.Name]*model.Service{
			"*.example.org":             exampleWildcardService,
			"goodbye.hello.example.org": exampleNestedWildcardService,
		}

		wildcardIndex := map[host.Name]types.NamespacedName{
			"*.example.org":       virtualServiceWithWildcardHost.NamespacedName(),
			"*.hello.example.org": virtualServiceWithNestedWildcardHost.NamespacedName(),
		}

		vhosts := route.BuildSidecarVirtualHostWrapper(nil, node(cg), cg.PushContext(), serviceRegistry,
			[]config.Config{
				virtualServiceWithWildcardHost,
				virtualServiceWithNestedWildcardHost,
				virtualServiceWithGoogleWildcardHost,
			}, 8080,
			wildcardIndex,
		)
		log.Printf("%#v", vhosts)
		// *.example.org, *.hello.example.org. The *.google.com VS is missing from virtualHosts because
		// it is not attached to a service
		g.Expect(vhosts).To(HaveLen(2))
		for _, vhost := range vhosts {
			g.Expect(vhost.Services).To(HaveLen(1))
			g.Expect(vhost.Routes).To(HaveLen(1))
		}
	})

	t.Run("for virtual service with routing to an service with inference semantics", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{})

		routeOpts := buildRouteOpts(serviceRegistry, nil)
		routeOpts.InferencePoolExtensionRefs = map[string]string{
			"routeA": "ext-proc-svc.test-namespace.svc.cluster.local:9002",
		}
		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServicePlain, 8080, gatewayNames, routeOpts)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(routes[0].GetTypedPerFilterConfig()).To(HaveKey(wellknown.HTTPExternalProcessing))
		extProcPerRoute := new(extproc.ExtProcPerRoute)
		if err := routes[0].GetTypedPerFilterConfig()[wellknown.HTTPExternalProcessing].UnmarshalTo(extProcPerRoute); err != nil {
			t.Errorf("couldn't unmarshal any proto: %v \n", err)
		}
		// nolint lll
		g.Expect(extProcPerRoute.GetOverrides().GetGrpcService().GetTargetSpecifier().(*envoycore.GrpcService_EnvoyGrpc_).EnvoyGrpc.GetClusterName()).To(Equal("outbound|9002||ext-proc-svc.test-namespace.svc.cluster.local"))
		g.Expect(extProcPerRoute.GetOverrides().GetProcessingMode().GetRequestBodyMode()).To(Equal(extproc.ProcessingMode_FULL_DUPLEX_STREAMED))
		g.Expect(extProcPerRoute.GetOverrides().GetProcessingMode().GetRequestHeaderMode()).To(Equal(extproc.ProcessingMode_SEND))
	})
	t.Run("for virtualservices with with wildcard hosts outside of the serviceregistry (on port 80)", func(t *testing.T) {
		g := NewWithT(t)
		cg := core.NewConfigGenTest(t, core.TestOptions{
			Configs: []config.Config{
				virtualServiceWithWildcardHost,
				virtualServiceWithNestedWildcardHost,
				virtualServiceWithGoogleWildcardHost,
			},
			Services: []*model.Service{exampleWildcardService, exampleNestedWildcardService},
		})

		// Redefine the service registry for this test
		serviceRegistry := map[host.Name]*model.Service{
			"*.example.org":             exampleWildcardService,
			"goodbye.hello.example.org": exampleNestedWildcardService,
		}

		// note that the VS containing *.google.com doesn't have an entry in the wildcard index
		wildcardIndex := map[host.Name]types.NamespacedName{
			"*.example.org":       virtualServiceWithWildcardHost.NamespacedName(),
			"*.hello.example.org": virtualServiceWithNestedWildcardHost.NamespacedName(),
		}

		vhosts := route.BuildSidecarVirtualHostWrapper(nil, node(cg), cg.PushContext(), serviceRegistry,
			[]config.Config{virtualServiceWithGoogleWildcardHost}, 80, wildcardIndex,
		)
		// The service hosts (*.example.org and goodbye.hello.example.org) and the unattached VS host (*.google.com)
		g.Expect(vhosts).To(HaveLen(3))
		for _, vhost := range vhosts {
			if len(vhost.VirtualServiceHosts) > 0 && vhost.VirtualServiceHosts[0] == "*.google.com" {
				// The *.google.com VS shouldn't have any services
				g.Expect(vhost.Services).To(HaveLen(0))
			} else {
				// The other two VSs should have one service each
				g.Expect(vhost.Services).To(HaveLen(1))
			}
			// All VSs should have one route
			g.Expect(vhost.Routes).To(HaveLen(1))
		}
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

var virtualServicePlain = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Name: "routeA",
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

var virtualServiceWithTimeout = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
				Timeout: &durationpb.Duration{
					Seconds: 10,
				},
			},
		},
	},
}

var virtualServiceWithTimeoutDisabled = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
				Timeout: &durationpb.Duration{
					Seconds: 0,
				},
			},
		},
	},
}

var virtualServiceWithCatchAllRoute = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Name: "route",
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

var virtualServiceWithCatchAllPort = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Name: "route 1",
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "catch-all for 8080",
						Port: 8080,
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "example1.default.svc.cluster.local",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
					},
				},
			},
			{
				Name: "route 2",
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "header match",
						Headers: map[string]*networking.StringMatch{
							"cookie": {
								MatchType: &networking.StringMatch_Exact{Exact: "canary"},
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "example2.default.svc.cluster.local",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
					},
				},
			},
			{
				Name: "route 3",
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "example1.default.svc.cluster.local",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
					},
				},
			},
		},
	},
}

var virtualServiceWithWildcardHost = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "wildcard",
	},
	Spec: &networking.VirtualService{
		Hosts: []string{"*.example.org"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "https",
						Port: uint32(8080),
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8080,
							},
						},
					},
				},
			},
		},
	},
}

var virtualServiceWithNestedWildcardHost = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "nested-wildcard",
	},
	Spec: &networking.VirtualService{
		Hosts: []string{"*.hello.example.org"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "https",
						Port: uint32(8080),
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.hello.example.org",
							Port: &networking.PortSelector{
								Number: 8080,
							},
						},
					},
				},
			},
		},
	},
}

var virtualServiceWithGoogleWildcardHost = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "google-wildcard",
	},
	Spec: &networking.VirtualService{
		Hosts: []string{"*.google.com"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
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
							Host: "internal-google.default.svc.cluster.local",
						},
					},
				},
			},
		},
	},
}

var virtualServiceWithCatchAllMultiPrefixRoute = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithCatchAllRouteWeightedDestination = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithHeaderOperationsForSingleCluster = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
								Set:    map[string]string{"x-route-req-set": "v1", ":authority": "internal.foo.extsvc.com"},
								Add:    map[string]string{"x-route-req-add": "v2", ":authority": "internal.bar.extsvc.com"},
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
						Set:    map[string]string{"x-req-set": "v1", ":authority": "foo.extsvc.com"},
						Add:    map[string]string{"x-req-add": "v2", ":authority": "bar.extsvc.com"},
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

var virtualServiceWithHeaderOperationsForWeightedCluster = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
							Subset: "blue",
						},
						Headers: &networking.Headers{
							Request: &networking.Headers_HeaderOperations{
								Set:    map[string]string{"x-route-req-set-blue": "v1", ":authority": "blue.foo.extsvc.com"},
								Add:    map[string]string{"x-route-req-add-blue": "v2", ":authority": "blue.bar.extsvc.com"},
								Remove: []string{"x-route-req-remove-blue"},
							},
							Response: &networking.Headers_HeaderOperations{
								Set:    map[string]string{"x-route-resp-set-blue": "v1"},
								Add:    map[string]string{"x-route-resp-add-blue": "v2"},
								Remove: []string{"x-route-resp-remove-blue"},
							},
						},
						Weight: 9,
					},
					{
						Destination: &networking.Destination{
							Host:   "c-weighted.extsvc.com",
							Subset: "green",
						},
						Headers: &networking.Headers{
							Request: &networking.Headers_HeaderOperations{
								Set:    map[string]string{"x-route-req-set-green": "v1", ":authority": "green.foo.extsvc.com"},
								Add:    map[string]string{"x-route-req-add-green": "v2", ":authority": "green.bar.extsvc.com"},
								Remove: []string{"x-route-req-remove-green"},
							},
							Response: &networking.Headers_HeaderOperations{
								Set:    map[string]string{"x-route-resp-set-green": "v1"},
								Add:    map[string]string{"x-route-resp-add-green": "v2"},
								Remove: []string{"x-route-resp-remove-green"},
							},
						},
						Weight: 1,
					},
				},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Set:    map[string]string{"x-req-set": "v1", ":authority": "foo.extsvc.com"},
						Add:    map[string]string{"x-req-add": "v2", ":authority": "bar.extsvc.com"},
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

var virtualServiceWithRedirect = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithInvalidRedirect = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
					RedirectCode: 317,
				},
			},
		},
	},
}

var virtualServiceWithRedirectPathPrefix = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Redirect: &networking.HTTPRedirect{
					Uri:          "%PREFIX()%/replace-prefix",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithRedirectPathPrefixNoGatewaySematics = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Redirect: &networking.HTTPRedirect{
					Uri:          "%PREFIX()%/replace-full",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithRedirectFullPath = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Redirect: &networking.HTTPRedirect{
					Uri:          "/replace-full-path",
					Authority:    "some-authority.default.svc.cluster.local",
					RedirectCode: 308,
				},
			},
		},
	},
}

var virtualServiceWithRewriteHost = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "host-rewrite",
					},
				},
				Rewrite: &networking.HTTPRewrite{
					Authority: "bar.example.org",
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithRewritePrefixPath = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "prefix-path-rewrite",
					},
				},
				Rewrite: &networking.HTTPRewrite{
					Uri: "/replace-prefix",
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithEmptyRewritePrefixPath = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "prefix-path-rewrite",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/prefix-to-be-removed"},
						},
					},
				},
				Rewrite: &networking.HTTPRewrite{
					Uri: "/",
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithRewriteFullPath = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "full-path-rewrite",
					},
				},
				Rewrite: &networking.HTTPRewrite{
					UriRegexRewrite: &networking.RegexRewrite{
						Match:   "/.*",
						Rewrite: "/replace-full",
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithRewriteFullPathAndHost = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "full-path-and-host-rewrite",
					},
				},
				Rewrite: &networking.HTTPRewrite{
					UriRegexRewrite: &networking.RegexRewrite{
						Match:   "/.*",
						Rewrite: "/replace-full",
					},
					Authority: "bar.example.org",
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithPathRegexMatchRegexRewrite = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
		Annotations: map[string]string{
			"internal.istio.io/route-semantics": "gateway",
		},
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "full-path-and-host-rewrite",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Regex{
								Regex: "^/service/[^/]+/.*$",
							},
						},
					},
				},
				Rewrite: &networking.HTTPRewrite{
					UriRegexRewrite: &networking.RegexRewrite{
						Match:   "^/service/([^/]+)(/.*)$",
						Rewrite: "\\2/instance/\\1",
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "foo.example.org",
						},
						Weight: 100,
					},
				},
			},
		},
	},
}

var virtualServiceWithRedirectAndSetHeader = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithDirectResponse = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				DirectResponse: &networking.HTTPDirectResponse{
					Status: 200,
					Body: &networking.HTTPBody{
						Specifier: &networking.HTTPBody_String_{String_: "hello"},
					},
				},
			},
		},
	},
}

var virtualServiceWithDirectResponseAndSetHeader = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts:    []string{},
		Gateways: []string{"some-gateway"},
		Http: []*networking.HTTPRoute{
			{
				DirectResponse: &networking.HTTPDirectResponse{
					Status: 200,
					Body: &networking.HTTPBody{
						Specifier: &networking.HTTPBody_String_{String_: "hello"},
					},
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

var virtualServiceWithRegexMatchingOnURI = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithExactMatchingOnHeaderForJWTClaims = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
							"@request.auth.claims.Foo": {
								MatchType: &networking.StringMatch_Exact{
									Exact: "Bar",
								},
							},
						},
						WithoutHeaders: map[string]*networking.StringMatch{
							"@request.auth.claims.Bla": {
								MatchType: &networking.StringMatch_Exact{
									Exact: "Bar",
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

var virtualServiceWithRegexMatchingOnHeader = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

func createVirtualServiceWithRegexMatchingForAllCasesOnHeader() []*config.Config {
	ret := []*config.Config{}
	regex := "*"
	ret = append(ret, &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithRegexMatchingOnWithoutHeader = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithPresentMatchingOnHeader = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithPresentMatchingOnHeader2 = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
							"FOO-HEADER": {},
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

var virtualServiceWithPresentMatchingOnWithoutHeader = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithExactMatchingOnQueryParameter = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
						QueryParams: map[string]*networking.StringMatch{
							"token": {
								MatchType: &networking.StringMatch_Exact{
									Exact: "foo",
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

var virtualServiceWithPrefixMatchingOnQueryParameter = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
						QueryParams: map[string]*networking.StringMatch{
							"token": {
								MatchType: &networking.StringMatch_Prefix{
									Prefix: "foo-",
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

var virtualServiceWithRegexMatchingOnQueryParameter = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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
						QueryParams: map[string]*networking.StringMatch{
							"token": {
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

func createVirtualServiceWithRegexMatchingForAllCasesOnQueryParameter() []*config.Config {
	ret := []*config.Config{}
	regex := "*"
	ret = append(ret, &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
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
							QueryParams: map[string]*networking.StringMatch{
								"token": {
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

var virtualServiceMatchingOnSourceNamespace = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
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

var virtualServiceWithStatPrefix = config.Config{
	Meta: config.Meta{
		GroupVersionKind: gvk.VirtualService,
		Name:             "acme",
	},
	Spec: &networking.VirtualService{
		Hosts: []string{},
		Http: []*networking.HTTPRoute{
			{
				Name: "foo",
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "foo",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/foo",
							},
						},
						StatPrefix: "foo",
					},
					{
						Name: "baz",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/baz",
							},
						},
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
						Name: "bar",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/bar",
							},
						},
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

var (
	exampleService         = []*model.Service{{Hostname: "*.example.org", Attributes: model.ServiceAttributes{Namespace: "istio-system"}}}
	exampleWildcardService = &model.Service{
		Hostname:   "*.example.org",
		Attributes: model.ServiceAttributes{Namespace: "istio-system"},
		Ports:      []*model.Port{{Port: 8080, Protocol: "HTTP"}},
	}
	exampleNestedWildcardService = &model.Service{
		Hostname:   "goodbye.hello.example.org",
		Attributes: model.ServiceAttributes{Namespace: "istio-system"},
		Ports:      []*model.Port{{Port: 8080, Protocol: "HTTP"}},
	}
)

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

func TestSortVHostRoutes(t *testing.T) {
	first := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				Regex: ".*?regex1",
			},
		}}},
	}
	wantFirst := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				Regex: ".*?regex1",
			},
		}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
	}
	second := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				Regex: ".*?regex12",
			},
		}}},
		{Match: &envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
				SafeRegex: &matcher.RegexMatcher{
					Regex: "*",
				},
			},
			Headers: []*envoyroute.HeaderMatcher{
				{
					Name: "foo",
					HeaderMatchSpecifier: &envoyroute.HeaderMatcher_StringMatch{
						StringMatch: &matcher.StringMatcher{MatchPattern: &matcher.StringMatcher_Exact{Exact: "bar"}},
					},
					InvertMatch: false,
				},
			},
		}},
	}

	wantSecond := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix12"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				Regex: ".*?regex12",
			},
		}}},
		{Match: &envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
				SafeRegex: &matcher.RegexMatcher{
					Regex: "*",
				},
			},
			Headers: []*envoyroute.HeaderMatcher{
				{
					Name: "foo",
					HeaderMatchSpecifier: &envoyroute.HeaderMatcher_StringMatch{
						StringMatch: &matcher.StringMatcher{MatchPattern: &matcher.StringMatcher_Exact{Exact: "bar"}},
					},
					InvertMatch: false,
				},
			},
		}},
	}

	testCases := []struct {
		name     string
		in       []*envoyroute.Route
		expected []*envoyroute.Route
	}{
		{
			name:     "routes with catchall match",
			in:       first,
			expected: wantFirst,
		},
		{
			name:     "routes without catchall match",
			in:       second,
			expected: wantSecond,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := route.SortVHostRoutes(tc.in)
			if !reflect.DeepEqual(tc.expected, got) {
				t.Errorf("SortVHostRoutes: \n")
				t.Errorf("got: \n")
				for _, g := range got {
					t.Errorf("%v\n", g.Match.PathSpecifier)
				}
				t.Errorf("want: \n")
				for _, g := range tc.expected {
					t.Errorf("%v\n", g.Match.PathSpecifier)
				}
			}
		})
	}
}

func TestInboundHTTPRoute(t *testing.T) {
	testCases := []struct {
		name        string
		enableRetry bool
		protocol    protocol.Instance
		expected    *envoyroute.Route
	}{
		{
			name:        "enable retry, http protocol",
			enableRetry: true,
			protocol:    protocol.HTTP,
			expected: &envoyroute.Route{
				Name:  "default",
				Match: route.TranslateRouteMatch(config.Config{}, nil),
				Action: &envoyroute.Route_Route{
					Route: &envoyroute.RouteAction{
						ClusterSpecifier: &envoyroute.RouteAction_Cluster{Cluster: "cluster"},
						RetryPolicy: &envoyroute.RetryPolicy{
							RetryOn: "reset-before-request",
							NumRetries: &wrapperspb.UInt32Value{
								Value: 2,
							},
						},
						Timeout: route.Notimeout,
						MaxStreamDuration: &envoyroute.RouteAction_MaxStreamDuration{
							MaxStreamDuration:    route.Notimeout,
							GrpcTimeoutHeaderMax: route.Notimeout,
						},
					},
				},
				Decorator: &envoyroute.Decorator{
					Operation: "operation",
				},
			},
		},
		{
			name:        "enable retry, grpc protocol",
			enableRetry: true,
			protocol:    protocol.GRPC,
			expected: &envoyroute.Route{
				Name:  "default",
				Match: route.TranslateRouteMatch(config.Config{}, nil),
				Action: &envoyroute.Route_Route{
					Route: &envoyroute.RouteAction{
						ClusterSpecifier: &envoyroute.RouteAction_Cluster{Cluster: "cluster"},
						Timeout:          route.Notimeout,
						MaxStreamDuration: &envoyroute.RouteAction_MaxStreamDuration{
							MaxStreamDuration:    route.Notimeout,
							GrpcTimeoutHeaderMax: route.Notimeout,
						},
					},
				},
				Decorator: &envoyroute.Decorator{
					Operation: "operation",
				},
			},
		},
		{
			name:        "disable retry",
			enableRetry: false,
			protocol:    protocol.HTTP,
			expected: &envoyroute.Route{
				Name:  "default",
				Match: route.TranslateRouteMatch(config.Config{}, nil),
				Action: &envoyroute.Route_Route{
					Route: &envoyroute.RouteAction{
						ClusterSpecifier: &envoyroute.RouteAction_Cluster{Cluster: "cluster"},
						Timeout:          route.Notimeout,
						MaxStreamDuration: &envoyroute.RouteAction_MaxStreamDuration{
							MaxStreamDuration:    route.Notimeout,
							GrpcTimeoutHeaderMax: route.Notimeout,
						},
					},
				},
				Decorator: &envoyroute.Decorator{
					Operation: "operation",
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableInboundRetryPolicy, tc.enableRetry)
			inroute := route.BuildDefaultHTTPInboundRoute(&model.Proxy{IstioVersion: &model.IstioVersion{Major: 1, Minor: 24, Patch: -1}},
				"cluster", "operation", tc.protocol)
			if !reflect.DeepEqual(tc.expected, inroute) {
				t.Errorf("error in inbound routes. Got: %v, Want: %v", inroute, tc.expected)
			}
		})
	}
}

func TestCheckAndGetInferencePoolConfig(t *testing.T) {
	virtualService := config.Config{
		Meta: config.Meta{
			Namespace: "test-namespace",
		},
		Spec: &networking.VirtualService{
			Http: []*networking.HTTPRoute{
				{
					Name: "%%service-name%%8080%%route-name",
				},
			},
		},
	}

	expected := map[string]string{}
	expected["%%service-name%%8080%%route-name"] = "service-name.test-namespace.svc.cluster.local:8080"
	result := route.CheckAndGetInferencePoolConfigs(virtualService)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}
