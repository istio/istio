// Copyright 2017 Istio Authors
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

package envoy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	restful "github.com/emicklei/go-restful"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/adapter/config/memory"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/mock"
	"istio.io/pilot/test/util"
)

var (
	ingressSecretURI = "my-secret.default"
	ingressCert      = []byte("abcdefghijklmnop")
	ingressKey       = []byte("qrstuvwxyz123456")
	ingressTLSSecret = &model.TLSSecret{Certificate: ingressCert, PrivateKey: ingressKey}
)

// Implement minimal methods to satisfy model.Controller interface for
// creating a new discovery service instance.
type mockController struct {
	handlers int
}

func (ctl *mockController) AppendServiceHandler(_ func(*model.Service, model.Event)) error {
	ctl.handlers++
	return nil
}
func (ctl *mockController) AppendInstanceHandler(_ func(*model.ServiceInstance, model.Event)) error {
	ctl.handlers++
	return nil
}
func (ctl *mockController) Run(_ <-chan struct{}) {}

func makeDiscoveryService(t *testing.T, r model.ConfigStore, mesh *proxyconfig.ProxyMeshConfig) *DiscoveryService {
	out, err := NewDiscoveryService(
		&mockController{},
		nil,
		proxy.Environment{
			ServiceDiscovery: mock.Discovery,
			ServiceAccounts:  mock.Discovery,
			IstioConfigStore: model.MakeIstioStore(r),
			SecretRegistry: mock.SecretRegistry{
				ingressSecretURI: ingressTLSSecret,
			},
			Mesh: mesh,
		},
		DiscoveryServiceOptions{
			EnableCaching:   true,
			EnableProfiling: true, // increase code coverage stats
		})
	if err != nil {
		t.Fatalf("NewDiscoveryService failed: %v", err)
	}
	return out
}

func makeDiscoveryRequest(ds *DiscoveryService, method, url string, t *testing.T) []byte {
	httpRequest, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpWriter := httptest.NewRecorder()
	container := restful.NewContainer()
	ds.Register(container)
	container.ServeHTTP(httpWriter, httpRequest)
	body, err := ioutil.ReadAll(httpWriter.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func compareResponse(body []byte, file string, t *testing.T) {
	err := ioutil.WriteFile(file, body, 0644)
	if err != nil {
		t.Fatalf(err.Error())
	}
	util.CompareYAML(file, t)
}

func TestServiceDiscovery(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds.json", t)
}

// Can we list Services?
func TestServiceDiscoveryListAllServices(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)

	url := "/v1/registration/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-sds.json", t)
}

func TestServiceDiscoveryVersion(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0],
		map[string]string{"version": "v1"})
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds-v1.json", t)
}

func TestServiceDiscoveryEmpty(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := "/v1/registration/nonexistent"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds-empty.json", t)
}

// Test listing all clusters
func TestClusterDiscoveryAllClusters(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := "/v1/clusters/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-cds.json", t)
}

func TestClusterDiscovery(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds.json", t)
}

func TestClusterDiscoveryCircuitBreaker(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addCircuitBreaker(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-circuit-breaker.json", t)
}

func TestClusterDiscoveryWithSSLContext(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ssl-context.json", t)
}

func TestClusterDiscoveryIngress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.Mesh.IstioServiceCluster, mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ingress.json", t)
}

func TestClusterDiscoveryEgress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.Mesh.IstioServiceCluster, mock.Egress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-egress.json", t)
}

// Test listing all routes
func TestRouteDiscoveryAllRoutes(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := "/v1/routes/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-rds.json", t)
}

func TestRouteDiscoveryV0(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0.json", t)
}

func TestRouteDiscoveryV0Status(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := fmt.Sprintf("/v1/routes/81/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0-status.json", t)
}

func TestRouteDiscoveryV1(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV1.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}

