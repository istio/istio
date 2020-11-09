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

package v1alpha3_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/golang/protobuf/jsonpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
)

func flattenInstances(il ...[]*model.ServiceInstance) []*model.ServiceInstance {
	ret := []*model.ServiceInstance{}
	for _, i := range il {
		ret = append(ret, i...)
	}
	return ret
}

func makeInstances(proxy *model.Proxy, svc *model.Service, servicePort int, targetPort int) []*model.ServiceInstance {
	ret := []*model.ServiceInstance{}
	for _, p := range svc.Ports {
		if p.Port != servicePort {
			continue
		}
		ret = append(ret, &model.ServiceInstance{
			Service:     svc,
			ServicePort: p,
			Endpoint: &model.IstioEndpoint{
				Address:         proxy.IPAddresses[0],
				ServicePortName: p.Name,
				EndpointPort:    uint32(targetPort),
			},
		})
	}
	return ret
}

func TestInboundClusters(t *testing.T) {
	proxy := &model.Proxy{
		IPAddresses: []string{"1.2.3.4"},
	}
	proxy180 := &model.Proxy{
		IPAddresses: []string{"1.2.3.4"},
		Metadata:    &model.NodeMetadata{IstioVersion: "1.8.0"},
	}
	service := &model.Service{
		Hostname:    host.Name("backend.default.svc.cluster.local"),
		Address:     "1.1.1.1",
		ClusterVIPs: make(map[string]string),
		Ports: model.PortList{&model.Port{
			Name:     "default",
			Port:     80,
			Protocol: protocol.HTTP,
		}, &model.Port{
			Name:     "other",
			Port:     81,
			Protocol: protocol.HTTP,
		}},
		Resolution: model.ClientSideLB,
	}
	serviceAlt := &model.Service{
		Hostname:    host.Name("backend-alt.default.svc.cluster.local"),
		Address:     "1.1.1.2",
		ClusterVIPs: make(map[string]string),
		Ports: model.PortList{&model.Port{
			Name:     "default",
			Port:     80,
			Protocol: protocol.HTTP,
		}, &model.Port{
			Name:     "other",
			Port:     81,
			Protocol: protocol.HTTP,
		}},
		Resolution: model.ClientSideLB,
	}

	cases := []struct {
		name      string
		configs   []config.Config
		services  []*model.Service
		instances []*model.ServiceInstance
		// Assertions
		clusters    map[string][]string
		telemetry   map[string][]string
		legacyProxy bool
	}{
		// Proxy 1.8.1+ tests
		{name: "empty"},
		{name: "empty service", services: []*model.Service{service}},
		{
			name:      "single service, partial instance",
			services:  []*model.Service{service},
			instances: makeInstances(proxy, service, 80, 8080),
			clusters: map[string][]string{
				"inbound|8080||": {"127.0.0.1:8080"},
			},
			telemetry: map[string][]string{
				"inbound|8080||": {string(service.Hostname)},
			},
		},
		{
			name:     "single service, multiple instance",
			services: []*model.Service{service},
			instances: flattenInstances(
				makeInstances(proxy, service, 80, 8080),
				makeInstances(proxy, service, 81, 8081)),
			clusters: map[string][]string{
				"inbound|8080||": {"127.0.0.1:8080"},
				"inbound|8081||": {"127.0.0.1:8081"},
			},
			telemetry: map[string][]string{
				"inbound|8080||": {string(service.Hostname)},
				"inbound|8081||": {string(service.Hostname)},
			},
		},
		{
			name:     "multiple services with same service port, different target",
			services: []*model.Service{service, serviceAlt},
			instances: flattenInstances(
				makeInstances(proxy, service, 80, 8080),
				makeInstances(proxy, service, 81, 8081),
				makeInstances(proxy, serviceAlt, 80, 8082),
				makeInstances(proxy, serviceAlt, 81, 8083)),
			clusters: map[string][]string{
				"inbound|8080||": {"127.0.0.1:8080"},
				"inbound|8081||": {"127.0.0.1:8081"},
				"inbound|8082||": {"127.0.0.1:8082"},
				"inbound|8083||": {"127.0.0.1:8083"},
			},
			telemetry: map[string][]string{
				"inbound|8080||": {string(service.Hostname)},
				"inbound|8081||": {string(service.Hostname)},
				"inbound|8082||": {string(serviceAlt.Hostname)},
				"inbound|8083||": {string(serviceAlt.Hostname)},
			},
		},
		{
			name:     "multiple services with same service port and target",
			services: []*model.Service{service, serviceAlt},
			instances: flattenInstances(
				makeInstances(proxy, service, 80, 8080),
				makeInstances(proxy, service, 81, 8081),
				makeInstances(proxy, serviceAlt, 80, 8080),
				makeInstances(proxy, serviceAlt, 81, 8081)),
			clusters: map[string][]string{
				"inbound|8080||": {"127.0.0.1:8080"},
				"inbound|8081||": {"127.0.0.1:8081"},
			},
			telemetry: map[string][]string{
				"inbound|8080||": {string(serviceAlt.Hostname), string(service.Hostname)},
				"inbound|8081||": {string(serviceAlt.Hostname), string(service.Hostname)},
			},
		},
		{
			name: "ingress to same port",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "127.0.0.1:80",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:80"},
			},
		},
		{
			name: "ingress to different port",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "127.0.0.1:8080",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
			},
		},
		{
			name: "ingress to socket",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "unix:///socket",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"/socket"},
			},
		},
		{
			name: "multiple ingress",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.Port{
								Number:   80,
								Protocol: "HTTP",
								Name:     "http",
							},
							DefaultEndpoint: "127.0.0.1:8080",
						},
						{
							Port: &networking.Port{
								Number:   81,
								Protocol: "HTTP",
								Name:     "http",
							},
							DefaultEndpoint: "127.0.0.1:8080",
						},
					}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
				"inbound|81||": {"127.0.0.1:8080"},
			},
		},

		// Proxy 1.8.0 tests
		{
			name:        "single service, partial instance",
			legacyProxy: true,
			services:    []*model.Service{service},
			instances:   makeInstances(proxy180, service, 80, 8080),
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
			},
			telemetry: map[string][]string{
				"inbound|80||": {string(service.Hostname)},
			},
		},
		{
			name:        "single service, multiple instance",
			legacyProxy: true,
			services:    []*model.Service{service},
			instances: flattenInstances(
				makeInstances(proxy180, service, 80, 8080),
				makeInstances(proxy180, service, 81, 8081)),
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
				"inbound|81||": {"127.0.0.1:8081"},
			},
			telemetry: map[string][]string{
				"inbound|80||": {string(service.Hostname)},
				"inbound|81||": {string(service.Hostname)},
			},
		},
		{
			name:        "multiple services with same service port, different target",
			legacyProxy: true,
			services:    []*model.Service{service, serviceAlt},
			instances: flattenInstances(
				makeInstances(proxy180, service, 80, 8080),
				makeInstances(proxy180, service, 81, 8081),
				makeInstances(proxy180, serviceAlt, 80, 8082),
				makeInstances(proxy180, serviceAlt, 81, 8083)),
			clusters: map[string][]string{
				// BUG: we are missing 8080 and 8081. This is fixed for 1.8.1+
				"inbound|80||": {"127.0.0.1:8082"},
				"inbound|81||": {"127.0.0.1:8083"},
			},
			telemetry: map[string][]string{
				"inbound|80||": {string(serviceAlt.Hostname), string(service.Hostname)},
				"inbound|81||": {string(serviceAlt.Hostname), string(service.Hostname)},
			},
		},
		{
			name:        "multiple services with same service port and target",
			legacyProxy: true,
			services:    []*model.Service{service, serviceAlt},
			instances: flattenInstances(
				makeInstances(proxy180, service, 80, 8080),
				makeInstances(proxy180, service, 81, 8081),
				makeInstances(proxy180, serviceAlt, 80, 8080),
				makeInstances(proxy180, serviceAlt, 81, 8081)),
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
				"inbound|81||": {"127.0.0.1:8081"},
			},
			telemetry: map[string][]string{
				"inbound|80||": {string(serviceAlt.Hostname), string(service.Hostname)},
				"inbound|81||": {string(serviceAlt.Hostname), string(service.Hostname)},
			},
		},
		{
			name:        "ingress to same port",
			legacyProxy: true,
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "127.0.0.1:80",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:80"},
			},
		},
		{
			name:        "ingress to different port",
			legacyProxy: true,
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "127.0.0.1:8080",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
			},
		},
		{
			name:        "ingress to socket",
			legacyProxy: true,
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "unix:///socket",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"/socket"},
			},
		},
		{
			name:        "multiple ingress",
			legacyProxy: true,
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{
						{
							Port: &networking.Port{
								Number:   80,
								Protocol: "HTTP",
								Name:     "http",
							},
							DefaultEndpoint: "127.0.0.1:8080",
						},
						{
							Port: &networking.Port{
								Number:   81,
								Protocol: "HTTP",
								Name:     "http",
							},
							DefaultEndpoint: "127.0.0.1:8080",
						},
					}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"127.0.0.1:8080"},
				"inbound|81||": {"127.0.0.1:8080"},
			},
		},
	}
	for _, tt := range cases {
		name := tt.name
		if tt.legacyProxy {
			name += "-legacy"
		}
		t.Run(name, func(t *testing.T) {
			s := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
				Services:  tt.services,
				Instances: tt.instances,
				Configs:   tt.configs,
			})
			testProxy := proxy
			if tt.legacyProxy {
				testProxy = proxy180
			}
			sim := simulation.NewSimulationFromConfigGen(t, s, s.SetupProxy(testProxy))

			clusters := xdstest.FilterClusters(sim.Clusters, func(c *cluster.Cluster) bool {
				return strings.HasPrefix(c.Name, "inbound")
			})
			if len(s.PushContext().ProxyStatus) != 0 {
				// TODO make this fatal, once inbound conflict is silenced
				t.Logf("got unexpected error: %+v", s.PushContext().ProxyStatus)
			}
			cmap := xdstest.ExtractClusters(clusters)
			got := xdstest.MapKeys(cmap)

			// Check we have all expected clusters
			if !reflect.DeepEqual(xdstest.MapKeys(tt.clusters), got) {
				t.Errorf("expected clusters: %v, got: %v", xdstest.MapKeys(tt.clusters), got)
			}

			for name, c := range cmap {
				// Check the upstream endpoints match
				got := xdstest.ExtractLoadAssignments([]*endpoint.ClusterLoadAssignment{c.GetLoadAssignment()})[name]
				if !reflect.DeepEqual(tt.clusters[name], got) {
					t.Errorf("%v: expected endpoints %v, got %v", name, tt.clusters[name], got)
				}
				gotTelemetry := extractClusterMetadataServices(t, c)
				if !reflect.DeepEqual(tt.telemetry[name], gotTelemetry) {
					t.Errorf("%v: expected telemetry services %v, got %v", name, tt.telemetry[name], gotTelemetry)
				}

				if tt.legacyProxy {
					// This doesn't work with the legacy proxies which have issues (https://github.com/istio/istio/issues/29199)
					continue
				}
				// simulate an actual call, this ensures we are aligned with the inbound listener configuration
				_, _, _, port := model.ParseSubsetKey(name)
				sim.Run(simulation.Call{
					Port:     port,
					Address:  "1.2.3.4",
					CallMode: simulation.CallModeInbound,
				}).Matches(t, simulation.Result{
					ClusterMatched: fmt.Sprintf("inbound|%d||", port),
				})
			}
			if t.Failed() {
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Listeners)))
			}
		})
	}
}

