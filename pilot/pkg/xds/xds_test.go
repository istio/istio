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
	"fmt"
	"io/ioutil"
	"path"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/structpath"
)

type SidecarTestConfig struct {
	ImportedNamespaces []string
	Resolution         string
	IngressListener    bool
}

var scopeConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
  namespace:  app
spec:
{{- if .IngressListener }}
  ingress:
    - port:
        number: 9080
        protocol: HTTP
        name: custom-http
      defaultEndpoint: unix:///var/run/someuds.sock
{{- end }}
  egress:
    - hosts:
{{ range $i, $ns := .ImportedNamespaces }}
      - {{$ns}}
{{ end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: app
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 1.1.1.1
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded
  namespace: excluded
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: excluded.com
{{- else }}
  - address: 9.9.9.9
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: included
  namespace: included
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: included.com
{{- else }}
  - address: 2.2.2.2
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app-https
  namespace: app
spec:
  hosts:
  - app.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded-https
  namespace: excluded
spec:
  hosts:
  - app.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 4431
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`

// TestServiceScoping is a high level test ensuring the Sidecar scoping works correctly, especially when
// there are multiple hostnames that are in different namespaces.
func TestServiceScoping(t *testing.T) {
	baseProxy := func() *model.Proxy {
		return &model.Proxy{
			Metadata:        &model.NodeMetadata{},
			ID:              "app.app",
			Type:            model.SidecarProxy,
			IPAddresses:     []string{"1.1.1.1"},
			ConfigNamespace: "app",
		}
	}

	t.Run("STATIC", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{
			ConfigString: scopeConfig,
			ConfigTemplateInput: SidecarTestConfig{
				ImportedNamespaces: []string{"./*", "included/*"},
				Resolution:         "STATIC",
			},
		})
		proxy := s.SetupProxy(baseProxy())

		endpoints := ExtractEndpoints(s.Endpoints(proxy))
		if !listEqualUnordered(endpoints["outbound|80||app.com"], []string{"1.1.1.1:80"}) {
			t.Fatalf("expected 1.1.1.1, got %v", endpoints["outbound|80||app.com"])
		}

		assertListEqual(t, ExtractListenerNames(s.Listeners(proxy)), []string{
			"0.0.0.0_80",
			"5.5.5.5_443",
			"virtualInbound",
			"virtualOutbound",
		})
	})

	t.Run("Ingress Listener", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{
			ConfigString: scopeConfig,
			ConfigTemplateInput: SidecarTestConfig{
				ImportedNamespaces: []string{"./*", "included/*"},
				Resolution:         "STATIC",
				IngressListener:    true,
			},
		})
		p := baseProxy()
		// Change the node's IP so that it does not match with any service entry
		p.IPAddresses = []string{"100.100.100.100"}
		proxy := s.SetupProxy(p)

		endpoints := ExtractClusterEndpoints(s.Clusters(proxy))
		eps := endpoints["inbound|9080|custom-http|sidecar.app"]
		if !listEqualUnordered(eps, []string{"/var/run/someuds.sock"}) {
			t.Fatalf("expected /var/run/someuds.sock, got %v", eps)
		}

		assertListEqual(t, ExtractListenerNames(s.Listeners(proxy)), []string{
			"0.0.0.0_80",
			"5.5.5.5_443",
			"virtualInbound",
			"virtualOutbound",
		})
	})

	t.Run("DNS", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{
			ConfigString: scopeConfig,
			ConfigTemplateInput: SidecarTestConfig{
				ImportedNamespaces: []string{"./*", "included/*"},
				Resolution:         "DNS",
			},
		})
		proxy := s.SetupProxy(baseProxy())

		assertListEqual(t, ExtractClusterEndpoints(s.Clusters(proxy))["outbound|80||app.com"], []string{"app.com:80"})
	})

	t.Run("DNS no self import", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{
			ConfigString: scopeConfig,
			ConfigTemplateInput: SidecarTestConfig{
				ImportedNamespaces: []string{"included/*"},
				Resolution:         "DNS",
			},
		})
		proxy := s.SetupProxy(baseProxy())

		assertListEqual(t, ExtractClusterEndpoints(s.Clusters(proxy))["outbound|80||app.com"], []string{"included.com:80"})
	})
}

func TestSidecarListeners(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{})
		proxy := s.SetupProxy(&model.Proxy{
			IPAddresses: []string{"10.2.0.1"},
			ID:          "app3.testns",
		})
		structpath.ForProto(ToDiscoveryResponse(s.Listeners(proxy))).
			Exists("{.resources[?(@.address.socketAddress.portValue==15001)]}").
			Select("{.resources[?(@.address.socketAddress.portValue==15001)]}").
			Equals("virtualOutbound", "{.name}").
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			Equals(wellknown.TCPProxy, "{.filterChains[0].filters[0].name}").
			Equals("PassthroughCluster", "{.filterChains[0].filters[0].typedConfig.cluster}").
			Equals("PassthroughCluster", "{.filterChains[0].filters[0].typedConfig.statPrefix}").
			Equals(true, "{.hiddenEnvoyDeprecatedUseOriginalDst}").
			CheckOrFail(t)
	})

	t.Run("mongo", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{
			ConfigString: mustReadFile(t, "./tests/testdata/config/se-example.yaml"),
		})
		proxy := s.SetupProxy(&model.Proxy{
			IPAddresses: []string{"10.2.0.1"},
			ID:          "app3.testns",
		})
		structpath.ForProto(ToDiscoveryResponse(s.Listeners(proxy))).
			Exists("{.resources[?(@.address.socketAddress.portValue==27018)]}").
			Select("{.resources[?(@.address.socketAddress.portValue==27018)]}").
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			// Example doing a struct comparison, note the pain with oneofs....
			Equals(&core.SocketAddress{
				Address: "0.0.0.0",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: uint32(27018),
				},
			}, "{.address.socketAddress}").
			Select("{.filterChains[0].filters[0]}").
			Equals("envoy.mongo_proxy", "{.name}").
			Select("{.typedConfig}").
			Exists("{.statPrefix}").
			CheckOrFail(t)
	})
}

func TestMeshNetworking(t *testing.T) {
	ingresses := []*corev1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "2.2.2.2"}}},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "istio-ingressgateway",
				Namespace:   "istio-system",
				Annotations: map[string]string{kube.NodeSelectorAnnotation: "{}"},
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 15443, NodePort: 25443}}},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "8.8.8.8"}}}, // 2.2.2.2 should be found on the node
			},
		},
	}

	meshNetworkConfigs := map[string]*meshconfig.MeshNetworks{
		"gateway-address": {Networks: map[string]*meshconfig.Network{
			// Explicitly set address
			"network-1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{{
					Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
				}},
				Gateways: []*meshconfig.Network_IstioNetworkGateway{{
					Gw:   &meshconfig.Network_IstioNetworkGateway_Address{Address: "2.2.2.2"},
					Port: 15443,
				}},
			},
		}},
		"gateway-registryServiceName": {Networks: map[string]*meshconfig.Network{
			// Address from service
			"network-1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{{
					Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
				}},
				Gateways: []*meshconfig.Network_IstioNetworkGateway{{
					Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
						RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
					},
					Port: 15443,
				}},
			},
		}},
	}

	for _, ingr := range ingresses {
		t.Run(string(ingr.Spec.Type), func(t *testing.T) {
			var k8sObjects []runtime.Object
			k8sObjects = append(k8sObjects,
				// NodePort ingress needs this
				&corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "2.2.2.2"}}}},
				ingr)
			k8sObjects = append(k8sObjects, fakePodService(fakeServiceOpts{name: "kubeapp", ns: "pod", ip: "10.10.10.20"})...)
			for name, networkConfig := range meshNetworkConfigs {
				t.Run(name, func(t *testing.T) {
					s := NewFakeDiscoveryServer(t, FakeOptions{
						KubernetesObjects: k8sObjects,
						ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se-pod
  namespace: pod
spec:
  hosts:
  - se-pod.pod.svc.cluster.local
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 10.10.10.30
    serviceAccount: svc-acc
    labels:
      app: se-pod
    network: network-1
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: vm
spec:
  hosts:
  - httpbin.com
  ports:
  - number: 7070
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 10.10.10.10
    labels:
      app: httpbin
    network: vm
`,
						NetworksWatcher: mesh.NewFixedNetworksWatcher(networkConfig),
					})
					se := s.SetupProxy(&model.Proxy{
						ID: "se-pod.pod",
						Metadata: &model.NodeMetadata{
							Network:   "network-1",
							ClusterID: "Kubernetes",
							Labels:    labels.Instance{"app": "se-pod"},
						},
					})
					pod := s.SetupProxy(&model.Proxy{
						ID: "kubeapp-1234.pod",
						Metadata: &model.NodeMetadata{
							Network:   "network-1",
							ClusterID: "Kubernetes",
							Labels:    labels.Instance{"app": "kubeapp"},
						},
					})
					vm := s.SetupProxy(&model.Proxy{
						ID:              "vm",
						IPAddresses:     []string{"10.10.10.10"},
						ConfigNamespace: "default",
						Metadata: &model.NodeMetadata{
							Network:          "vm",
							InterceptionMode: "NONE",
						},
					})
					gw := s.SetupProxy(&model.Proxy{
						ID:              "gw",
						IPAddresses:     []string{"2.2.2.2"},
						ConfigNamespace: "default",
						Metadata: &model.NodeMetadata{
							Network:              "vm", // should only see the VM
							InterceptionMode:     "NONE",
							RequestedNetworkView: []string{"vm"},
						},
					})

					gatewayPort := "15443"
					if ingr.Spec.Type == corev1.ServiceTypeNodePort && name == "gateway-registryServiceName" {
						gatewayPort = "25443"
					}
					tests := []struct {
						p      *model.Proxy
						expect map[string][]string
					}{
						{
							p: pod,
							expect: map[string][]string{
								"outbound|7070||httpbin.com":                 {"10.10.10.10:7070"},
								"outbound|80||kubeapp.pod.svc.cluster.local": {"10.10.10.20:80"},
								"outbound|80||se-pod.pod.svc.cluster.local":  {"10.10.10.30:80"},
							},
						},
						{
							p: se,
							expect: map[string][]string{
								"outbound|7070||httpbin.com":                 {"10.10.10.10:7070"},
								"outbound|80||kubeapp.pod.svc.cluster.local": {"10.10.10.20:80"},
								"outbound|80||se-pod.pod.svc.cluster.local":  {"10.10.10.30:80"},
							},
						},
						{
							p: vm,
							expect: map[string][]string{
								"outbound|7070||httpbin.com":                 {"10.10.10.10:7070"},
								"outbound|80||kubeapp.pod.svc.cluster.local": {"2.2.2.2:" + gatewayPort},
								"outbound|80||se-pod.pod.svc.cluster.local":  {"2.2.2.2:" + gatewayPort},
							},
						},
						{
							p: gw,
							expect: map[string][]string{
								"outbound|7070||httpbin.com": {"10.10.10.10:7070"},
								// Network view will filter these out
								"outbound|80||kubeapp.pod.svc.cluster.local": {},
								"outbound|80||se-pod.pod.svc.cluster.local":  {},
							},
						},
					}

					for _, tt := range tests {
						eps := ExtractEndpoints(s.Endpoints(tt.p))
						for c, ip := range tt.expect {
							t.Run(fmt.Sprintf("%s from %s", c, tt.p.ID), func(t *testing.T) {
								assertListEqual(t, eps[c], ip)
							})
						}
					}
				})
			}
		})
	}
}