func TestRouteDiscoveryTimeout(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addTimeout(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-timeout.json", t)
}

func TestRouteDiscoveryWeighted(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addWeightedRoute(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-weighted.json", t)
}

func TestRouteDiscoveryFault(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addFaultRoute(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-fault.json", t)

	url = fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV1.ServiceNode())
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}

func TestRouteDiscoveryRedirect(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addRedirect(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-redirect.json", t)
}

func TestRouteDiscoveryRewrite(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addRewrite(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-rewrite.json", t)
}

func TestRouteDiscoveryIngress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress.json", t)

	url = fmt.Sprintf("/v1/routes/443/%s/%s", ds.Mesh.IstioServiceCluster, mock.Ingress.ServiceNode())
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress-ssl.json", t)
}

func TestRouteDiscoveryIngressWeighted(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addIngressRoutes(registry, t)
	addWeightedRoute(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress-weighted.json", t)
}

func TestRouteDiscoveryEgress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/routes/8888/%s/%s", ds.Mesh.IstioServiceCluster, mock.Egress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-egress.json", t)
}

func TestSidecarListenerDiscovery(t *testing.T) {
	testCases := []struct {
		name string
		typ  string
		file string
	}{
		{name: "none"},
		/* these configs do not affect listeners
		{
			name: "cb",
			typ:  model.DestinationPolicy,
			file: cbPolicy,
		},
		{
			name: "redirect",
			typ:  model.RouteRule,
			file: redirectRouteRule,
		},
		{
			name: "rewrite",
			typ:  model.RouteRule,
			file: rewriteRouteRule,
		},
		{
			name: "timeout",
			typ:  model.RouteRule,
			file: timeoutRouteRule,
		},
		*/
		{
			name: "weighted",
			typ:  model.RouteRule.Type,
			file: weightedRouteRule,
		},
		{
			name: "fault",
			typ:  model.RouteRule.Type,
			file: faultRouteRule,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := memory.Make(model.IstioConfigTypes)

			if testCase.typ != "" {
				msg, err := configObjectFromYAML(testCase.typ, testCase.file)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = registry.Post(msg); err != nil {
					t.Fatal(err)
				}
			}

			// test with no auth
			mesh := makeMeshConfig()
			ds := makeDiscoveryService(t, registry, &mesh)
			url := fmt.Sprintf("/v1/listeners/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
			response := makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s.json", testCase.name), t)

			url = fmt.Sprintf("/v1/listeners/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV1.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v1-%s.json", testCase.name), t)

			// test with auth
			mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
			ds = makeDiscoveryService(t, registry, &mesh)
			url = fmt.Sprintf("/v1/listeners/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s-auth.json", testCase.name), t)

			url = fmt.Sprintf("/v1/listeners/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV1.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v1-%s-auth.json", testCase.name), t)
		})
	}
}

func TestListenerDiscoveryIngress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/listeners/%s/%s", ds.Mesh.IstioServiceCluster, mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-ingress.json", t)

	mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	ds = makeDiscoveryService(t, registry, &mesh)
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-ingress.json", t)
}

func TestListenerDiscoveryEgress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/listeners/%s/%s", ds.Mesh.IstioServiceCluster, mock.Egress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-egress.json", t)

	mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	ds = makeDiscoveryService(t, registry, &mesh)
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-egress-auth.json", t)
}

func TestSecretDiscovery(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1alpha/secret/%s/%s", ds.Mesh.IstioServiceCluster, mock.Ingress.ServiceNode())
	got := makeDiscoveryRequest(ds, "GET", url, t)
	want, err := json.Marshal(ingressTLSSecret)
	if err != nil {
		t.Error(err)
	}
	if string(got) != string(want) {
		t.Errorf("ListSecret() => Got %q, expected %q", got, want)
	}
}

func TestDiscoveryCache(t *testing.T) {
	mesh := makeMeshConfig()
	ds := makeDiscoveryService(t, memory.Make(model.IstioConfigTypes), &mesh)

	sds := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	cds := fmt.Sprintf("/v1/clusters/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	rds := fmt.Sprintf("/v1/routes/80/%s/%s", ds.Mesh.IstioServiceCluster, mock.ProxyV0.ServiceNode())
	responseByPath := map[string]string{
		sds: "testdata/sds.json",
		cds: "testdata/cds.json",
		rds: "testdata/rds-v1.json",
	}

	cases := []struct {
		wantCache  string
		query      bool
		clearCache bool
		clearStats bool
	}{
		{
			wantCache: "testdata/cache-empty.json",
		},
		{
			wantCache: "testdata/cache-cold.json",
			query:     true,
		},
		{
			wantCache: "testdata/cache-warm-one.json",
			query:     true,
		},
		{
			wantCache: "testdata/cache-warm-two.json",
			query:     true,
		},
		{
			wantCache:  "testdata/cache-cleared.json",
			clearCache: true,
			query:      true,
		},
		{
			wantCache:  "testdata/cache-cold.json",
			clearCache: true,
			clearStats: true,
			query:      true,
		},
	}
	for _, c := range cases {
		if c.clearCache {
			ds.clearCache()
		}
		if c.clearStats {
			_ = makeDiscoveryRequest(ds, "POST", "/cache_stats_delete", t)
		}
		if c.query {
			for path, want := range responseByPath {
				got := makeDiscoveryRequest(ds, "GET", path, t)
				compareResponse(got, want, t)
			}
		}
		got := makeDiscoveryRequest(ds, "GET", "/cache_stats", t)
		compareResponse(got, c.wantCache, t)
	}
}
