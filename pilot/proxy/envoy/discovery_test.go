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

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
)

func makeDiscoveryService(r *model.IstioRegistry) *DiscoveryService {
	return &DiscoveryService{
		services: mock.Discovery,
		config:   r,
		mesh:     DefaultMeshConfig,
	}
}

func makeDiscoveryServiceWithSSLContext(r *model.IstioRegistry) *DiscoveryService {
	meshConfigWithSSLContext := *DefaultMeshConfig
	meshConfigWithSSLContext.EnableAuth = true
	meshConfigWithSSLContext.AuthConfigPath = "/etc/certs"
	return &DiscoveryService{
		services: mock.Discovery,
		config:   r,
		mesh:     &meshConfigWithSSLContext,
	}
}

func makeDiscoveryRequest(ds *DiscoveryService, url string, t *testing.T) []byte {
	httpRequest, err := http.NewRequest("GET", url, nil)
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
	compareJSON(file, t)
}

func TestServiceDiscovery(t *testing.T) {
	ds := makeDiscoveryService(mock.MakeRegistry())
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/sds.json", t)
}

func TestServiceDiscoveryVersion(t *testing.T) {
	ds := makeDiscoveryService(mock.MakeRegistry())
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0],
		map[string]string{"version": "v1"})
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/sds-v1.json", t)
}

func TestServiceDiscoveryEmpty(t *testing.T) {
	ds := makeDiscoveryService(mock.MakeRegistry())
	url := "/v1/registration/nonexistent"
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/sds-empty.json", t)
}

func TestClusterDiscovery(t *testing.T) {
	registry := mock.MakeRegistry()
	ds := makeDiscoveryService(registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/cds.json", t)
}

func TestClusterDiscoveryCircuitBreaker(t *testing.T) {
	registry := mock.MakeRegistry()
	addCircuitBreaker(registry, t)
	ds := makeDiscoveryService(registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/cds-circuit-breaker.json", t)
}

func TestClusterDiscoveryWithSSLContext(t *testing.T) {
	registry := mock.MakeRegistry()
	ds := makeDiscoveryServiceWithSSLContext(registry)
	url := fmt.Sprintf("/v1/clusters/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/cds-ssl-context.json", t)
}

func TestRouteDiscovery(t *testing.T) {
	ds := makeDiscoveryService(mock.MakeRegistry())
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/rds-v0.json", t)
	url = fmt.Sprintf("/v1/routes/80/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV1)
	response = makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}

func TestRouteDiscoveryTimeout(t *testing.T) {
	registry := mock.MakeRegistry()
	addTimeout(registry, t)
	ds := makeDiscoveryService(registry)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/rds-timeout.json", t)
}

func TestRouteDiscoveryWeighted(t *testing.T) {
	registry := mock.MakeRegistry()
	addWeightedRoute(registry, t)
	ds := makeDiscoveryService(registry)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/rds-weighted.json", t)
}

func TestRouteDiscoveryFault(t *testing.T) {
	registry := mock.MakeRegistry()
	addFaultRoute(registry, t)
	ds := makeDiscoveryService(registry)

	// fault rule is source based: we check that the rule only affect v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV0)
	response := makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/rds-fault.json", t)

	url = fmt.Sprintf("/v1/routes/80/%s/%s", ds.mesh.IstioServiceCluster, mock.HostInstanceV1)
	response = makeDiscoveryRequest(ds, url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}