func TestEgressProxy(t *testing.T) {
	s := NewFakeDiscoveryServer(t, FakeOptions{
		ConfigString: `
# Add a random endpoint, otherwise there will be no routes to check
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: pod
spec:
  hosts:
  - pod.pod.svc.cluster.local
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 10.10.10.20
---
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar-with-egressproxy
  namespace: app
spec:
  outboundTrafficPolicy:
    mode: ALLOW_ANY
    egressProxy:
      host: foo.bar
      subset: shiny
      port:
        number: 5000
  egress:
  - hosts:
    - "*/*"
`,
	})
	proxy := s.SetupProxy(&model.Proxy{
		ConfigNamespace: "app",
	})

	listeners := s.Listeners(proxy)
	assertListEqual(t, ExtractListenerNames(listeners), []string{
		"0.0.0.0_80",
		"virtualInbound",
		"virtualOutbound",
	})

	expectedEgressCluster := "outbound|5000|shiny|foo.bar"

	found := false
	for _, f := range ExtractListener("virtualOutbound", listeners).FilterChains {
		// We want to check the match all filter chain, as this is testing the fallback logic
		if f.FilterChainMatch != nil {
			continue
		}
		tcp := ExtractTCPProxy(t, f)
		if tcp.GetCluster() != expectedEgressCluster {
			t.Fatalf("got unexpected fallback destination: %v, want %v", tcp.GetCluster(), expectedEgressCluster)
		}
		found = true
	}
	if !found {
		t.Fatalf("failed to find tcp proxy")
	}

	found = false
	routes := s.Routes(proxy)
	for _, rc := range routes {
		for _, vh := range rc.GetVirtualHosts() {
			if vh.GetName() == "allow_any" {
				for _, r := range vh.GetRoutes() {
					if expectedEgressCluster == r.GetRoute().GetCluster() {
						found = true
						break
					}
				}
				break
			}
		}
	}
	if !found {
		t.Fatalf("failed to find expected fallthrough route")
	}
}

