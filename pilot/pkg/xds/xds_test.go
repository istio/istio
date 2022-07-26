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
	"os"
	"path"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
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

		endpoints := xdstest.ExtractLoadAssignments(s.Endpoints(proxy))
		if !listEqualUnordered(endpoints["outbound|80||app.com"], []string{"1.1.1.1:80"}) {
			t.Fatalf("expected 1.1.1.1, got %v", endpoints["outbound|80||app.com"])
		}

		assertListEqual(t, xdstest.ExtractListenerNames(s.Listeners(proxy)), []string{
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

		endpoints := xdstest.ExtractClusterEndpoints(s.Clusters(proxy))
		eps := endpoints["inbound|9080||"]
		if !listEqualUnordered(eps, []string{"/var/run/someuds.sock"}) {
			t.Fatalf("expected /var/run/someuds.sock, got %v", eps)
		}

		assertListEqual(t, xdstest.ExtractListenerNames(s.Listeners(proxy)), []string{
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

		assertListEqual(t, xdstest.ExtractClusterEndpoints(s.Clusters(proxy))["outbound|80||app.com"], []string{"app.com:80"})
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

		assertListEqual(t, xdstest.ExtractClusterEndpoints(s.Clusters(proxy))["outbound|80||app.com"], []string{"included.com:80"})
	})
}

func TestSidecarListeners(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := NewFakeDiscoveryServer(t, FakeOptions{})
		proxy := s.SetupProxy(&model.Proxy{
			IPAddresses: []string{"10.2.0.1"},
			ID:          "app3.testns",
		})
		structpath.ForProto(xdstest.ToDiscoveryResponse(s.Listeners(proxy))).
			Exists("{.resources[?(@.address.socketAddress.portValue==15001)]}").
			Select("{.resources[?(@.address.socketAddress.portValue==15001)]}").
			Equals("virtualOutbound", "{.name}").
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			Equals(wellknown.TCPProxy, "{.filterChains[1].filters[0].name}").
			Equals("PassthroughCluster", "{.filterChains[1].filters[0].typedConfig.cluster}").
			Equals("PassthroughCluster", "{.filterChains[1].filters[0].typedConfig.statPrefix}").
			Equals(true, "{.useOriginalDst}").
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
		structpath.ForProto(xdstest.ToDiscoveryResponse(s.Listeners(proxy))).
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
	assertListEqual(t, xdstest.ExtractListenerNames(listeners), []string{
		"0.0.0.0_80",
		"virtualInbound",
		"virtualOutbound",
	})

	expectedEgressCluster := "outbound|5000|shiny|foo.bar"

	found := false
	for _, f := range xdstest.ExtractListener("virtualOutbound", listeners).FilterChains {
		// We want to check the match all filter chain, as this is testing the fallback logic
		if f.FilterChainMatch != nil {
			continue
		}
		tcp := xdstest.ExtractTCPProxy(t, f)
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

func assertListEqual(t test.Failer, a, b []string) {
	t.Helper()
	if !listEqualUnordered(a, b) {
		t.Fatalf("Expected list %v to be equal to %v", a, b)
	}
}

func mustReadFile(t *testing.T, f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

func TestClusterLocal(t *testing.T) {
	tests := map[string]struct {
		fakeOpts            FakeOptions
		serviceCluster      string
		wantClusterLocal    map[cluster.ID][]string
		wantNonClusterLocal map[cluster.ID][]string
	}{
		// set up a k8s service in each cluster, with a pod in each cluster and a workloadentry in cluster-1
		"k8s service with pod and workloadentry": {
			fakeOpts: func() FakeOptions {
				k8sObjects := map[cluster.ID]string{
					"cluster-1": "",
					"cluster-2": "",
				}
				i := 1
				for range k8sObjects {
					clusterID := fmt.Sprintf("cluster-%d", i)
					k8sObjects[cluster.ID(clusterID)] = fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo-app
  name: echo-app
  namespace: default
spec:
  clusterIP: 1.2.3.4
  selector:
    app: echo-app
  ports:
  - name: grpc
    port: 7070
---
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: echo-app
  name: echo-app-%s
  namespace: default
---
apiVersion: v1
kind: Endpoints
metadata:
  name: echo-app
  namespace: default
  labels:
    app: echo-app
subsets:
- addresses:
  - ip: 10.0.0.%d
  ports:
  - name: grpc
    port: 7070
`, clusterID, i)
					i++
				}
				return FakeOptions{
					DefaultClusterName:              "cluster-1",
					KubernetesObjectStringByCluster: k8sObjects,
					ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: WorkloadEntry
metadata:
  name: echo-app
  namespace: default
spec:
  address: 10.1.1.1
  labels:
    app: echo-app
`,
				}
			}(),
			serviceCluster: "outbound|7070||echo-app.default.svc.cluster.local",
			wantClusterLocal: map[cluster.ID][]string{
				"cluster-1": {"10.0.0.1:7070", "10.1.1.1:7070"},
				"cluster-2": {"10.0.0.2:7070"},
			},
			wantNonClusterLocal: map[cluster.ID][]string{
				"cluster-1": {"10.0.0.1:7070", "10.1.1.1:7070", "10.0.0.2:7070"},
				"cluster-2": {"10.0.0.1:7070", "10.1.1.1:7070", "10.0.0.2:7070"},
			},
		},
		"serviceentry": {
			fakeOpts: FakeOptions{
				ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-svc-mongocluster
spec:
  hosts:
  - mymongodb.somedomain 
  addresses:
  - 192.192.192.192/24 # VIPs
  ports:
  - number: 27018
    name: mongodb
    protocol: MONGO
  location: MESH_INTERNAL
  resolution: STATIC
  endpoints:
  - address: 2.2.2.2
  - address: 3.3.3.3
`,
			},
			serviceCluster: "outbound|27018||mymongodb.somedomain",
			wantClusterLocal: map[cluster.ID][]string{
				"Kubernetes": {"2.2.2.2:27018", "3.3.3.3:27018"},
				"other":      {},
			},
			wantNonClusterLocal: map[cluster.ID][]string{
				"Kubernetes": {"2.2.2.2:27018", "3.3.3.3:27018"},
				"other":      {"2.2.2.2:27018", "3.3.3.3:27018"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			for _, local := range []bool{true, false} {
				name := "cluster-local"
				want := tt.wantClusterLocal
				if !local {
					name = "non-cluster-local"
					want = tt.wantNonClusterLocal
				}
				t.Run(name, func(t *testing.T) {
					meshConfig := mesh.DefaultMeshConfig()
					meshConfig.ServiceSettings = []*v1alpha1.MeshConfig_ServiceSettings{
						{Hosts: []string{"*"}, Settings: &v1alpha1.MeshConfig_ServiceSettings_Settings{
							ClusterLocal: local,
						}},
					}
					fakeOpts := tt.fakeOpts
					fakeOpts.MeshConfig = meshConfig
					s := NewFakeDiscoveryServer(t, fakeOpts)
					for clusterID := range want {
						p := s.SetupProxy(&model.Proxy{Metadata: &model.NodeMetadata{ClusterID: clusterID}})
						eps := xdstest.ExtractLoadAssignments(s.Endpoints(p))[tt.serviceCluster]
						if want := want[clusterID]; !listEqualUnordered(eps, want) {
							t.Errorf("got %v but want %v for %s", eps, want, clusterID)
						}
					}
				})
			}
		})
	}
}