type clusterServicesMetadata struct {
	Services []struct {
		Host      string
		Name      string
		Namespace string
	}
}

func extractClusterMetadataServices(t test.Failer, c *cluster.Cluster) []string {
	got := c.GetMetadata().GetFilterMetadata()[util.IstioMetadataKey]
	if got == nil {
		return nil
	}
	s, err := (&jsonpb.Marshaler{}).MarshalToString(got)
	if err != nil {
		t.Fatal(err)
	}
	meta := clusterServicesMetadata{}
	if err := json.Unmarshal([]byte(s), &meta); err != nil {
		t.Fatal(err)
	}
	res := []string{}
	for _, m := range meta.Services {
		res = append(res, m.Host)
	}
	return res
}

func TestPortLevelPeerAuthentication(t *testing.T) {
	pa := `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  selector:
    matchLabels:
      app: foo
  mtls:
    mode: STRICT
  portLevelMtls:
    9090:
      mode: DISABLE
---`
	sidecar := `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  labels:
    app: foo
  name: sidecar
spec:
  ingress:
  - defaultEndpoint: 127.0.0.1:8080
    port:
      name: tls
      number: 8080
      protocol: TCP
  - defaultEndpoint: 127.0.0.1:9090
    port:
      name: plaintext
      number: 9090
      protocol: TCP
  egress:
  - hosts:
    - "*/*"
  workloadSelector:
    labels:
      app: foo
---`
	partialSidecar := `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  labels:
    app: foo
  name: sidecar
spec:
  ingress:
  - defaultEndpoint: 127.0.0.1:8080
    port:
      name: tls
      number: 8080
      protocol: TCP
  egress:
  - hosts:
    - "*/*"
  workloadSelector:
    labels:
      app: foo
---`
	instancePorts := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - foo.bar
  endpoints:
  - address: 1.1.1.1
    labels:
      app: foo
  location: MESH_INTERNAL
  resolution: STATIC
  ports:
  - name: tls
    number: 8080
    protocol: TCP
  - name: plaintext
    number: 9090
    protocol: TCP
---`
	instanceNoPorts := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - foo.bar
  endpoints:
  - address: 1.1.1.1
    labels:
      app: foo
  location: MESH_INTERNAL
  resolution: STATIC
  ports:
  - name: random
    number: 5050
    protocol: TCP
---`
	mkCall := func(port int, tls simulation.TLSMode) simulation.Call {
		// TODO https://github.com/istio/istio/issues/28506 address should not be required here
		return simulation.Call{Port: port, CallMode: simulation.CallModeInbound, TLS: tls, Address: "1.1.1.1"}
	}
	cases := []struct {
		name   string
		config string
		calls  []simulation.Expect
	}{
		{
			name:   "service, no sidecar",
			config: pa + instancePorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				{
					Name:   "plaintext on plaintext port",
					Call:   mkCall(9090, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
				{
					Name:   "tls on plaintext port",
					Call:   mkCall(9090, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
			},
		},
		{
			name:   "service, full sidecar",
			config: pa + sidecar + instancePorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				{
					Name:   "plaintext on plaintext port",
					Call:   mkCall(9090, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
				{
					Name:   "tls on plaintext port",
					Call:   mkCall(9090, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
			},
		},
		{
			name:   "no service, no sidecar",
			config: pa + instanceNoPorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name: "tls on tls port",
					Call: mkCall(8080, simulation.MTLS),
					// no ports defined, so we will passthrough
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
				{
					Name: "plaintext on plaintext port",
					Call: mkCall(9090, simulation.Plaintext),
					// no ports defined, so we will fail. STRICT enforced
					// TODO(https://github.com/istio/istio/issues/27994) portLevelMtls should still apply
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// no ports defined, so we will fail. STRICT allows
					// TODO(https://github.com/istio/istio/issues/27994) portLevelMtls should still apply
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
		{
			name:   "no service, full sidecar",
			config: pa + sidecar + instanceNoPorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				{
					Name:   "plaintext on plaintext port",
					Call:   mkCall(9090, simulation.Plaintext),
					Result: simulation.Result{ClusterMatched: "inbound|9090||"},
				},
				{
					Name:   "tls on plaintext port",
					Call:   mkCall(9090, simulation.MTLS),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
			},
		},
		{
			name:   "service, partial sidecar",
			config: pa + partialSidecar + instancePorts,
			calls: []simulation.Expect{
				{
					Name:   "plaintext on tls port",
					Call:   mkCall(8080, simulation.Plaintext),
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name:   "tls on tls port",
					Call:   mkCall(8080, simulation.MTLS),
					Result: simulation.Result{ClusterMatched: "inbound|8080||"},
				},
				// Despite being defined in the Service, we get no filter chain since its not in Sidecar
				// Since we do not create configs on passthrough, our port level policy is not defined
				{
					Name: "plaintext on plaintext port",
					Call: mkCall(9090, simulation.Plaintext),
					// no ports defined, so we will fail. STRICT enforced
					// TODO(https://github.com/istio/istio/issues/27994) portLevelMtls should still apply
					Result: simulation.Result{Error: simulation.ErrNoFilterChain},
				},
				{
					Name: "tls on plaintext port",
					Call: mkCall(9090, simulation.MTLS),
					// no ports defined, so we will fail. STRICT allows
					// TODO(https://github.com/istio/istio/issues/27994) portLevelMtls should still apply
					Result: simulation.Result{ClusterMatched: "InboundPassthroughClusterIpv4"},
				},
			},
		},
	}
	proxy := &model.Proxy{Metadata: &model.NodeMetadata{Labels: map[string]string{"app": "foo"}}}
	for _, tt := range cases {
		runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
			name:   tt.name,
			config: tt.config,
			calls:  tt.calls,
		})
	}
}

func TestInbound(t *testing.T) {
	mtlsMode := func(m string) string {
		return fmt.Sprintf(`apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: %s
`, m)
	}
	svc := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - foo.bar
  endpoints:
  - address: 1.1.1.1
  location: MESH_INTERNAL
  resolution: STATIC
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: auto
    number: 81
---
`
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		name:   "disable",
		config: svc + mtlsMode("DISABLE"),
		calls: []simulation.Expect{
			{
				Name: "http inbound",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|80",
					ClusterMatched:     "inbound|80||",
				},
			},
			{
				Name: "auto port inbound",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto port2 inbound",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP2,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto tcp inbound",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "passthrough http",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched: "InboundPassthroughClusterIpv4",
				},
			},
			{
				Name: "passthrough tcp",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched: "InboundPassthroughClusterIpv4",
				},
			},
			{
				Name: "passthrough tls",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched: "InboundPassthroughClusterIpv4",
				},
			},
		},
	})

	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		name:   "permissive",
		config: svc + mtlsMode("PERMISSIVE"),
		calls: []simulation.Expect{
			{
				Name: "http port",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|80",
					ClusterMatched:     "inbound|80||",
				},
			},
			{
				Name: "http port tls",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					TLS:      simulation.TLS,
					Alpn:     "http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// This is expected. Protocol is explicitly declared HTTP but we send TLS traffic
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "http port mtls",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					TLS:      simulation.MTLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|80",
					ClusterMatched:     "inbound|80||",
				},
			},
			{
				Name: "auto port port",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto port port https",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					TLS:      simulation.TLS,
					Alpn:     "http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Passed through as plain tcp
					ClusterMatched:     "inbound|81||",
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port port https mtls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					TLS:      simulation.MTLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
					RouteMatched:       "default",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port tcp",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port tls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port mtls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					Alpn:     "istio",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "passthrough http",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound-catchall-http",
				},
			},
			{
				Name: "passthrough tcp",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound",
				},
			},
			{
				Name: "passthrough tls",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// TODO: This is a bug, see https://github.com/istio/istio/issues/26079#issuecomment-673699228
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "passthrough mtls",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Alpn:     "istio",
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "virtualInbound",
					StrictMatch:        true,
				},
			},
			{
				Name: "passthrough https",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Alpn:     "http/1.1",
					Protocol: simulation.HTTP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "virtualInbound-catchall-http",
					RouteMatched:       "default",
					VirtualHostMatched: "inbound|http|0",
					// TODO: This is a bug, see https://github.com/istio/istio/issues/26079#issuecomment-673699228
					// We should NOT be terminating TLs here, this is supposed to be passthrough. This breaks traffic
					// sending TLS with ALPN (ie curl, or many other clients) to a port not exposed in the service.
					StrictMatch: true,
				},
			},
		},
	})

	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		name:           "strict",
		config:         svc + mtlsMode("STRICT"),
		skipValidation: false,
		calls: []simulation.Expect{
			{
				Name: "http port",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Plaintext to strict, should fail
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "auto port http",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Plaintext to strict, should fail
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "auto port http mtls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					TLS:      simulation.MTLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto port tcp",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Plaintext to strict, should fail
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "passthrough plaintext",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Cannot send plaintext with strict
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "passthrough tls",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound",
				},
			},
			{
				Name: "passthrough mtls",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					Alpn:     "istio",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound",
				},
			},
			{
				Name: "passthrough mtls http",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					VirtualHostMatched: "inbound|http|0",
				},
			},
			{
				Name: "passthrough mtls http legacy",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					Alpn:     "http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					VirtualHostMatched: "inbound|http|0",
				},
			},
		},
	})
}

func TestHeadlessServices(t *testing.T) {
	ports := `
  - name: http
    port: 80
  - name: auto
    port: 81
  - name: tcp
    port: 82
  - name: tls
    port: 83
  - name: https
    port: 84`

	calls := []simulation.Expect{}
	for _, call := range []simulation.Call{
		{Address: "1.2.3.4", Port: 80, Protocol: simulation.HTTP, HostHeader: "headless.default.svc.cluster.local"},

		// Auto port should support any protocol
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, HostHeader: "headless.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "headless.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.TCP, HostHeader: "headless.default.svc.cluster.local"},

		{Address: "1.2.3.4", Port: 82, Protocol: simulation.TCP, HostHeader: "headless.default.svc.cluster.local"},

		// TODO: https://github.com/istio/istio/issues/27677 use short host name
		{Address: "1.2.3.4", Port: 83, Protocol: simulation.TCP, TLS: simulation.TLS, HostHeader: "headless.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 84, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "headless.default.svc.cluster.local"},
	} {
		calls = append(calls, simulation.Expect{
			Name: fmt.Sprintf("%s-%d", call.Protocol, call.Port),
			Call: call,
			Result: simulation.Result{
				ClusterMatched: fmt.Sprintf("outbound|%d||headless.default.svc.cluster.local", call.Port),
			},
		})
	}
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		kubeConfig: `apiVersion: v1
kind: Service
metadata:
  name: headless
  namespace: default
spec:
  clusterIP: None
  selector:
    app: headless
  ports:` + ports + `
---
apiVersion: v1
kind: Endpoints
metadata:
  name: headless
  namespace: default
subsets:
- addresses:
  - ip: 1.2.3.4
  ports:
` + ports,
		calls: calls,
	},
	)
}

func TestPassthroughTraffic(t *testing.T) {
	calls := map[string]simulation.Call{}
	for port := 80; port < 87; port++ {
		for _, call := range []simulation.Call{
			{Port: port, Protocol: simulation.HTTP, TLS: simulation.Plaintext, HostHeader: "foo"},
			{Port: port, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "foo"},
			{Port: port, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "foo", Alpn: "http/1.1"},
			{Port: port, Protocol: simulation.TCP, TLS: simulation.Plaintext, HostHeader: "foo"},
			{Port: port, Protocol: simulation.HTTP2, TLS: simulation.TLS, HostHeader: "foo"},
		} {
			suffix := ""
			if call.Alpn != "" {
				suffix = "-" + call.Alpn
			}
			calls[fmt.Sprintf("%v-%v-%v%v", call.Protocol, call.TLS, port, suffix)] = call
		}
	}
	ports := `
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: auto
    number: 81
  - name: tcp
    number: 82
    protocol: TCP
  - name: tls
    number: 83
    protocol: TLS
  - name: https
    number: 84
    protocol: HTTPS
  - name: grpc
    number: 85
    protocol: GRPC
  - name: h2
    number: 86
    protocol: HTTP2`

	isHTTPPort := func(p int) bool {
		switch p {
		case 80, 85, 86:
			return true
		default:
			return false
		}
	}
	isAutoPort := func(p int) bool {
		switch p {
		case 81:
			return true
		default:
			return false
		}
	}
	for _, tp := range []meshconfig.MeshConfig_OutboundTrafficPolicy_Mode{
		meshconfig.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY,
		meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY,
	} {
		t.Run(tp.String(), func(t *testing.T) {
			o := xds.FakeOptions{
				MeshConfig: func() *meshconfig.MeshConfig {
					m := mesh.DefaultMeshConfig()
					m.OutboundTrafficPolicy.Mode = tp
					return &m
				}(),
			}
			expectedCluster := map[meshconfig.MeshConfig_OutboundTrafficPolicy_Mode]string{
				meshconfig.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY: util.BlackHoleCluster,
				meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY:     util.PassthroughCluster,
			}[tp]
			t.Run("with VIP", func(t *testing.T) {
				testCalls := []simulation.Expect{}
				for name, call := range calls {
					e := simulation.Expect{
						Name: name,
						Call: call,
						Result: simulation.Result{
							ClusterMatched: expectedCluster,
						},
					}
					// For blackhole, we will 502 where possible instead of blackhole cluster
					// This only works for HTTP on HTTP
					if expectedCluster == util.BlackHoleCluster && call.IsHTTP() && isHTTPPort(call.Port) {
						e.Result.ClusterMatched = ""
						e.Result.VirtualHostMatched = util.BlackHole
					}
					testCalls = append(testCalls, e)
				}
				sort.Slice(testCalls, func(i, j int) bool {
					return testCalls[i].Name < testCalls[j].Name
				})
				runSimulationTest(t, nil, o,
					simulationTest{
						config: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - istio.io
  addresses: [1.2.3.4]
  location: MESH_EXTERNAL
  resolution: DNS` + ports,
						calls: testCalls,
					})
			})
			t.Run("without VIP", func(t *testing.T) {
				testCalls := []simulation.Expect{}
				for name, call := range calls {
					e := simulation.Expect{
						Name: name,
						Call: call,
						Result: simulation.Result{
							ClusterMatched: expectedCluster,
						},
					}
					// For blackhole, we will 502 where possible instead of blackhole cluster
					// This only works for HTTP on HTTP
					if expectedCluster == util.BlackHoleCluster && call.IsHTTP() && (isHTTPPort(call.Port) || isAutoPort(call.Port)) {
						e.Result.ClusterMatched = ""
						e.Result.VirtualHostMatched = util.BlackHole
					}
					// TCP without a VIP will capture everything.
					// Auto without a VIP is similar, but HTTP happens to work because routing is done on header
					if call.Port == 82 || (call.Port == 81 && !call.IsHTTP()) {
						e.Result.Error = nil
						e.Result.ClusterMatched = ""
					}
					testCalls = append(testCalls, e)
				}
				sort.Slice(testCalls, func(i, j int) bool {
					return testCalls[i].Name < testCalls[j].Name
				})
				runSimulationTest(t, nil, o,
					simulationTest{
						config: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  resolution: DNS` + ports,
						calls: testCalls,
					})
			})
		})
	}
}

func TestLoop(t *testing.T) {
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		calls: []simulation.Expect{
			{
				Name: "direct request to outbound port",
				Call: simulation.Call{
					Port:     15001,
					Protocol: simulation.TCP,
				},
				Result: simulation.Result{
					// This request should be blocked
					ClusterMatched: "BlackHoleCluster",
				},
			},
			{
				Name: "direct request to inbound port",
				Call: simulation.Call{
					Port:     15006,
					Protocol: simulation.TCP,
				},
				Result: simulation.Result{
					// This request should be blocked
					ClusterMatched: "BlackHoleCluster",
				},
			},
		},
	})
}