type fakeServiceOpts struct {
	name         string
	ns           string
	ip           string
	podLabels    labels.Instance
	servicePorts []corev1.ServicePort
}

// fakePodService build the minimal k8s objects required to discover one endpoint.
// If servicePorts is empty a default of http-80 will be used.
func fakePodService(opts fakeServiceOpts) []runtime.Object {
	baseMeta := metav1.ObjectMeta{
		Name: opts.name,
		Labels: labels.Instance{
			"app":         opts.name,
			label.TLSMode: model.IstioMutualTLSModeLabel,
		},
		Namespace: opts.ns,
	}
	podMeta := baseMeta
	podMeta.Name = opts.name + "-" + rand.String(4)
	for k, v := range opts.podLabels {
		podMeta.Labels[k] = v
	}

	if len(opts.servicePorts) == 0 {
		opts.servicePorts = []corev1.ServicePort{{
			Port:     80,
			Name:     "http",
			Protocol: corev1.ProtocolTCP,
		}}
	}
	var endpointPorts []corev1.EndpointPort
	for _, sp := range opts.servicePorts {
		endpointPorts = append(endpointPorts, corev1.EndpointPort{
			Name:        sp.Name,
			Port:        sp.Port,
			Protocol:    sp.Protocol,
			AppProtocol: sp.AppProtocol,
		})
	}

	return []runtime.Object{
		&corev1.Pod{
			ObjectMeta: podMeta,
		},
		&corev1.Service{
			ObjectMeta: baseMeta,
			Spec: corev1.ServiceSpec{
				ClusterIP: "1.2.3.4", // just can't be 0.0.0.0/ClusterIPNone
				Selector:  baseMeta.Labels,
				Ports:     opts.servicePorts,
			},
		},
		&corev1.Endpoints{
			ObjectMeta: baseMeta,
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: opts.ip,
					TargetRef: &corev1.ObjectReference{
						APIVersion: "v1",
						Kind:       "Pod",
						Name:       podMeta.Name,
						Namespace:  podMeta.Namespace,
					},
				}},
				Ports: endpointPorts,
			}},
		},
	}
}

func assertListEqual(t test.Failer, a, b []string) {
	t.Helper()
	if !listEqualUnordered(a, b) {
		t.Fatalf("Expected list %v to be equal to %v", a, b)
	}
}

func mustReadFile(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}
