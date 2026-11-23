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

package gateway

import (
	"fmt"
	"testing"

	envoyroute "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	istioroute "istio.io/istio/pilot/pkg/networking/core/route"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestMeshRoutePortMerge(t *testing.T) {
	cases := []struct {
		kind          string
		allPortsMatch string
		port80Match   string
		allPortsURI   *istio.StringMatch
		port80Path    string
		allPortsEnvoy *envoyroute.RouteMatch
	}{
		{
			kind:          "HTTPRoute",
			allPortsMatch: "matches: [{path: {type: PathPrefix, value: /}}]",
			port80Match:   "matches: [{path: {type: Exact, value: /special}}]",
			allPortsURI:   prefixURI("/"),
			port80Path:    "/special",
			allPortsEnvoy: &envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{Prefix: "/"},
				CaseSensitive: wrapperspb.Bool(true),
			},
		},
		{
			kind:          "GRPCRoute",
			allPortsMatch: "matches: [{method: {service: example.Service}}]",
			port80Match:   "matches: [{method: {service: example.Service, method: Call}}]",
			allPortsURI:   prefixURI("/example.Service/"),
			port80Path:    "/example.Service/Call",
			allPortsEnvoy: &envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_PathSeparatedPrefix{PathSeparatedPrefix: "/example.Service"},
				CaseSensitive: wrapperspb.Bool(true),
			},
		},
	}
	for _, tc := range cases {
		for _, first := range []string{"all-ports", "port-specific"} {
			t.Run(tc.kind+"/"+first+"-first", func(t *testing.T) {
				allPortsTime, port80Time := "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"
				if first == "port-specific" {
					allPortsTime, port80Time = port80Time, allPortsTime
				}
				input, err := kubernetesObjectsFromString(fmt.Sprintf(`apiVersion: gateway.networking.k8s.io/v1
kind: %s
metadata:
  name: all-ports
  namespace: default
  creationTimestamp: %s
spec:
  parentRefs: [{group: "", kind: Service, name: front}]
  rules:
  - %s
    backendRefs: [{name: target, port: 80}]
---
apiVersion: gateway.networking.k8s.io/v1
kind: %s
metadata:
  name: port-specific
  namespace: default
  creationTimestamp: %s
spec:
  parentRefs: [{group: "", kind: Service, name: front, port: 80}]
  rules:
  - %s
    backendRefs: [{name: alternate, port: 80}]
`, tc.kind, allPortsTime, tc.allPortsMatch, tc.kind, port80Time, tc.port80Match))
				assert.NoError(t, err)
				input = append(input, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
				for _, name := range []string{"front", "target", "alternate"} {
					ports := []corev1.ServicePort{{Name: "http", Port: 80}}
					if name == "front" {
						ports = append(ports, corev1.ServicePort{Name: "http-alt", Port: 8080})
					}
					input = append(input, &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
						Spec:       corev1.ServiceSpec{Ports: ports},
					})
				}
				kc := kube.NewFakeClient(input...)
				setupClientCRDs(t, kc)
				cg := core.NewConfigGenTest(t, core.TestOptions{})
				ctrl := NewController(kc, AlwaysReady, controller.Options{DomainSuffix: "cluster.local"}, nil)
				stop := test.NewStop(t)
				go ctrl.Run(stop)
				kc.RunAndWait(stop)
				ctrl.Reconcile(cg.PushContext())
				kube.WaitForCacheSync("test", stop, ctrl.HasSynced)

				configs := ctrl.List(gvk.VirtualService, "default")
				assert.Equal(t, len(configs), 1, "mesh routes must merge into one VirtualService")
				vs := configs[0].Spec.(*istio.VirtualService)
				assert.Equal(t, vs.Hosts, []string{"front.default.svc.cluster.local"})
				assert.Equal(t, vs.Gateways, []string{"mesh"})
				assert.Equal(t, len(vs.Http), 2)
				assert.Equal(t, vs.Http[0].Match, []*istio.HTTPMatchRequest{{Uri: exactURI(tc.port80Path), Port: 80}})
				assert.Equal(t, vs.Http[1].Match, []*istio.HTTPMatchRequest{{Uri: tc.allPortsURI}})

				proxy := &model.Proxy{Metadata: &model.NodeMetadata{Namespace: "default"}}
				opts := istioroute.RouteOptions{
					Mesh:                     mesh.DefaultMeshConfig(),
					LookupService:            func(host.Name) *model.Service { return nil },
					LookupDestinationCluster: istioroute.GetDestinationCluster,
					LookupHash:               func(*istio.HTTPRouteDestination) *istio.LoadBalancerSettings_ConsistentHashLB { return nil },
				}
				for _, port := range []int{80, 8080} {
					t.Run(fmt.Sprintf("port-%d", port), func(t *testing.T) {
						routes, err := istioroute.BuildHTTPRoutesForVirtualService(proxy, configs[0], port, sets.New("mesh"), opts)
						assert.NoError(t, err)
						if port == 80 {
							assert.Equal(t, len(routes), 2)
							assert.Equal(t, routes[0].Match, &envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Path{Path: tc.port80Path},
								CaseSensitive: wrapperspb.Bool(true),
							})
							assert.Equal(t, routes[0].GetRoute().GetCluster(), "outbound|80||alternate.default.svc.cluster.local")
							routes = routes[1:]
						}
						assert.Equal(t, len(routes), 1, "all-ports route must remain available")
						assert.Equal(t, routes[0].Match, tc.allPortsEnvoy)
						assert.Equal(t, routes[0].GetRoute().GetCluster(), "outbound|80||target.default.svc.cluster.local")
					})
				}
			})
		}
	}
}
