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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/mock"
	"istio.io/pilot/test/util"
)

// Implement minimal methods to satisfy model.Controller interface for
// creating a new discovery service instance.
type mockController struct {
	handlers int
}

func (ctl *mockController) AppendConfigHandler(_ string, _ func(model.Key, proto.Message, model.Event)) error {
	ctl.handlers++
	return nil
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

func makeDiscoveryService(t *testing.T, r *model.IstioRegistry) *DiscoveryService {
	mesh := proxy.DefaultMeshConfig()
	out, err := NewDiscoveryService(
		&mockController{},
		&proxy.Context{
			Discovery:  mock.Discovery,
			Accounts:   mock.Discovery,
			Config:     r,
			MeshConfig: &mesh,
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

func makeDiscoveryServiceWithSSLContext(t *testing.T, r *model.IstioRegistry) *DiscoveryService {
	mesh := proxy.DefaultMeshConfig()
	mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	out, err := NewDiscoveryService(
		&mockController{},
		&proxy.Context{
			Discovery:  mock.Discovery,
			Accounts:   mock.Discovery,
			Config:     r,
			MeshConfig: &mesh,
		},
		DiscoveryServiceOptions{
			EnableCaching: true,
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
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds.json", t)
}

// Can we list Services?
func TestServiceDiscoveryListAllServices(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())

	url := "/v1/registration/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-sds.json", t)
}

func TestServiceDiscoveryVersion(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0],
		map[string]string{"version": "v1"})
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds-v1.json", t)
}

func TestServiceDiscoveryEmpty(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := "/v1/registration/nonexistent"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds-empty.json", t)
}

// Test listing all clusters
func TestClusterDiscoveryAllClusters(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := "/v1/clusters/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-cds.json", t)
}

func TestClusterDiscovery(t *testing.T) {
	registry := mock.MakeRegistry()
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds.json", t)
}

func TestClusterDiscoveryCircuitBreaker(t *testing.T) {
	registry := mock.MakeRegistry()
	addCircuitBreaker(registry, t)
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-circuit-breaker.json", t)
}

func TestClusterDiscoveryWithSSLContext(t *testing.T) {
	registry := mock.MakeRegistry()
	ds := makeDiscoveryServiceWithSSLContext(t, registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ssl-context.json", t)
}

func TestClusterDiscoveryIngress(t *testing.T) {
	registry := mock.MakeRegistry()
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.MeshConfig.IstioServiceCluster, ingressNode)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ingress.json", t)
}

func TestClusterDiscoveryEgress(t *testing.T) {
	registry := mock.MakeRegistry()
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.MeshConfig.IstioServiceCluster, egressNode)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-egress.json", t)
}

// Test listing all routes
func TestRouteDiscoveryAllRoutes(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := "/v1/routes/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-rds.json", t)
}

func TestRouteDiscoveryV0(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0.json", t)
}

func TestRouteDiscoveryV0Status(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := fmt.Sprintf("/v1/routes/81/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0-status.json", t)
}

func TestRouteDiscoveryV1(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV1)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}

func TestRouteDiscoveryTimeout(t *testing.T) {
	registry := mock.MakeRegistry()
	addTimeout(registry, t)
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-timeout.json", t)
}

func TestRouteDiscoveryWeighted(t *testing.T) {
	registry := mock.MakeRegistry()
	addWeightedRoute(registry, t)
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-weighted.json", t)
}

func TestRouteDiscoveryFault(t *testing.T) {
	registry := mock.MakeRegistry()
	addFaultRoute(registry, t)
	ds := makeDiscoveryService(t, registry)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-fault.json", t)

	url = fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV1)
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}

func TestRouteDiscoveryRedirect(t *testing.T) {
	registry := mock.MakeRegistry()
	addRedirect(registry, t)
	ds := makeDiscoveryService(t, registry)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-redirect.json", t)
}

func TestRouteDiscoveryRewrite(t *testing.T) {
	registry := mock.MakeRegistry()
	addRewrite(registry, t)
	ds := makeDiscoveryService(t, registry)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-rewrite.json", t)
}

func TestRouteDiscoveryIngress(t *testing.T) {
	registry := mock.MakeRegistry()
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, ingressNode)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress.json", t)

	url = fmt.Sprintf("/v1/routes/443/%s/%s", ds.MeshConfig.IstioServiceCluster, ingressNode)
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress-ssl.json", t)
}

func TestRouteDiscoveryIngressWeighted(t *testing.T) {
	registry := mock.MakeRegistry()
	addIngressRoutes(registry, t)
	addWeightedRoute(registry, t)
	ds := makeDiscoveryService(t, registry)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, ingressNode)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress-weighted.json", t)
}

func TestRouteDiscoveryEgress(t *testing.T) {
	registry := mock.MakeRegistry()
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, egressNode)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-egress.json", t)
}

func TestSecretDiscovery(t *testing.T) {
	registry := mock.MakeRegistry()
	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry)
	url := fmt.Sprintf("/v1alpha/secret/%s/%s", ds.MeshConfig.IstioServiceCluster, ingressNode)
	got := makeDiscoveryRequest(ds, "GET", url, t)
	want := "my-secret.default"
	if string(got) != want {
		t.Errorf("ListSecret() => Got %q, expected %q", got, want)
	}
}

func TestDiscoveryCache(t *testing.T) {
	ds := makeDiscoveryService(t, mock.MakeRegistry())

	sds := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	cds := fmt.Sprintf("/v1/clusters/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
	rds := fmt.Sprintf("/v1/routes/80/%s/%s", ds.MeshConfig.IstioServiceCluster, mock.HostInstanceV0)
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
