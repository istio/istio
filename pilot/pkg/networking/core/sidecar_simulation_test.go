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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/protomarshal"
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
				Addresses:       proxy.IPAddresses,
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
		Metadata:    &model.NodeMetadata{},
	}
	service := &model.Service{
		Hostname:       host.Name("backend.default.svc.cluster.local"),
		DefaultAddress: "1.1.1.1",
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
		Hostname:       host.Name("backend-alt.default.svc.cluster.local"),
		DefaultAddress: "1.1.1.2",
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
		clusters                  map[string][]string
		telemetry                 map[string][]string
		proxy                     *model.Proxy
		disableInboundPassthrough bool
	}{
		// Proxy 1.8.1+ tests
		{name: "empty"},
		{name: "empty service", services: []*model.Service{service}},
		{
			name:      "single service, partial instance",
			services:  []*model.Service{service},
			instances: makeInstances(proxy, service, 80, 8080),
			clusters: map[string][]string{
				"inbound|8080||": nil,
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
				"inbound|8080||": nil,
				"inbound|8081||": nil,
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
				"inbound|8080||": nil,
				"inbound|8081||": nil,
				"inbound|8082||": nil,
				"inbound|8083||": nil,
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
				"inbound|8080||": nil,
				"inbound|8081||": nil,
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
						Port: &networking.SidecarPort{
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
						Port: &networking.SidecarPort{
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
			name: "ingress to instance IP",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.SidecarPort{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
						DefaultEndpoint: "0.0.0.0:8080",
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": {"1.2.3.4:8080"},
			},
		},
		{
			name: "ingress without default endpoint",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.SidecarPort{
							Number:   80,
							Protocol: "HTTP",
							Name:     "http",
						},
					}}},
				},
			},
			clusters: map[string][]string{
				"inbound|80||": nil,
			},
		},
		{
			name: "ingress to socket",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.SidecarPort{
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
							Port: &networking.SidecarPort{
								Number:   80,
								Protocol: "HTTP",
								Name:     "http",
							},
							DefaultEndpoint: "127.0.0.1:8080",
						},
						{
							Port: &networking.SidecarPort{
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
		// Disable inbound passthrough
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
			disableInboundPassthrough: true,
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
			disableInboundPassthrough: true,
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
			disableInboundPassthrough: true,
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
			disableInboundPassthrough: true,
		},
	}
	for _, tt := range cases {
		name := tt.name
		if tt.proxy == nil {
			tt.proxy = proxy
		} else {
			name += "-" + tt.proxy.Metadata.IstioVersion
		}

		t.Run(name, func(t *testing.T) {
			s := core.NewConfigGenTest(t, core.TestOptions{
				Services:  tt.services,
				Instances: tt.instances,
				Configs:   tt.configs,
				MeshConfig: func() *meshconfig.MeshConfig {
					m := mesh.DefaultMeshConfig()
					if tt.disableInboundPassthrough {
						m.InboundTrafficPolicy.Mode = meshconfig.MeshConfig_InboundTrafficPolicy_LOCALHOST
					}
					return m
				}(),
			})
			sim := simulation.NewSimulationFromConfigGen(t, s, s.SetupProxy(tt.proxy))

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

			for cname, c := range cmap {
				// Check the upstream endpoints match
				got := xdstest.ExtractLoadAssignments([]*endpoint.ClusterLoadAssignment{c.GetLoadAssignment()})[cname]
				if !reflect.DeepEqual(tt.clusters[cname], got) {
					t.Errorf("%v: expected endpoints %v, got %v", cname, tt.clusters[cname], got)
				}
				gotTelemetry := extractClusterMetadataServices(t, c)
				if !reflect.DeepEqual(tt.telemetry[cname], gotTelemetry) {
					t.Errorf("%v: expected telemetry services %v, got %v", cname, tt.telemetry[cname], gotTelemetry)
				}

				// simulate an actual call, this ensures we are aligned with the inbound listener configuration
				_, _, hostname, port := model.ParseSubsetKey(cname)
				if tt.proxy.Metadata.IstioVersion != "" {
					// This doesn't work with the legacy proxies which have issues (https://github.com/istio/istio/issues/29199)
					for _, i := range tt.instances {
						if len(hostname) > 0 && i.Service.Hostname != hostname {
							continue
						}
						if i.ServicePort.Port == port {
							port = int(i.Endpoint.EndpointPort)
						}
					}
				}
				sim.Run(simulation.Call{
					Port:     port,
					Protocol: simulation.HTTP,
					Address:  "1.2.3.4",
					CallMode: simulation.CallModeInbound,
				}).Matches(t, simulation.Result{
					ClusterMatched: cname,
				})
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
	s, err := protomarshal.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	meta := clusterServicesMetadata{}
	if err := json.Unmarshal(s, &meta); err != nil {
		t.Fatal(err)
	}
	res := []string{}
	for _, m := range meta.Services {
		res = append(res, m.Host)
	}
	return res
}

func mtlsMode(m string) string {
	return fmt.Sprintf(`apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: %s
`, m)
}

func TestInbound(t *testing.T) {
	svc := `
apiVersion: networking.istio.io/v1
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
  - name: tcp
    number: 70
    protocol: TCP
  - name: http
    number: 80
    protocol: HTTP
  - name: auto
    number: 81
---
`
	cases := []struct {
		Name       string
		Call       simulation.Call
		Disabled   simulation.Result
		Permissive simulation.Result
		Strict     simulation.Result
	}{
		{
			Name: "tcp",
			Call: simulation.Call{
				Port:     70,
				Protocol: simulation.TCP,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Permissive: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Strict: simulation.Result{
				// Plaintext to strict, should fail
				Error: simulation.ErrNoFilterChain,
			},
		},
		{
			Name: "http to tcp",
			Call: simulation.Call{
				Port:     70,
				Protocol: simulation.HTTP,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Permissive: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Strict: simulation.Result{
				// Plaintext to strict, should fail
				Error: simulation.ErrNoFilterChain,
			},
		},
		{
			Name: "tls to tcp",
			Call: simulation.Call{
				Port:     70,
				Protocol: simulation.TCP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Permissive: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Strict: simulation.Result{
				// TLS, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "https to tcp",
			Call: simulation.Call{
				Port:     70,
				Protocol: simulation.HTTP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Permissive: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Strict: simulation.Result{
				// TLS, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "mtls tcp to tcp",
			Call: simulation.Call{
				Port:     70,
				Protocol: simulation.TCP,
				TLS:      simulation.MTLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// This is probably a user error, but there is no reason we should block mTLS traffic
				// we just will not terminate it
				ClusterMatched: "inbound|70||",
			},
			Permissive: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Strict: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
		},
		{
			Name: "mtls http to tcp",
			Call: simulation.Call{
				Port:     70,
				Protocol: simulation.HTTP,
				TLS:      simulation.MTLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// This is probably a user error, but there is no reason we should block mTLS traffic
				// we just will not terminate it
				ClusterMatched: "inbound|70||",
			},
			Permissive: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
			Strict: simulation.Result{
				ClusterMatched: "inbound|70||",
			},
		},
		{
			Name: "http",
			Call: simulation.Call{
				Port:     80,
				Protocol: simulation.HTTP,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				VirtualHostMatched: "inbound|http|80",
				ClusterMatched:     "inbound|80||",
			},
			Permissive: simulation.Result{
				VirtualHostMatched: "inbound|http|80",
				ClusterMatched:     "inbound|80||",
			},
			Strict: simulation.Result{
				// Plaintext to strict, should fail
				Error: simulation.ErrNoFilterChain,
			},
		},
		{
			Name: "tls to http",
			Call: simulation.Call{
				Port:     80,
				Protocol: simulation.TCP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// TLS is not terminated, so we will attempt to decode as HTTP and fail
				Error: simulation.ErrProtocolError,
			},
			Permissive: simulation.Result{
				// This could also be a protocol error. In the current implementation, we choose not
				// to create a match since if we did it would just be rejected in HCM; no match
				// is more performant
				Error: simulation.ErrNoFilterChain,
			},
			Strict: simulation.Result{
				// TLS, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "https to http",
			Call: simulation.Call{
				Port:     80,
				Protocol: simulation.HTTP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// TLS is not terminated, so we will attempt to decode as HTTP and fail
				Error: simulation.ErrProtocolError,
			},
			Permissive: simulation.Result{
				// This could also be a protocol error. In the current implementation, we choose not
				// to create a match since if we did it would just be rejected in HCM; no match
				// is more performant
				Error: simulation.ErrNoFilterChain,
			},
			Strict: simulation.Result{
				// TLS, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "mtls to http",
			Call: simulation.Call{
				Port:     80,
				Protocol: simulation.HTTP,
				TLS:      simulation.MTLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// TLS is not terminated, so we will attempt to decode as HTTP and fail
				Error: simulation.ErrProtocolError,
			},
			Permissive: simulation.Result{
				VirtualHostMatched: "inbound|http|80",
				ClusterMatched:     "inbound|80||",
			},
			Strict: simulation.Result{
				VirtualHostMatched: "inbound|http|80",
				ClusterMatched:     "inbound|80||",
			},
		},
		{
			Name: "tcp to http",
			Call: simulation.Call{
				Port:     80,
				Protocol: simulation.TCP,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// Expected, the port only supports HTTP
				Error: simulation.ErrProtocolError,
			},
			Permissive: simulation.Result{
				// Expected, the port only supports HTTP
				Error: simulation.ErrProtocolError,
			},
			Strict: simulation.Result{
				// Plaintext to strict fails
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
			Disabled: simulation.Result{
				VirtualHostMatched: "inbound|http|81",
				ClusterMatched:     "inbound|81||",
			},
			Permissive: simulation.Result{
				VirtualHostMatched: "inbound|http|81",
				ClusterMatched:     "inbound|81||",
			},
			Strict: simulation.Result{
				// Plaintext to strict fails
				Error: simulation.ErrNoFilterChain,
			},
		},
		{
			Name: "auto port http2",
			Call: simulation.Call{
				Port:     81,
				Protocol: simulation.HTTP2,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				VirtualHostMatched: "inbound|http|81",
				ClusterMatched:     "inbound|81||",
			},
			Permissive: simulation.Result{
				VirtualHostMatched: "inbound|http|81",
				ClusterMatched:     "inbound|81||",
			},
			Strict: simulation.Result{
				// Plaintext to strict fails
				Error: simulation.ErrNoFilterChain,
			},
		},
		{
			Name: "auto port tcp",
			Call: simulation.Call{
				Port:     81,
				Protocol: simulation.TCP,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Permissive: simulation.Result{
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Strict: simulation.Result{
				// Plaintext to strict fails
				Error: simulation.ErrNoFilterChain,
			},
		},
		{
			Name: "tls to auto port",
			Call: simulation.Call{
				Port:     81,
				Protocol: simulation.TCP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// Should go through the TCP chains
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Permissive: simulation.Result{
				// Should go through the TCP chains
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Strict: simulation.Result{
				// Tls, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "https to auto port",
			Call: simulation.Call{
				Port:     81,
				Protocol: simulation.HTTP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// Should go through the TCP chains
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Permissive: simulation.Result{
				// Should go through the TCP chains
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Strict: simulation.Result{
				// Tls, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "mtls tcp to auto port",
			Call: simulation.Call{
				Port:     81,
				Protocol: simulation.TCP,
				TLS:      simulation.MTLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// This is probably a user error, but there is no reason we should block mTLS traffic
				// we just will not terminate it
				ClusterMatched: "inbound|81||",
			},
			Permissive: simulation.Result{
				// Should go through the TCP chains
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
			Strict: simulation.Result{
				// Should go through the TCP chains
				ListenerMatched:    "virtualInbound",
				FilterChainMatched: "0.0.0.0_81",
				ClusterMatched:     "inbound|81||",
				StrictMatch:        true,
			},
		},
		{
			Name: "mtls http to auto port",
			Call: simulation.Call{
				Port:     81,
				Protocol: simulation.HTTP,
				TLS:      simulation.MTLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				// This is probably a user error, but there is no reason we should block mTLS traffic
				// we just will not terminate it
				ClusterMatched: "inbound|81||",
			},
			Permissive: simulation.Result{
				// Should go through the HTTP chains
				VirtualHostMatched: "inbound|http|81",
				ClusterMatched:     "inbound|81||",
			},
			Strict: simulation.Result{
				// Should go through the HTTP chains
				VirtualHostMatched: "inbound|http|81",
				ClusterMatched:     "inbound|81||",
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
			Disabled: simulation.Result{
				ClusterMatched:     "InboundPassthroughCluster",
				FilterChainMatched: "virtualInbound-catchall-http",
			},
			Permissive: simulation.Result{
				ClusterMatched:     "InboundPassthroughCluster",
				FilterChainMatched: "virtualInbound-catchall-http",
			},
			Strict: simulation.Result{
				// Plaintext to strict fails
				Error: simulation.ErrNoFilterChain,
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
			Disabled: simulation.Result{
				ClusterMatched:     "InboundPassthroughCluster",
				FilterChainMatched: "virtualInbound",
			},
			Permissive: simulation.Result{
				ClusterMatched:     "InboundPassthroughCluster",
				FilterChainMatched: "virtualInbound",
			},
			Strict: simulation.Result{
				// Plaintext to strict fails
				Error: simulation.ErrNoFilterChain,
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
			Disabled: simulation.Result{
				ClusterMatched:     "InboundPassthroughCluster",
				FilterChainMatched: "virtualInbound",
			},
			Permissive: simulation.Result{
				ClusterMatched: "InboundPassthroughCluster",
			},
			Strict: simulation.Result{
				// tls, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "passthrough https",
			Call: simulation.Call{
				Address:  "1.2.3.4",
				Port:     82,
				Protocol: simulation.HTTP,
				TLS:      simulation.TLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ClusterMatched: "InboundPassthroughCluster",
			},
			Permissive: simulation.Result{
				ClusterMatched: "InboundPassthroughCluster",
			},
			Strict: simulation.Result{
				// tls, but not mTLS
				Error: simulation.ErrMTLSError,
			},
		},
		{
			Name: "passthrough mtls",
			Call: simulation.Call{
				Address:  "1.2.3.4",
				Port:     82,
				Protocol: simulation.HTTP,
				TLS:      simulation.MTLS,
				CallMode: simulation.CallModeInbound,
			},
			Disabled: simulation.Result{
				ClusterMatched: "InboundPassthroughCluster",
			},
			Permissive: simulation.Result{
				ClusterMatched: "InboundPassthroughCluster",
			},
			Strict: simulation.Result{
				ClusterMatched: "InboundPassthroughCluster",
			},
		},
	}
	t.Run("Disable", func(t *testing.T) {
		calls := []simulation.Expect{}
		for _, c := range cases {
			calls = append(calls, simulation.Expect{
				Name:   c.Name,
				Call:   c.Call,
				Result: c.Disabled,
			})
		}
		runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
			config: svc + mtlsMode("DISABLE"),
			calls:  calls,
		})
	})

	t.Run("Permissive", func(t *testing.T) {
		calls := []simulation.Expect{}
		for _, c := range cases {
			calls = append(calls, simulation.Expect{
				Name:   c.Name,
				Call:   c.Call,
				Result: c.Permissive,
			})
		}
		runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
			config: svc + mtlsMode("PERMISSIVE"),
			calls:  calls,
		})
	})

	t.Run("Strict", func(t *testing.T) {
		calls := []simulation.Expect{}
		for _, c := range cases {
			calls = append(calls, simulation.Expect{
				Name:   c.Name,
				Call:   c.Call,
				Result: c.Strict,
			})
		}
		runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
			config: svc + mtlsMode("STRICT"),
			calls:  calls,
		})
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

		// Use short host name
		{Address: "1.2.3.4", Port: 83, Protocol: simulation.TCP, TLS: simulation.TLS, HostHeader: "headless.default"},
		{Address: "1.2.3.4", Port: 84, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "headless.default"},
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
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: headless
  namespace: default
  labels:
    kubernetes.io/service-name: headless
endpoints:
- addresses:
  - 1.2.3.4
ports:
` + ports,
		calls: calls,
	},
	)
}

func TestExternalNameServices(t *testing.T) {
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
		{Address: "1.2.3.4", Port: 80, Protocol: simulation.HTTP, HostHeader: "alias.default.svc.cluster.local"},

		// Auto port should support any protocol
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, HostHeader: "alias.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "alias.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.TCP},

		{Address: "1.2.3.4", Port: 82, Protocol: simulation.TCP},

		// Use short host name
		{Address: "1.2.3.4", Port: 83, Protocol: simulation.TCP, TLS: simulation.TLS, HostHeader: "alias.default"},
		{Address: "1.2.3.4", Port: 84, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "alias.default"},
	} {
		calls = append(calls, simulation.Expect{
			Name: fmt.Sprintf("%s-%d", call.Protocol, call.Port),
			Call: call,
			Result: simulation.Result{
				ClusterMatched: fmt.Sprintf("outbound|%d||concrete.default.svc.cluster.local", call.Port),
			},
		})
	}
	service := `apiVersion: v1
kind: Service
metadata:
  name: alias
  namespace: default
spec:
  type: ExternalName
  externalName: concrete.default.svc.cluster.local
` + `---
apiVersion: v1
kind: Service
metadata:
  name: concrete
  namespace: default
spec:
  clusterIP: 1.2.3.4
  ports:` + ports
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		kubeConfig: service,
		calls:      calls,
	})

	// HTTP Routes
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		config: `apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: alias
spec:
  hosts:
  - alias.default.svc.cluster.local
  http:
  - name: "route1"
    match:
    - uri:
        prefix: "/one"
    route:
    - destination:
        host: concrete.default.svc.cluster.local`,
		kubeConfig: service,
		calls: []simulation.Expect{
			{
				// This work, Host is just an opaque hostname match
				Name: "HTTP virtual service applies to alias fqdn",
				Call: simulation.Call{Address: "1.2.3.4", Port: 80, Protocol: simulation.HTTP, HostHeader: "alias.default.svc.cluster.local", Path: "/one"},
				Result: simulation.Result{
					RouteMatched:   "route1",
					ClusterMatched: "outbound|80||concrete.default.svc.cluster.local",
				},
			},
			{
				// Host is opaque, so no expansion
				Name: "HTTP virtual service does not apply to alias without exact match",
				Call: simulation.Call{Address: "1.2.3.4", Port: 80, Protocol: simulation.HTTP, HostHeader: "alias.default", Path: "/one"},
				Result: simulation.Result{
					RouteMatched:   "default",
					ClusterMatched: "outbound|80||concrete.default.svc.cluster.local",
				},
			},
			{
				Name: "HTTP virtual service of alias does not apply to concrete",
				Call: simulation.Call{Address: "1.2.3.4", Port: 80, Protocol: simulation.HTTP, HostHeader: "concrete.default.svc.cluster.local", Path: "/one"},
				Result: simulation.Result{
					RouteMatched:   "default",
					ClusterMatched: "outbound|80||concrete.default.svc.cluster.local",
				},
			},
			// Auto
			{
				// No opaque host match for auto
				Name: "Auto virtual service applies to alias fqdn",
				Call: simulation.Call{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, HostHeader: "alias.default.svc.cluster.local", Path: "/one"},
				Result: simulation.Result{
					RouteMatched:   "default",
					ClusterMatched: "outbound|81||concrete.default.svc.cluster.local",
				},
			},
			{
				// Host is opaque, so no expansion
				Name: "Auto virtual service does not apply to alias without exact match",
				Call: simulation.Call{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, HostHeader: "alias.default", Path: "/one"},
				Result: simulation.Result{
					RouteMatched:   "default",
					ClusterMatched: "outbound|81||concrete.default.svc.cluster.local",
				},
			},
			{
				Name: "Auto virtual service of alias does not apply to concrete",
				Call: simulation.Call{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, HostHeader: "concrete.default.svc.cluster.local", Path: "/one"},
				Result: simulation.Result{
					RouteMatched:   "default",
					ClusterMatched: "outbound|81||concrete.default.svc.cluster.local",
				},
			},
		},
	})

	// TCP Routes
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		config: `apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: alias
spec:
  hosts:
  - alias.default.svc.cluster.local
  tcp:
  - name: "route1"
    route:
    - destination:
        host: concrete.default.svc.cluster.local
        port:
          number: 80`,
		kubeConfig: service,
		calls: []simulation.Expect{
			{
				Name: "TCP virtual services do not apply",
				Call: simulation.Call{Address: "1.2.3.4", Port: 82, Protocol: simulation.TCP, Path: "/one"},
				Result: simulation.Result{
					ClusterMatched: "outbound|82||concrete.default.svc.cluster.local",
				},
			},
		},
	})
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
					return m
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
apiVersion: networking.istio.io/v1
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
apiVersion: networking.istio.io/v1
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

func TestInboundSidecarTLSModes(t *testing.T) {
	peerAuthConfig := func(m string) string {
		return fmt.Sprintf(`apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: peer-auth
  namespace: default
spec:
  selector:
    matchLabels:
      app: foo
  mtls:
    mode: STRICT
  portLevelMtls:
    9080:
      mode: %s
---
`, m)
	}
	sidecarSimple := func(protocol string) string {
		return fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  labels:
    app: foo
  name: sidecar
  namespace: default
spec:
  ingress:
    - defaultEndpoint: 0.0.0.0:9080
      port:
        name: tls
        number: 9080
        protocol: %s
      tls:
        mode: SIMPLE
        privateKey: "httpbinkey.pem"
        serverCertificate: "httpbin.pem"
  workloadSelector:
    labels:
      app: foo
---
`, protocol)
	}
	sidecarMutual := func(protocol string) string {
		return fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  labels:
    app: foo
  name: sidecar
  namespace: default
spec:
  ingress:
    - defaultEndpoint: 0.0.0.0:9080
      port:
        name: tls
        number: 9080
        protocol: %s
      tls:
        mode: MUTUAL
        privateKey: "httpbinkey.pem"
        serverCertificate: "httpbin.pem"
        caCertificates: "rootCA.pem"
  workloadSelector:
    labels:
      app: foo
---
`, protocol)
	}
	expectedTLSContext := func(filterChain *listener.FilterChain) error {
		tlsContext := &tls.DownstreamTlsContext{}
		ts := filterChain.GetTransportSocket().GetTypedConfig()
		if ts == nil {
			return fmt.Errorf("expected transport socket for chain %v", filterChain.GetName())
		}
		if err := ts.UnmarshalTo(tlsContext); err != nil {
			return err
		}
		commonTLSContext := tlsContext.CommonTlsContext
		if len(commonTLSContext.TlsCertificateSdsSecretConfigs) == 0 {
			return fmt.Errorf("expected tls certificates")
		}
		if commonTLSContext.TlsCertificateSdsSecretConfigs[0].Name != "file-cert:httpbin.pem~httpbinkey.pem" {
			return fmt.Errorf("expected certificate httpbin.pem, actual %s", commonTLSContext.TlsCertificates[0].CertificateChain.String())
		}
		if tlsContext.RequireClientCertificate.Value {
			return fmt.Errorf("expected RequireClientCertificate to be false")
		}
		return nil
	}

	mkCall := func(port int, protocol simulation.Protocol,
		tls simulation.TLSMode, validations []simulation.CustomFilterChainValidation,
		mTLSSecretConfigName string,
	) simulation.Call {
		return simulation.Call{
			Protocol:                  protocol,
			Port:                      port,
			CallMode:                  simulation.CallModeInbound,
			TLS:                       tls,
			CustomListenerValidations: validations,
			MtlsSecretConfigName:      mTLSSecretConfigName,
		}
	}
	cases := []struct {
		name   string
		config string
		calls  []simulation.Expect
	}{
		{
			name:   "sidecar http over TLS simple mode with peer auth on port disabled",
			config: peerAuthConfig("DISABLE") + sidecarSimple("HTTPS"),
			calls: []simulation.Expect{
				{
					Name: "http over tls",
					Call: mkCall(9080, simulation.HTTP, simulation.TLS, []simulation.CustomFilterChainValidation{expectedTLSContext}, ""),
					Result: simulation.Result{
						FilterChainMatched: "1.1.1.1_9080",
						ClusterMatched:     "inbound|9080||",
						VirtualHostMatched: "inbound|http|9080",
						RouteMatched:       "default",
						ListenerMatched:    "virtualInbound",
					},
				},
				{
					Name: "plaintext",
					Call: mkCall(9080, simulation.HTTP, simulation.Plaintext, nil, ""),
					Result: simulation.Result{
						Error: simulation.ErrNoFilterChain,
					},
				},
				{
					Name: "http over mTLS",
					Call: mkCall(9080, simulation.HTTP, simulation.MTLS, nil, "file-cert:httpbin.pem~httpbinkey.pem"),
					Result: simulation.Result{
						Error: simulation.ErrMTLSError,
					},
				},
			},
		},
		{
			name:   "sidecar TCP over TLS simple mode with peer auth on port disabled",
			config: peerAuthConfig("DISABLE") + sidecarSimple("TLS"),
			calls: []simulation.Expect{
				{
					Name: "tcp over tls",
					Call: mkCall(9080, simulation.TCP, simulation.TLS, []simulation.CustomFilterChainValidation{expectedTLSContext}, ""),
					Result: simulation.Result{
						FilterChainMatched: "1.1.1.1_9080",
						ClusterMatched:     "inbound|9080||",
						ListenerMatched:    "virtualInbound",
					},
				},
				{
					Name: "plaintext",
					Call: mkCall(9080, simulation.TCP, simulation.Plaintext, nil, ""),
					Result: simulation.Result{
						Error: simulation.ErrNoFilterChain,
					},
				},
				{
					Name: "tcp over mTLS",
					Call: mkCall(9080, simulation.TCP, simulation.MTLS, nil, "file-cert:httpbin.pem~httpbinkey.pem"),
					Result: simulation.Result{
						Error: simulation.ErrMTLSError,
					},
				},
			},
		},
		{
			name:   "sidecar http over mTLS mutual mode with peer auth on port disabled",
			config: peerAuthConfig("DISABLE") + sidecarMutual("HTTPS"),
			calls: []simulation.Expect{
				{
					Name: "http over mtls",
					Call: mkCall(9080, simulation.HTTP, simulation.MTLS, nil, "file-cert:httpbin.pem~httpbinkey.pem"),
					Result: simulation.Result{
						FilterChainMatched: "1.1.1.1_9080",
						ClusterMatched:     "inbound|9080||",
						ListenerMatched:    "virtualInbound",
					},
				},
				{
					Name: "plaintext",
					Call: mkCall(9080, simulation.HTTP, simulation.Plaintext, nil, ""),
					Result: simulation.Result{
						Error: simulation.ErrNoFilterChain,
					},
				},
				{
					Name: "http over tls",
					Call: mkCall(9080, simulation.HTTP, simulation.TLS, nil, "file-cert:httpbin.pem~httpbinkey.pem"),
					Result: simulation.Result{
						Error: simulation.ErrMTLSError,
					},
				},
			},
		},
		{
			name:   "sidecar tcp over mTLS mutual mode with peer auth on port disabled",
			config: peerAuthConfig("DISABLE") + sidecarMutual("TLS"),
			calls: []simulation.Expect{
				{
					Name: "tcp over mtls",
					Call: mkCall(9080, simulation.TCP, simulation.MTLS, nil, "file-cert:httpbin.pem~httpbinkey.pem"),
					Result: simulation.Result{
						FilterChainMatched: "1.1.1.1_9080",
						ClusterMatched:     "inbound|9080||",
						ListenerMatched:    "virtualInbound",
					},
				},
				{
					Name: "plaintext",
					Call: mkCall(9080, simulation.TCP, simulation.Plaintext, nil, ""),
					Result: simulation.Result{
						Error: simulation.ErrNoFilterChain,
					},
				},
				{
					Name: "http over tls",
					Call: mkCall(9080, simulation.TCP, simulation.TLS, nil, "file-cert:httpbin.pem~httpbinkey.pem"),
					Result: simulation.Result{
						Error: simulation.ErrMTLSError,
					},
				},
			},
		},
		{
			name:   "sidecar http over TLS SIMPLE mode with peer auth on port STRICT",
			config: peerAuthConfig("STRICT") + sidecarMutual("TLS"),
			calls: []simulation.Expect{
				{
					Name: "http over tls",
					Call: mkCall(9080, simulation.HTTP, simulation.TLS, nil, ""),
					Result: simulation.Result{
						Error: simulation.ErrMTLSError,
					},
				},
				{
					Name: "plaintext",
					Call: mkCall(9080, simulation.HTTP, simulation.Plaintext, nil, ""),
					Result: simulation.Result{
						Error: simulation.ErrNoFilterChain,
					},
				},
				{
					Name: "http over mtls",
					Call: mkCall(9080, simulation.HTTP, simulation.MTLS, nil, ""),
					Result: simulation.Result{
						FilterChainMatched: "1.1.1.1_9080",
						ClusterMatched:     "inbound|9080||",
						ListenerMatched:    "virtualInbound",
					},
				},
			},
		},
	}
	proxy := &model.Proxy{
		Labels:   map[string]string{"app": "foo"},
		Metadata: &model.NodeMetadata{Labels: map[string]string{"app": "foo"}},
	}
	test.SetForTest(t, &features.EnableTLSOnSidecarIngress, true)
	for _, tt := range cases {
		runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
			name:   tt.name,
			config: tt.config,
			calls:  tt.calls,
		})
	}
}

const (
	TimeOlder = "2019-01-01T00:00:00Z"
	TimeBase  = "2020-01-01T00:00:00Z"
	TimeNewer = "2021-01-01T00:00:00Z"
)

type Configer interface {
	Config(t *testing.T, variant string) string
}

type vsArgs struct {
	Namespace string
	Match     string
	Matches   []string
	GwMatches []types.NamespacedName
	Dest      string
	Port      int
	PortMatch int
	Time      string
}

func (args vsArgs) Config(t *testing.T, variant string) string {
	if args.Time == "" {
		args.Time = TimeBase
	}

	if args.Matches == nil {
		args.Matches = []string{args.Match}
	}
	if variant == "httproute" {
		if args.GwMatches == nil {
			args.GwMatches = make([]types.NamespacedName, 0, len(args.Matches))
			for _, m := range args.Matches {
				spl := strings.Split(m, ".")
				if len(spl) != 5 {
					t.Skipf("unsupported match: %v", spl)
				}
				if spl[0] == "*" {
					t.Skipf("unsupported match: %v", spl)
				}
				args.GwMatches = append(args.GwMatches, types.NamespacedName{
					Namespace: spl[1],
					Name:      spl[0],
				})
			}
		}
	}
	switch variant {
	case "httproute":
		return tmpl.MustEvaluate(`apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: "{{.Namespace}}{{.Match | replace "*" "wild"}}{{.Dest}}"
  namespace: {{.Namespace}}
  creationTimestamp: "{{.Time}}"
spec:
  parentRefs:
{{- range $val := .GwMatches }}
  - group: ""
    kind: Service
    name: "{{$val.Name}}"
    namespace: "{{$val.Namespace}}"
{{ with $.PortMatch }}
    port: {{.}}
{{ end }}
{{ end }}
  rules:
  - backendRefs:
    - kind: Hostname
      group: networking.istio.io
      name: {{.Dest}}
      port: {{.Port | default 80}}
`, args)
	case "virtualservice":
		return tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: "{{.Namespace}}{{.Match | replace "*" "wild"}}{{.Dest}}"
  namespace: {{.Namespace}}
  creationTimestamp: "{{.Time}}"
spec:
  hosts:
{{- range $val := .Matches }}
  - "{{$val}}"
{{ end }}
  http:
  - route:
    - destination:
        host: {{.Dest}}
{{ with .Port }}
        port:
          number: {{.}}
{{ end }}
{{ with .PortMatch }}
    match:
    - port: {{.}}
{{ end }}
`, args)
	default:
		panic(variant + " unknown")
	}
}

type scArgs struct {
	Namespace string
	Egress    []string
}

func (args scArgs) Config(t *testing.T, variant string) string {
	return tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  name: "{{.Namespace}}"
  namespace: "{{.Namespace}}"
spec:
  egress:
  - hosts:
{{- range $val := .Egress }}
    - "{{$val}}"
{{- end }}
`, args)
}

func TestSidecarRoutes(t *testing.T) {
	knownServices := `
apiVersion: v1
kind: Service
metadata:
  name: known
  namespace: default
spec:
  clusterIP: 2.0.0.0
  ports:
  - port: 80
    name: http
  - port: 8080
    name: http
---
apiVersion: v1
kind: Service
metadata:
  name: alt-known
  namespace: default
spec:
  clusterIP: 2.0.0.1
  ports:
  - port: 80
    name: http
  - port: 8080
    name: http
---
apiVersion: v1
kind: Service
metadata:
  name: not-default
  namespace: not-default
spec:
  clusterIP: 2.0.0.2
  ports:
  - port: 80
    name: http
  - port: 8080
    name: http
`
	proxy := func(ns string) *model.Proxy {
		return &model.Proxy{ConfigNamespace: ns}
	}
	cases := []struct {
		name            string
		cfg             []Configer
		proxy           *model.Proxy
		routeName       string
		expected        map[string][]string
		expectedGateway map[string][]string
	}{
		// Port 80 has special cases as there is defaulting logic around this port
		{
			name: "simple port 80",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
		},
		{
			name: "simple port 8080",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("default"),
			routeName: "8080",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|8080||alt-known.default.svc.cluster.local"},
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
		},
		{
			name: "unknown port 80",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "foo.default.svc.cluster.local",
				Dest:      "foo.default.svc.cluster.local",
			}},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"foo.default.svc.cluster.local": {"outbound|80||foo.default.svc.cluster.local"},
			},
			expectedGateway: map[string][]string{
				"foo.default.svc.cluster.local": nil,
			},
		},
		{
			name: "unknown port 8080",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "foo.default.svc.cluster.local",
				Dest:      "foo.default.svc.cluster.local",
			}},
			proxy:     proxy("default"),
			routeName: "8080",
			// For unknown services, we only will add a route to the port 80
			expected: map[string][]string{
				"foo.default.svc.cluster.local": nil,
			},
		},
		{
			name: "unknown port 8080 match 8080",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "foo.default.svc.cluster.local",
				Dest:      "foo.default.svc.cluster.local",
				PortMatch: 8080,
			}},
			proxy:     proxy("default"),
			routeName: "8080",
			// For unknown services, we only will add a route to the port 80
			expected: map[string][]string{
				"foo.default.svc.cluster.local": nil,
			},
		},
		{
			name: "unknown port 8080 dest 8080 ",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "foo.default.svc.cluster.local",
				Dest:      "foo.default.svc.cluster.local",
				Port:      8080,
			}},
			proxy:     proxy("default"),
			routeName: "8080",
			// For unknown services, we only will add a route to the port 80
			expected: map[string][]string{
				"foo.default.svc.cluster.local": nil,
			},
		},
		{
			name: "producer rule port 80",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("not-default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
		},
		{
			name: "producer rule port 8080",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("not-default"),
			routeName: "8080",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|8080||alt-known.default.svc.cluster.local"},
			},
			expectedGateway: map[string][]string{ // No implicit port matching for gateway
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
		},
		{
			name: "consumer rule port 80",
			cfg: []Configer{vsArgs{
				Namespace: "not-default",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("not-default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
		},
		{
			name: "consumer rule port 8080",
			cfg: []Configer{vsArgs{
				Namespace: "not-default",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("not-default"),
			routeName: "8080",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|8080||alt-known.default.svc.cluster.local"},
			},
			expectedGateway: map[string][]string{ // No implicit port matching for gateway
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
		},
		{
			name: "arbitrary rule port 80",
			cfg: []Configer{vsArgs{
				Namespace: "arbitrary",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("not-default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||known.default.svc.cluster.local"},
			},
		},
		{
			name: "arbitrary rule port 8080",
			cfg: []Configer{vsArgs{
				Namespace: "arbitrary",
				Match:     "known.default.svc.cluster.local",
				Dest:      "alt-known.default.svc.cluster.local",
			}},
			proxy:     proxy("not-default"),
			routeName: "8080",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|8080||alt-known.default.svc.cluster.local"},
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|8080||known.default.svc.cluster.local"},
			},
		},
		{
			name: "multiple rules 80",
			cfg: []Configer{
				vsArgs{
					Namespace: "arbitrary",
					Match:     "known.default.svc.cluster.local",
					Dest:      "arbitrary.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "default.example.com",
					Time:      TimeBase,
				},
				vsArgs{
					Namespace: "not-default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "not-default.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("not-default"),
			routeName: "80",
			expected: map[string][]string{
				// Oldest wins
				"known.default.svc.cluster.local": {"outbound|80||arbitrary.example.com"},
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||not-default.example.com"},
			},
		},
		{
			name: "multiple rules 8080",
			cfg: []Configer{
				vsArgs{
					Namespace: "arbitrary",
					Match:     "known.default.svc.cluster.local",
					Dest:      "arbitrary.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "default.example.com",
					Time:      TimeBase,
				},
				vsArgs{
					Namespace: "not-default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "not-default.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("not-default"),
			routeName: "8080",
			expected: map[string][]string{
				// Oldest wins
				"known.default.svc.cluster.local": {"outbound|8080||arbitrary.example.com"},
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||not-default.example.com"},
			},
		},
		{
			name: "wildcard random",
			cfg: []Configer{vsArgs{
				Namespace: "default",
				Match:     "*.unknown.example.com",
				Dest:      "arbitrary.example.com",
			}},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// match no VS, get default config
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				"known.default.svc.cluster.local":     {"outbound|80||known.default.svc.cluster.local"},
				// Wildcard doesn't match any known services, insert it as-is
				"*.unknown.example.com": {"outbound|80||arbitrary.example.com"},
			},
		},
		{
			name: "wildcard match with sidecar",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.cluster.local",
					Dest:      "arbitrary.example.com",
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"*/*.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"alt-known.default.svc.cluster.local": {"outbound|80||arbitrary.example.com"},
				"known.default.svc.cluster.local":     {"outbound|80||arbitrary.example.com"},
				// Matched an exact service, so we have no route for the wildcard
				"*.cluster.local": nil,
			},
			expectedGateway: map[string][]string{
				// Exact service matches do not get the wildcard applied
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				"known.default.svc.cluster.local":     {"outbound|80||known.default.svc.cluster.local"},
				// The wildcard
				"*.cluster.local": {"outbound|80||arbitrary.example.com"},
			},
		},
		{
			name: "wildcard first then explicit",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.cluster.local",
					Dest:      "wild.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "explicit.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"alt-known.default.svc.cluster.local": {"outbound|80||wild.example.com"},
				"known.default.svc.cluster.local":     {"outbound|80||explicit.example.com"},
				// Matched an exact service, so we have no route for the wildcard
				"*.cluster.local": nil,
			},
			expectedGateway: map[string][]string{
				// No overrides, use default
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				// Explicit has precedence
				"known.default.svc.cluster.local": {"outbound|80||explicit.example.com"},
				// Last is our wildcard
				"*.cluster.local": {"outbound|80||wild.example.com"},
			},
		},
		{
			name: "explicit first then wildcard",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.cluster.local",
					Dest:      "wild.example.com",
					Time:      TimeNewer,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "explicit.example.com",
					Time:      TimeOlder,
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"alt-known.default.svc.cluster.local": {"outbound|80||wild.example.com"},
				"known.default.svc.cluster.local":     {"outbound|80||explicit.example.com"},
				// Matched an exact service, so we have no route for the wildcard
				"*.cluster.local": nil,
			},
			expectedGateway: map[string][]string{
				// No overrides, use default
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				// Explicit has precedence
				"known.default.svc.cluster.local": {"outbound|80||explicit.example.com"},
				// Last is our wildcard
				"*.cluster.local": {"outbound|80||wild.example.com"},
			},
		},
		{
			name: "wildcard and explicit with sidecar",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.cluster.local",
					Dest:      "wild.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "explicit.example.com",
					Time:      TimeNewer,
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"default/known.default.svc.cluster.local", "default/alt-known.default.svc.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// Even though we did not import `*.cluster.local`, the VS attaches
				"alt-known.default.svc.cluster.local": {"outbound|80||wild.example.com"},
				// Most exact match wins
				"known.default.svc.cluster.local": {"outbound|80||explicit.example.com"},
				// Matched an exact service, so we have no route for the wildcard
				"*.cluster.local": nil,
			},
			expectedGateway: map[string][]string{
				// No rule imported
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				// Imported rule
				"known.default.svc.cluster.local": {"outbound|80||explicit.example.com"},
				// Not imported
				"*.cluster.local": nil,
			},
		},
		{
			name: "explicit first then wildcard with sidecar cross namespace",
			cfg: []Configer{
				vsArgs{
					Namespace: "not-default",
					Match:     "*.cluster.local",
					Dest:      "wild.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "explicit.example.com",
					Time:      TimeNewer,
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"default/known.default.svc.cluster.local", "default/alt-known.default.svc.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// Similar to above, but now the older wildcard VS is in a complete different namespace which we don't import
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				"known.default.svc.cluster.local":     {"outbound|80||explicit.example.com"},
				// Matched an exact service, so we have no route for the wildcard
				"*.cluster.local": nil,
			},
		},
		{
			name: "wildcard and explicit cross namespace",
			cfg: []Configer{
				vsArgs{
					Namespace: "not-default",
					Match:     "*.cluster.local",
					Dest:      "wild.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "explicit.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// Exact match wins
				"alt-known.default.svc.cluster.local": {"outbound|80||wild.example.com"},
				"known.default.svc.cluster.local":     {"outbound|80||explicit.example.com"},
				// Matched an exact service, so we have no route for the wildcard
				"*.cluster.local": nil,
			},
			expectedGateway: map[string][]string{
				// Exact match wins
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"},
				"known.default.svc.cluster.local":     {"outbound|80||explicit.example.com"},
				// Wildcard last
				"*.cluster.local": {"outbound|80||wild.example.com"},
			},
		},
		{
			name: "wildcard and explicit unknown",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.tld",
					Dest:      "wild.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "example.tld",
					Dest:      "explicit.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// wildcard does not match
				"known.default.svc.cluster.local": {"outbound|80||known.default.svc.cluster.local"},
				// Even though its less exact, this wildcard wins
				"*.tld":         {"outbound|80||wild.example.com"},
				"*.example.tld": nil,
			},
		},
		{
			name: "explicit match with wildcard sidecar",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "arbitrary.svc.cluster.local",
					Dest:      "arbitrary.svc.cluster.local",
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"*/*.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"arbitrary.svc.cluster.local": {"outbound|80||arbitrary.svc.cluster.local"},
			},
		},
		{
			name: "wildcard match with explicit sidecar",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.cluster.local",
					Dest:      "arbitrary.example.com",
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"*/known.default.svc.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||arbitrary.example.com"},
				"*.cluster.local":                 nil,
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||known.default.svc.cluster.local"},
				"*.cluster.local":                 nil,
			},
		},
		{
			name: "non-service wildcard match with explicit sidecar",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "*.example.org",
					Dest:      "arbitrary.example.com",
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"*/explicit.example.org", "*/alt-known.default.svc.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local":     nil,                                                  // Not imported
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"}, // No change
				"*.example.org":                       {"outbound|80||arbitrary.example.com"},
			},
			expectedGateway: map[string][]string{
				"known.default.svc.cluster.local":     nil,                                                  // Not imported
				"alt-known.default.svc.cluster.local": {"outbound|80||alt-known.default.svc.cluster.local"}, // No change
				"*.example.org":                       nil,                                                  // Not imported
			},
		},
		{
			name: "sidecar filter",
			cfg: []Configer{
				vsArgs{
					Namespace: "not-default",
					Match:     "*.default.svc.cluster.local",
					Dest:      "arbitrary.example.com",
				},
				vsArgs{
					Namespace: "default",
					Match:     "explicit.default.svc.cluster.local",
					Dest:      "explicit.default.svc.cluster.local",
				},
				scArgs{
					Namespace: "not-default",
					Egress:    []string{"not-default/*.default.svc.cluster.local", "not-default/not-default.not-default.svc.cluster.local"},
				},
			},
			proxy:     proxy("not-default"),
			routeName: "80",
			expected: map[string][]string{
				// even though there is an *.svc.cluster.local, since we do not import it we should create a wildcard matcher
				"*.default.svc.cluster.local": {"outbound|80||arbitrary.example.com"},
				// We did not import this, shouldn't show up
				"explicit.default.svc.cluster.local":        nil,
				"not-default.not-default.svc.cluster.local": {"outbound|80||not-default.not-default.svc.cluster.local"},
			},
		},
		{
			name: "same namespace conflict",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "old.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "new.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				"known.default.svc.cluster.local": {"outbound|80||old.example.com"}, // oldest wins
			},
		},
		{
			name: "cross namespace conflict",
			cfg: []Configer{
				vsArgs{
					Namespace: "not-default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "producer.example.com",
					Time:      TimeOlder,
				},
				vsArgs{
					Namespace: "default",
					Match:     "known.default.svc.cluster.local",
					Dest:      "consumer.example.com",
					Time:      TimeNewer,
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// oldest wins
				"known.default.svc.cluster.local": {"outbound|80||producer.example.com"},
			},
			expectedGateway: map[string][]string{
				// consumer wins
				"known.default.svc.cluster.local": {"outbound|80||consumer.example.com"},
			},
		},
		{
			name: "import only a unknown service route",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Match:     "foo.default.svc.cluster.local",
					Dest:      "example.com",
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"*/foo.default.svc.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected:  nil, // We do not even get a route as there is no service on the port
		},
		{
			// https://github.com/istio/istio/issues/37087
			name: "multi-host import single",
			cfg: []Configer{
				vsArgs{
					Namespace: "default",
					Matches:   []string{"known.default.svc.cluster.local", "alt-known.default.svc.cluster.local"},
					Dest:      "example.com",
				},
				scArgs{
					Namespace: "default",
					Egress:    []string{"*/known.default.svc.cluster.local"},
				},
			},
			proxy:     proxy("default"),
			routeName: "80",
			expected: map[string][]string{
				// imported
				"known.default.svc.cluster.local": {"outbound|80||example.com"},
				// Not imported but we include it anyway
				"alt-known.default.svc.cluster.local": {"outbound|80||example.com"},
			},
			expectedGateway: map[string][]string{
				// imported
				"known.default.svc.cluster.local": {"outbound|80||example.com"},
				// Not imported
				"alt-known.default.svc.cluster.local": nil,
			},
		},
	}
	for _, variant := range []string{"httproute", "virtualservice"} {
		t.Run(variant, func(t *testing.T) {
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel() // feature flags and parallel tests don't mix
					cfg := knownServices
					for _, tc := range tt.cfg {
						cfg = cfg + "\n---\n" + tc.Config(t, variant)
					}
					istio, _, err := crd.ParseInputs(cfg)
					if err != nil {
						t.Fatal(err)
					}
					s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
						Configs:                istio,
						KubernetesObjectString: cfg,
					})
					sim := simulation.NewSimulation(t, s, s.SetupProxy(tt.proxy))
					xdstest.ValidateListeners(t, sim.Listeners)
					xdstest.ValidateRouteConfigurations(t, sim.Routes)
					r := xdstest.ExtractRouteConfigurations(sim.Routes)
					vh := r[tt.routeName]
					exp := tt.expected
					if variant == "httproute" && tt.expectedGateway != nil {
						exp = tt.expectedGateway
					}
					if vh == nil && exp != nil {
						t.Fatalf("route %q not found, have %v", tt.routeName, xdstest.MapKeys(r))
					}
					gotHosts := xdstest.ExtractVirtualHosts(vh)
					for wk, wv := range exp {
						got := gotHosts[wk]
						if !reflect.DeepEqual(wv, got) {
							t.Errorf("%q: wanted %v, got %v (had %v)", wk, wv, got, xdstest.MapKeys(gotHosts))
						}
					}
				})
			}
		})
	}
}

func SlowConvertKindsToRuntimeObjects(in []crd.IstioKind) ([]runtime.Object, error) {
	res := make([]runtime.Object, 0, len(in))
	for _, o := range in {
		r, err := SlowConvertToRuntimeObject(&o)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, nil
}

// SlowConvertToRuntimeObject converts an IstioKind to a runtime.Object.
// As the name implies, it is not efficient.
func SlowConvertToRuntimeObject(in *crd.IstioKind) (runtime.Object, error) {
	by, err := config.ToJSON(in)
	if err != nil {
		return nil, err
	}
	gvk := in.GetObjectKind().GroupVersionKind()
	obj, _, err := kube.IstioCodec.UniversalDeserializer().Decode(by, &gvk, nil)
	if err != nil {
		return nil, err
	}
	return obj, nil
}
