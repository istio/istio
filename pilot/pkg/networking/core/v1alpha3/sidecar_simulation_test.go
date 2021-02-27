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
	"istio.io/istio/pilot/pkg/features"
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
		Metadata:    &model.NodeMetadata{},
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
			name: "ingress to instance IP",
			configs: []config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Namespace: "default", Name: "sidecar"},
					Spec: &networking.Sidecar{Ingress: []*networking.IstioIngressListener{{
						Port: &networking.Port{
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
						Port: &networking.Port{
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

		if tt.disableInboundPassthrough {
			name += "-disableinbound"
		}
		t.Run(name, func(t *testing.T) {
			old := features.EnableInboundPassthrough
			defer func() { features.EnableInboundPassthrough = old }()
			features.EnableInboundPassthrough = !tt.disableInboundPassthrough
			s := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
				Services:  tt.services,
				Instances: tt.instances,
				Configs:   tt.configs,
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
				ClusterMatched:     "InboundPassthroughClusterIpv4",
				FilterChainMatched: "virtualInbound-catchall-http",
			},
			Permissive: simulation.Result{
				ClusterMatched:     "InboundPassthroughClusterIpv4",
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
				ClusterMatched:     "InboundPassthroughClusterIpv4",
				FilterChainMatched: "virtualInbound",
			},
			Permissive: simulation.Result{
				ClusterMatched:     "InboundPassthroughClusterIpv4",
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
				ClusterMatched:     "InboundPassthroughClusterIpv4",
				FilterChainMatched: "virtualInbound",
			},
			Permissive: simulation.Result{
				Error: simulation.ErrNoFilterChain,
				Skip:  "https://github.com/istio/istio/issues/29538",
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
				ClusterMatched: "InboundPassthroughClusterIpv4",
			},
			Permissive: simulation.Result{
				Error: simulation.ErrMTLSError,
				// We are matching the mtls chain unexpectedly
				Skip: "https://github.com/istio/istio/issues/29538#issuecomment-742819586",
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
				ClusterMatched: "InboundPassthroughClusterIpv4",
			},
			Permissive: simulation.Result{
				ClusterMatched: "InboundPassthroughClusterIpv4",
			},
			Strict: simulation.Result{
				ClusterMatched: "InboundPassthroughClusterIpv4",
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
