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
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
)

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

	node := func(cg *v1alpha3.ConfigGenTest) *model.Proxy {
		return cg.SetupProxy(&model.Proxy{
			Type:        model.SidecarProxy,
			IPAddresses: []string{"1.1.1.1"},
			ID:          "someID",
			DNSDomain:   "foo.com",
		})
	}

	gatewayNames := map[string]bool{"some-gateway": true}

	t.Run("for virtual service", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		t.Setenv("ISTIO_DEFAULT_REQUEST_TIMEOUT", "0ms")

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServicePlain, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		// Validate that when timeout is not specified, we disable it based on default value of flag.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(0)))
		g.Expect(routes[0].GetRoute().MaxStreamDuration.GrpcTimeoutHeaderMax.Seconds).To(gomega.Equal(int64(0)))
		g.Expect(routes[0].GetRoute().MaxStreamDuration.MaxStreamDuration.Seconds).To(gomega.Equal(int64(0)))
	})

	t.Run("for virtual service with HTTP/3 discovery enabled", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServicePlain, serviceRegistry, nil, 8080, gatewayNames, true, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(routes[0].GetResponseHeadersToAdd()).To(gomega.Equal([]*core.HeaderValueOption{
			{
				Header: &core.HeaderValue{
					Key:   util.AltSvcHeader,
					Value: `h3=":8080"; ma=86400`,
				},
				Append: &wrappers.BoolValue{Value: true},
			},
		}))
	})

	t.Run("for virtual service with changed default timeout", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		dt := features.DefaultRequestTimeout
		features.DefaultRequestTimeout = durationpb.New(1 * time.Second)
		defer func() { features.DefaultRequestTimeout = dt }()

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServicePlain, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		// Validate that when timeout is not specified, we send what is set in the timeout flag.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(1)))
		// nolint: staticcheck
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(gomega.Equal(int64(1)))
	})

	t.Run("for virtual service with timeout", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithTimeout, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		// Validate that when timeout specified, we send the configured timeout to Envoys.
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(10)))
		// nolint: staticcheck
		g.Expect(routes[0].GetRoute().MaxGrpcTimeout.Seconds).To(gomega.Equal(int64(10)))
	})

	t.Run("for virtual service with disabled timeout", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithTimeoutDisabled, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetRoute().Timeout.Seconds).To(gomega.Equal(int64(0)))
		g.Expect(routes[0].GetRoute().MaxStreamDuration.MaxStreamDuration.Seconds).To(gomega.Equal(int64(0)))
		g.Expect(routes[0].GetRoute().MaxStreamDuration.GrpcTimeoutHeaderMax.Seconds).To(gomega.Equal(int64(0)))
	})

	t.Run("for virtual service with catch all route", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})
		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllRoute,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(2))
		g.Expect(routes[0].Name).To(gomega.Equal("route.non-catch-all"))
		g.Expect(routes[1].Name).To(gomega.Equal("route.catch-all"))
	})

	t.Run("for virtual service with catch all route：port match", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})
		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllPort,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].Name).To(gomega.Equal("route 1.catch-all for 8080"))
	})

	t.Run("for internally generated virtual service with ingress semantics (istio version<1.14)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		vs := virtualServiceWithCatchAllRoute
		if vs.Annotations == nil {
			vs.Annotations = make(map[string]string)
		}
		vs.Annotations[constants.InternalRouteSemantics] = constants.RouteSemanticsIngress

		proxy := node(cg)
		proxy.IstioVersion = &model.IstioVersion{
			Major: 1,
			Minor: 13,
		}
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, vs,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(routes[0].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: util.RegexEngine,
				Regex:      `/route/v1((\/).*)?`,
			},
		}))
		g.Expect(routes[1].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_Prefix{
			Prefix: "/",
		}))
	})

	t.Run("for internally generated virtual service with gateway semantics (istio version<1.14)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		vs := virtualServiceWithCatchAllRoute
		if vs.Annotations == nil {
			vs.Annotations = make(map[string]string)
		}
		vs.Annotations[constants.InternalRouteSemantics] = constants.RouteSemanticsGateway

		proxy := node(cg)
		proxy.IstioVersion = &model.IstioVersion{
			Major: 1,
			Minor: 13,
		}
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, vs,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(routes[0].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: util.RegexEngine,
				Regex:      `/route/v1((\/).*)?`,
			},
		}))
		g.Expect(routes[0].Action.(*envoyroute.Route_Route).Route.ClusterNotFoundResponseCode).
			To(gomega.Equal(envoyroute.RouteAction_SERVICE_UNAVAILABLE))
		g.Expect(routes[1].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_Prefix{
			Prefix: "/",
		}))
		g.Expect(routes[1].Action.(*envoyroute.Route_Route).Route.ClusterNotFoundResponseCode).
			To(gomega.Equal(envoyroute.RouteAction_SERVICE_UNAVAILABLE))
	})

	t.Run("for internally generated virtual service with ingress semantics", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		vs := virtualServiceWithCatchAllRoute
		if vs.Annotations == nil {
			vs.Annotations = make(map[string]string)
		}
		vs.Annotations[constants.InternalRouteSemantics] = constants.RouteSemanticsIngress

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), vs,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(routes[0].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_PathSeparatedPrefix{
			PathSeparatedPrefix: "/route/v1",
		}))
		g.Expect(routes[1].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_Prefix{
			Prefix: "/",
		}))
	})

	t.Run("for internally generated virtual service with gateway semantics", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		vs := virtualServiceWithCatchAllRoute
		if vs.Annotations == nil {
			vs.Annotations = make(map[string]string)
		}
		vs.Annotations[constants.InternalRouteSemantics] = constants.RouteSemanticsGateway

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), vs,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(routes[0].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_PathSeparatedPrefix{
			PathSeparatedPrefix: "/route/v1",
		}))
		g.Expect(routes[0].Action.(*envoyroute.Route_Route).Route.ClusterNotFoundResponseCode).
			To(gomega.Equal(envoyroute.RouteAction_INTERNAL_SERVER_ERROR))
		g.Expect(routes[1].Match.PathSpecifier).To(gomega.Equal(&envoyroute.RouteMatch_Prefix{
			Prefix: "/",
		}))
		g.Expect(routes[1].Action.(*envoyroute.Route_Route).Route.ClusterNotFoundResponseCode).
			To(gomega.Equal(envoyroute.RouteAction_INTERNAL_SERVER_ERROR))
	})

	t.Run("for virtual service with top level catch all route", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllRouteWeightedDestination,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("for virtual service with multi prefix catch all route", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithCatchAllMultiPrefixRoute,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
	})

	t.Run("for virtual service with regex matching on URI", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnURI,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetSafeRegex().GetRegex()).To(gomega.Equal("\\/(.?)\\/status"))
	})

	t.Run("for virtual service with exact matching on JWT claims", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithExactMatchingOnHeaderForJWTClaims,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(len(routes[0].GetMatch().GetHeaders())).To(gomega.Equal(0))
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[0].GetFilter()).To(gomega.Equal("istio_authn"))
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[0].GetInvert()).To(gomega.BeFalse())
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[1].GetFilter()).To(gomega.Equal("istio_authn"))
		g.Expect(routes[0].GetMatch().GetDynamicMetadata()[1].GetInvert()).To(gomega.BeTrue())
	})

	t.Run("for virtual service with regex matching on header", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnHeader,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetStringMatch().GetSafeRegex().GetRegex()).To(gomega.Equal("Bearer .+?\\..+?\\..+?"))
	})

	t.Run("for virtual service with regex matching on without_header", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRegexMatchingOnWithoutHeader,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetStringMatch().GetSafeRegex().GetRegex()).To(gomega.Equal("BAR .+?\\..+?\\..+?"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(true))
	})

	t.Run("for virtual service with presence matching on header", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithPresentMatchingOnHeader,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		xdstest.ValidateRoutes(t, routes)
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(gomega.Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(gomega.Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(false))
	})

	t.Run("for virtual service with presence matching on header and without_header", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithPresentMatchingOnWithoutHeader,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		xdstest.ValidateRoutes(t, routes)
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(gomega.Equal("FOO-HEADER"))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(gomega.Equal(true))
		g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(true))
	})

	t.Run("for virtual service with regex matching for all cases on header", func(t *testing.T) {
		cset := createVirtualServiceWithRegexMatchingForAllCasesOnHeader()

		for _, c := range cset {
			g := gomega.NewWithT(t)
			cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})
			routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), *c, serviceRegistry, nil,
				8080, gatewayNames, false, nil)
			xdstest.ValidateRoutes(t, routes)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(len(routes)).To(gomega.Equal(1))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetName()).To(gomega.Equal("FOO-HEADER"))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetPresentMatch()).To(gomega.Equal(true))
			g.Expect(routes[0].GetMatch().GetHeaders()[0].GetInvertMatch()).To(gomega.Equal(false))
		}
	})

	t.Run("for virtual service with source namespace matching", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		fooNode := cg.SetupProxy(&model.Proxy{
			ConfigNamespace: "foo",
		})

		routes, err := route.BuildHTTPRoutesForVirtualService(fooNode, virtualServiceMatchingOnSourceNamespace,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetName()).To(gomega.Equal("foo"))

		barNode := cg.SetupProxy(&model.Proxy{
			ConfigNamespace: "bar",
		})

		routes, err = route.BuildHTTPRoutesForVirtualService(barNode, virtualServiceMatchingOnSourceNamespace,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))
		g.Expect(routes[0].GetName()).To(gomega.Equal("bar"))
	})

	t.Run("for virtual service with ring hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ttl := durationpb.Duration{Nanos: 100}
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualServicePlain, serviceRegistry)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualServicePlain, serviceRegistry,
			hashByDestination, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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

	t.Run("for virtual service with query param based ring hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualServicePlain, serviceRegistry)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualServicePlain, serviceRegistry,
			hashByDestination, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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
		g := gomega.NewWithT(t)
		virtualService := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "acme",
			},
			Spec: virtualServiceWithSubset,
		}
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualService, serviceRegistry)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualService, serviceRegistry,
			hashByDestination, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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
		g := gomega.NewWithT(t)
		virtualService := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "acme",
			},
			Spec: virtualServiceWithSubsetWithPortLevelSettings,
		}
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualService, serviceRegistry)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualService, serviceRegistry,
			hashByDestination, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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
		g := gomega.NewWithT(t)

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

		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
			Services: exampleService,
			Configs:  []config.Config{cnfg, virtualService},
		})

		proxy := node(cg)
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualService, serviceRegistry)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualService, serviceRegistry,
			hashByDestination, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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
		g := gomega.NewWithT(t)

		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		gatewayNames := map[string]bool{"some-gateway": true}
		hashByDestination := route.GetConsistentHashForVirtualService(cg.PushContext(), proxy, virtualServicePlain, serviceRegistry)
		routes, err := route.BuildHTTPRoutesForVirtualService(proxy, virtualServicePlain, serviceRegistry,
			hashByDestination, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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

	t.Run("for header operations for single cluster", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithHeaderOperationsForSingleCluster,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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

		routeAction, ok := r.GetAction().(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(gomega.BeFalse())
		g.Expect(routeAction.Route.GetHostRewriteLiteral()).To(gomega.Equal("foo.extsvc.com"))
	})

	t.Run("for header operations for weighted cluster", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithHeaderOperationsForWeightedCluster,
			serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		r := routes[0]
		routeAction, ok := r.GetAction().(*envoyroute.Route_Route)
		g.Expect(ok).NotTo(gomega.BeFalse())

		weightedCluster := routeAction.Route.GetWeightedClusters()
		g.Expect(weightedCluster).NotTo(gomega.BeNil())
		g.Expect(len(weightedCluster.GetClusters())).To(gomega.Equal(2))

		expectResults := []struct {
			reqAdd     []*core.HeaderValueOption
			reqRemove  []string
			respAdd    []*core.HeaderValueOption
			respRemove []string
			authority  string
		}{
			{
				reqAdd: []*core.HeaderValueOption{
					{
						Header: &core.HeaderValue{
							Key:   "x-route-req-set-blue",
							Value: "v1",
						},
						Append: &wrappers.BoolValue{Value: false},
					},
					{
						Header: &core.HeaderValue{
							Key:   "x-route-req-add-blue",
							Value: "v2",
						},
						Append: &wrappers.BoolValue{Value: true},
					},
				},
				reqRemove: []string{"x-route-req-remove-blue"},
				respAdd: []*core.HeaderValueOption{
					{
						Header: &core.HeaderValue{
							Key:   "x-route-resp-set-blue",
							Value: "v1",
						},
						Append: &wrappers.BoolValue{Value: false},
					},
					{
						Header: &core.HeaderValue{
							Key:   "x-route-resp-add-blue",
							Value: "v2",
						},
						Append: &wrappers.BoolValue{Value: true},
					},
				},
				respRemove: []string{"x-route-resp-remove-blue"},
				authority:  "blue.foo.extsvc.com",
			},
			{
				reqAdd: []*core.HeaderValueOption{
					{
						Header: &core.HeaderValue{
							Key:   "x-route-req-set-green",
							Value: "v1",
						},
						Append: &wrappers.BoolValue{Value: false},
					},
					{
						Header: &core.HeaderValue{
							Key:   "x-route-req-add-green",
							Value: "v2",
						},
						Append: &wrappers.BoolValue{Value: true},
					},
				},
				reqRemove: []string{"x-route-req-remove-green"},
				respAdd: []*core.HeaderValueOption{
					{
						Header: &core.HeaderValue{
							Key:   "x-route-resp-set-green",
							Value: "v1",
						},
						Append: &wrappers.BoolValue{Value: false},
					},
					{
						Header: &core.HeaderValue{
							Key:   "x-route-resp-add-green",
							Value: "v2",
						},
						Append: &wrappers.BoolValue{Value: true},
					},
				},
				respRemove: []string{"x-route-resp-remove-green"},
				authority:  "green.foo.extsvc.com",
			},
		}

		var totalWeight uint32
		for i, expectResult := range expectResults {
			cluster := weightedCluster.GetClusters()[i]
			g.Expect(cluster.RequestHeadersToAdd).To(gomega.Equal(expectResult.reqAdd))
			g.Expect(cluster.RequestHeadersToRemove).To(gomega.Equal(expectResult.reqRemove))
			g.Expect(cluster.ResponseHeadersToAdd).To(gomega.Equal(expectResult.respAdd))
			g.Expect(cluster.RequestHeadersToRemove).To(gomega.Equal(expectResult.reqRemove))
			g.Expect(cluster.GetHostRewriteLiteral()).To(gomega.Equal(expectResult.authority))
			totalWeight += cluster.Weight.GetValue()
		}
		// total weight must be set
		g.Expect(weightedCluster.GetTotalWeight().GetValue()).To(gomega.Equal(totalWeight))
	})

	t.Run("for redirect code", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRedirect, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		redirectAction, ok := routes[0].Action.(*envoyroute.Route_Redirect)
		g.Expect(ok).NotTo(gomega.BeFalse())
		g.Expect(redirectAction.Redirect.ResponseCode).To(gomega.Equal(envoyroute.RedirectAction_PERMANENT_REDIRECT))
	})

	t.Run("for redirect and header manipulation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithRedirectAndSetHeader, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
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

	t.Run("for direct response code", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithDirectResponse, serviceRegistry, nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		directResponseAction, ok := routes[0].Action.(*envoyroute.Route_DirectResponse)
		g.Expect(ok).NotTo(gomega.BeFalse())
		g.Expect(directResponseAction.DirectResponse.Status).To(gomega.Equal(uint32(200)))
		g.Expect(directResponseAction.DirectResponse.Body.Specifier.(*core.DataSource_InlineString).InlineString).To(gomega.Equal("hello"))
	})

	t.Run("for direct response code and header manipulation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})

		routes, err := route.BuildHTTPRoutesForVirtualService(node(cg), virtualServiceWithDirectResponseAndSetHeader, serviceRegistry,
			nil, 8080, gatewayNames, false, nil)
		xdstest.ValidateRoutes(t, routes)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(len(routes)).To(gomega.Equal(1))

		directResponseAction, ok := routes[0].Action.(*envoyroute.Route_DirectResponse)
		g.Expect(ok).NotTo(gomega.BeFalse())
		g.Expect(directResponseAction.DirectResponse.Status).To(gomega.Equal(uint32(200)))
		g.Expect(directResponseAction.DirectResponse.Body.Specifier.(*core.DataSource_InlineString).InlineString).To(gomega.Equal("hello"))
		g.Expect(len(routes[0].ResponseHeadersToAdd)).To(gomega.Equal(1))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Append.Value).To(gomega.BeFalse())
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Key).To(gomega.Equal("Strict-Transport-Security"))
		g.Expect(routes[0].ResponseHeadersToAdd[0].Header.Value).To(gomega.Equal("max-age=31536000; includeSubDomains; preload"))
	})

	t.Run("for no virtualservice but has destinationrule with consistentHash loadbalancer", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		vhosts := route.BuildSidecarVirtualHostWrapper(nil, node(cg), cg.PushContext(), serviceRegistry, []config.Config{}, 8080)
		g.Expect(vhosts[0].Routes[0].Action.(*envoyroute.Route_Route).Route.HashPolicy).NotTo(gomega.BeNil())
	})
	t.Run("for no virtualservice but has destinationrule with portLevel consistentHash loadbalancer", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
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
		vhosts := route.BuildSidecarVirtualHostWrapper(nil, node(cg), cg.PushContext(), serviceRegistry, []config.Config{}, 8080)

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

var exampleService = []*model.Service{{Hostname: "*.example.org", Attributes: model.ServiceAttributes{Namespace: "istio-system"}}}

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
	regexEngine := &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{}}
	first := []*envoyroute.Route{
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Path{Path: "/path1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/prefix1"}}},
		{Match: &envoyroute.RouteMatch{PathSpecifier: &envoyroute.RouteMatch_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				EngineType: regexEngine,
				Regex:      ".*?regex1",
			},
		}}},
	}
	wantFirst := []*envoyroute.Route{
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
