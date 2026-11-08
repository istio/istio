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

package core_test

import (
	"fmt"
	"testing"
	"time"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	metadatafake "k8s.io/client-go/metadata/fake"
	k8stesting "k8s.io/client-go/testing"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiconsts "sigs.k8s.io/gateway-api/pkg/consts"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

func TestGatewayTLSRouteListenerPrecedence(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPI, true)
	cert, key, err := pkiutil.GenCertKeyFromOptions(pkiutil.CertOptions{
		Host: "*.example.com", TTL: time.Hour, IsSelfSigned: true, IsServer: true,
		ECSigAlg: pkiutil.EcdsaSigAlg,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Both listeners are valid Gateway API TLS termination listeners. Listener
	// hostname precedence must choose the exact listener before considering
	// the TLSRoute attached to the wildcard listener, regardless of object order.
	for _, reverseListeners := range []bool{false, true} {
		for _, exactGatewayFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("reverse-listeners=%v/exact-gateway-first=%v", reverseListeners, exactGatewayFirst), func(t *testing.T) {
				exactName, wildcardName := gatewayv1.SectionName("z-exact"), gatewayv1.SectionName("a-wildcard")
				if exactGatewayFirst {
					exactName, wildcardName = "a-exact", "z-wildcard"
				}
				listener := func(name gatewayv1.SectionName, hostname gatewayv1.Hostname, kind gatewayv1.Kind, secret string) gatewayv1.Listener {
					return gatewayv1.Listener{
						Name: name, Hostname: ptr.Of(hostname), Port: 443, Protocol: gatewayv1.TLSProtocolType,
						AllowedRoutes: &gatewayv1.AllowedRoutes{
							Kinds: []gatewayv1.RouteGroupKind{{Group: ptr.Of(gatewayv1.Group(gatewayv1.GroupName)), Kind: kind}},
						},
						TLS: &gatewayv1.ListenerTLSConfig{
							Mode:            ptr.Of(gatewayv1.TLSModeTerminate),
							CertificateRefs: []gatewayv1.SecretObjectReference{{Name: gatewayv1.ObjectName(secret)}},
						},
					}
				}
				listeners := []gatewayv1.Listener{
					listener(exactName, "foo.example.com", "TCPRoute", "exact-cert"),
					listener(wildcardName, "*.example.com", "TLSRoute", "wildcard-cert"),
				}
				if reverseListeners {
					listeners[0], listeners[1] = listeners[1], listeners[0]
				}
				backend := func(name gatewayv1.ObjectName) gatewayv1.BackendRef {
					return gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: name, Port: ptr.Of(gatewayv1.PortNumber(8080)),
					}}
				}
				service := func(name string, port int32) *corev1.Service {
					return &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
						Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
							Name: "tcp", Port: port, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(port),
						}}},
					}
				}
				gatewayService := service("ingressgateway", 443)
				gatewayService.Spec.ClusterIP = "10.0.0.1"
				gatewayService.Spec.Selector = map[string]string{"app": "gateway"}
				objects := []runtime.Object{
					&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
					&gatewayv1.GatewayClass{
						ObjectMeta: metav1.ObjectMeta{Name: "istio"},
						Spec:       gatewayv1.GatewayClassSpec{ControllerName: "istio.io/gateway-controller"},
					},
					&gatewayv1.Gateway{
						ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "default"},
						Spec: gatewayv1.GatewaySpec{
							GatewayClassName: "istio",
							Addresses: []gatewayv1.GatewaySpecAddress{{
								Type: ptr.Of(gatewayv1.HostnameAddressType), Value: "ingressgateway",
							}},
							Listeners: listeners,
						},
					},
					&gatewayv1.TCPRoute{
						ObjectMeta: metav1.ObjectMeta{Name: "tcp-route", Namespace: "default"},
						Spec: gatewayv1.TCPRouteSpec{
							CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
								Name: "gateway", SectionName: ptr.Of(exactName),
							}}},
							Rules: []gatewayv1.TCPRouteRule{{BackendRefs: []gatewayv1.BackendRef{backend("tcp-backend")}}},
						},
					},
					&gatewayv1.TLSRoute{
						ObjectMeta: metav1.ObjectMeta{Name: "tls-route", Namespace: "default"},
						Spec: gatewayv1.TLSRouteSpec{
							CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
								Name: "gateway", SectionName: ptr.Of(wildcardName),
							}}},
							Hostnames: []gatewayv1.Hostname{"foo.example.com", "bar.example.com"},
							Rules:     []gatewayv1.TLSRouteRule{{BackendRefs: []gatewayv1.BackendRef{backend("tls-backend")}}},
						},
					},
					gatewayService,
					service("tcp-backend", 8080),
					service("tls-backend", 8080),
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "default", Labels: map[string]string{"app": "gateway"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "gateway", Image: "gateway"}}},
						Status:     corev1.PodStatus{PodIP: "1.1.1.1", Phase: corev1.PodRunning},
					},
					&discoveryv1.EndpointSlice{
						ObjectMeta: metav1.ObjectMeta{
							Name: "ingressgateway", Namespace: "default",
							Labels: map[string]string{discoveryv1.LabelServiceName: "ingressgateway"},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{{
							Name: ptr.Of("tcp"), Port: ptr.Of(int32(443)), Protocol: ptr.Of(corev1.ProtocolTCP),
						}},
						Endpoints: []discoveryv1.Endpoint{{
							Addresses: []string{"1.1.1.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.Of(true)},
							TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "gateway", Namespace: "default"},
						}},
					},
				}
				for _, name := range []string{"exact-cert", "wildcard-cert"} {
					objects = append(objects, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
						Type:       corev1.SecretTypeTLS,
						Data:       map[string][]byte{corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key},
					})
				}

				server := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
					KubernetesObjects: objects,
					KubeClientModifier: func(client kube.Client) {
						// The fake server installs CRD metadata after this hook. Add the
						// real bundle version when those CRDs are created so the Gateway
						// API controller watches the stable TCPRoute and TLSRoute APIs.
						metadata := client.Metadata().(*metadatafake.FakeMetadataClient)
						metadata.PrependReactor("create", "customresourcedefinitions", func(action k8stesting.Action) (bool, runtime.Object, error) {
							crd := action.(k8stesting.CreateAction).GetObject().(*metav1.PartialObjectMetadata)
							if crd.Name == "tcproutes.gateway.networking.k8s.io" || crd.Name == "tlsroutes.gateway.networking.k8s.io" {
								if crd.Annotations == nil {
									crd.Annotations = map[string]string{}
								}
								crd.Annotations[gatewayapiconsts.BundleVersionAnnotation] = gatewayapiconsts.BundleVersion
							}
							return false, nil, nil
						})
					},
				})
				// Assert that the real controller accepted and converted both listeners
				// and both route kinds, rather than testing hand-built VirtualServices.
				store := server.ConfigGenTest.Env().ConfigStore
				if gateways := store.List(gvk.Gateway, "default"); len(gateways) != 2 {
					t.Fatalf("expected two converted Gateway listeners, got %d", len(gateways))
				}
				if routes := store.List(gvk.VirtualService, "default"); len(routes) != 3 {
					t.Fatalf("expected converted TCPRoute and both TLSRoute hostnames, got %d VirtualServices", len(routes))
				}
				proxy := server.SetupProxy(&model.Proxy{
					Type: model.Router, ConfigNamespace: "default", IPAddresses: []string{"1.1.1.1"},
					Labels:   map[string]string{"app": "gateway"},
					Metadata: &model.NodeMetadata{Namespace: "default", Labels: map[string]string{"app": "gateway"}},
				})
				sim := simulation.NewSimulation(t, server, proxy)
				xdstest.ValidateListeners(t, sim.Listeners)
				sim.Run(simulation.Call{
					Port: 443, Protocol: simulation.TCP, TLS: simulation.TLS, HostHeader: "foo.example.com",
				}).Matches(t, simulation.Result{
					ListenerMatched: "0.0.0.0_443",
					ClusterMatched:  "outbound|8080||tcp-backend.default.svc.cluster.local",
				})

				sim.Run(simulation.Call{
					Port: 443, Protocol: simulation.TCP, TLS: simulation.TLS, HostHeader: "bar.example.com",
				}).Matches(t, simulation.Result{
					ListenerMatched: "0.0.0.0_443",
					ClusterMatched:  "outbound|8080||tls-backend.default.svc.cluster.local",
				})

				var exactChains int
				for _, l := range sim.Listeners {
					if l.Name != "0.0.0.0_443" {
						continue
					}
					for _, chain := range l.FilterChains {
						if !slices.Contains(chain.GetFilterChainMatch().GetServerNames(), "foo.example.com") {
							continue
						}
						exactChains++
						context := &tls.DownstreamTlsContext{}
						if err := chain.GetTransportSocket().GetTypedConfig().UnmarshalTo(context); err != nil {
							t.Fatal(err)
						}
						secrets := context.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs()
						if len(secrets) != 1 || secrets[0].GetName() != "kubernetes-gateway://default/exact-cert" {
							t.Fatalf("exact listener must use its own TLS credential, got %v", secrets)
						}
					}
				}
				if exactChains != 1 {
					t.Fatalf("expected one exact listener filter chain, got %d", exactChains)
				}
			})
		}
	}
}
