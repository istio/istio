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

package v1

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	restful "github.com/emicklei/go-restful"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
	"istio.io/istio/pilot/test/util"
	pkgutil "istio.io/istio/pkg/util"
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

var mockDiscovery *mock.ServiceDiscovery

func makeDiscoveryService(t *testing.T, r model.ConfigStore, mesh *meshconfig.MeshConfig) *DiscoveryService {
	mockDiscovery = mock.Discovery
	mockDiscovery.ClearErrors()
	out, err := NewDiscoveryService(
		&mockController{},
		nil,
		model.Environment{
			ServiceDiscovery: mockDiscovery,
			ServiceAccounts:  mockDiscovery,
			IstioConfigStore: model.MakeIstioStore(r),
			Mesh:             mesh,
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

func getDiscoveryResponse(ds *DiscoveryService, method, url string, t *testing.T) *http.Response {
	httpRequest, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpWriter := httptest.NewRecorder()
	container := restful.NewContainer()
	ds.Register(container)
	container.ServeHTTP(httpWriter, httpRequest)

	return httpWriter.Result()
}

func commonSetup(t *testing.T) (*meshconfig.MeshConfig, model.ConfigStore, *DiscoveryService) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	return &mesh, registry, ds
}

func compareResponse(body []byte, file string, t *testing.T) {
	err := ioutil.WriteFile(file, body, 0644)
	if err != nil {
		t.Fatalf(err.Error())
	}
	util.CompareYAML(file, t)
}

func TestServiceDiscovery(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds.json", t)
}

func TestDiscoveryLDSWebHooks(t *testing.T) {
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != url {
			t.Errorf("WebHook expected URL: %s, got %s", url, r.URL.Path)
		}
		fmt.Fprintln(w, "'listeners': [ {'name': 'Hello-LDS-WebHook'}]")
	}))
	defer ts.Close()

	_, _, ds := commonSetup(t)
	ds.webhookEndpoint, ds.webhookClient = pkgutil.NewWebHookClient(ts.URL)

	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-webhook.json", t)
}

func TestDiscoveryCDSWebHooks(t *testing.T) {
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != url {
			t.Errorf("WebHook expected URL: %s, got %s", url, r.URL.Path)
		}
		fmt.Fprintln(w, "'clusters': [ {'name': 'Hello-CDS-WebHook'}]")
	}))
	defer ts.Close()

	_, _, ds := commonSetup(t)
	ds.webhookEndpoint, ds.webhookClient = pkgutil.NewWebHookClient(ts.URL)

	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-webhook.json", t)
}

func TestDiscoveryRDSWebHooks(t *testing.T) {
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != url {
			t.Errorf("WebHook expected URL: %s, got %s", url, r.URL.Path)
		}
		fmt.Fprintln(w, "'routes': [ {'name': 'Hello-RDS-WebHook'}]")
	}))
	defer ts.Close()

	_, _, ds := commonSetup(t)
	ds.webhookEndpoint, ds.webhookClient = pkgutil.NewWebHookClient(ts.URL)

	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-webhook.json", t)
}

// Can we list Services?
func TestServiceDiscoveryListAllServices(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := "/v1/registration/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-sds.json", t)
}

func TestServiceDiscoveryListAllServicesError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := "/v1/registration/"
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestServiceDiscoveryListAllServicesError2(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.InstancesError = errors.New("mock Instances() error")
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0],
		map[string]string{"version": "v1"})
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestServiceDiscoveryError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.InstancesError = errors.New("mock Instances() error")
	url := "/v1/registration/nonexistent"
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestServiceDiscoveryVersion(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0],
		map[string]string{"version": "v1"})
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds-v1.json", t)
}

func TestServiceDiscoveryEmpty(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := "/v1/registration/nonexistent"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/sds-empty.json", t)
}

// Test listing all clusters
func TestClusterDiscoveryAllClusters(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := "/v1/clusters/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-cds.json", t)
}

func TestClusterDiscovery(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds.json", t)
}

func TestClusterDiscoveryError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestClusterDiscoveryError2(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.GetSidecarServiceInstancesError = errors.New("mock GetSidecarServiceInstances() error")
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestClusterDiscoveryCircuitBreaker(t *testing.T) {
	tests := []struct {
		configs  []fileConfig
		response string
	}{
		{configs: []fileConfig{weightedRouteRule, cbPolicy, egressRule, egressRuleCBPolicy},
			response: "testdata/cds-circuit-breaker.json"},
		{configs: []fileConfig{cbRouteRuleV2, destinationRuleWorldCB, externalServiceRule, destinationRuleGoogleCB},
			response: "testdata/cds-circuit-breaker-v1alpha2.json"},
	}

	for _, tc := range tests {
		_, registry, ds := commonSetup(t)
		for _, config := range tc.configs {
			addConfig(registry, config, t)
		}

		url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, tc.response, t)
	}
}

func TestClusterDiscoveryEgressRedirect(t *testing.T) {
	_, registry, ds := commonSetup(t)
	addConfig(registry, egressRule, t)
	addConfig(registry, redirectRouteToEgressRule, t)

	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-redirect-egress.json", t)
}

func TestClusterDiscoveryWithAuthOptIn(t *testing.T) {
	// Change mock service security for test.
	mock.WorldService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
	_, _, ds := commonSetup(t)
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ssl-context-optin.json", t)
	// Reset mock service security option.
	mock.WorldService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_INHERIT
}

func TestClusterDiscoveryWithSecurityOn(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
	registry := memory.Make(model.IstioConfigTypes)
	addConfig(registry, egressRule, t) // original dst cluster should not have auth

	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ssl-context.json", t)
}

func TestClusterDiscoveryWithAuthOptOut(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
	registry := memory.Make(model.IstioConfigTypes)
	addConfig(registry, egressRule, t) // original dst cluster should not have auth

	// Change mock service security for test.
	mock.WorldService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_NONE

	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ssl-context-optout.json", t)

	// Reset mock service security option.
	mock.WorldService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_INHERIT
}

func TestClusterDiscoveryIngress(t *testing.T) {
	_, registry, ds := commonSetup(t)
	addIngressRoutes(registry, t)
	addConfig(registry, egressRuleTCP, t)
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-ingress.json", t)
}

func TestClusterDiscoveryRouterError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.Router.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestClusterDiscoveryRouter(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.Router.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-router.json", t)
}

// Test listing all routes
func TestRouteDiscoveryAllRoutes(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := "/v1/routes/"
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/all-rds.json", t)
}

func TestRouteDiscoveryV0(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0.json", t)
}

func TestRouteDiscoveryError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestRouteDiscoveryError2(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.GetSidecarServiceInstancesError = errors.New("mock GetSidecarServiceInstances() error")
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestRouteDiscoveryV0Mixerless(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.MixerAddress = ""
	registry := memory.Make(model.IstioConfigTypes)
	addConfig(registry, egressRule, t) //expect *.google.com and *.yahoo.com
	addConfig(registry, egressRuleTCP, t)

	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0-nomixer.json", t)
}

func TestRouteDiscoveryV0Status(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := fmt.Sprintf("/v1/routes/81/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v0-status.json", t)
}

func TestRouteDiscoveryV1(t *testing.T) {
	_, _, ds := commonSetup(t)
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-v1.json", t)
}

func TestRouteDiscoveryTimeout(t *testing.T) {
	tests := [][]fileConfig{
		{timeoutRouteRule, egressRule, egressRuleTimeoutRule},
		{timeoutRouteRuleV2, externalServiceRule, googleTimeoutRuleV2},
	}

	for _, configs := range tests {
		_, registry, ds := commonSetup(t)
		for _, config := range configs {
			addConfig(registry, config, t)
		}
		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-timeout.json", t)
	}
}

func TestRouteDiscoveryWeighted(t *testing.T) {
	for _, weightedConfig := range []fileConfig{weightedRouteRule, weightedRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, weightedConfig, t)

		// TODO: v1alpha2 only
		addConfig(registry, destinationRuleWorld, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-weighted.json", t)
	}
}

func TestRouteDiscoveryFault(t *testing.T) {
	for _, faultConfig := range []fileConfig{faultRouteRule, faultRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, faultConfig, t)

		// TODO: v1alpha2 only
		addConfig(registry, destinationRuleWorld, t)

		// fault rule is source based: we check that the rule only affect v0 and not v1
		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-fault.json", t)

		url = fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
		response = makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-v1.json", t)
	}
}

func TestRouteDiscoveryMultiMatchFault(t *testing.T) {
	_, registry, ds := commonSetup(t)
	addConfig(registry, multiMatchFaultRouteRuleV2, t)

	// TODO: v1alpha2 only
	addConfig(registry, destinationRuleWorld, t)

	// fault rule is source based: we check that the rule only affects v0 and not v1
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-fault-v0.json", t)

	url = fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-fault-v1.json", t)
}

func TestRouteDiscoveryMirror(t *testing.T) {
	for _, mirrorConfig := range []fileConfig{mirrorRule, mirrorRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, mirrorConfig, t)

		// TODO: v1alpha2 only
		addConfig(registry, destinationRuleHello, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-mirror.json", t)
	}
}

func TestRouteDiscoveryAppendHeaders(t *testing.T) {
	for _, addHeaderConfig := range []fileConfig{addHeaderRule, addHeaderRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, addHeaderConfig, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-append-headers.json", t)
	}
}

func TestRouteDiscoveryCORSPolicy(t *testing.T) {
	for _, corsConfig := range []fileConfig{corsPolicyRule, corsPolicyRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, corsConfig, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-cors-policy.json", t)
	}
}

func TestRouteDiscoveryRedirect(t *testing.T) {
	for _, redirectConfig := range []fileConfig{redirectRouteRule, redirectRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, redirectConfig, t)

		// fault rule is source based: we check that the rule only affect v0 and not v1
		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-redirect.json", t)
	}
}

func TestRouteDiscoveryEgressRedirect(t *testing.T) {
	_, registry, ds := commonSetup(t)
	addConfig(registry, egressRule, t)
	addConfig(registry, redirectRouteToEgressRule, t)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-redirect-egress.json", t)
}

func TestRouteDiscoveryRewrite(t *testing.T) {
	for _, rewriteConfig := range []fileConfig{rewriteRouteRule, rewriteRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, rewriteConfig, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-rewrite.json", t)
	}
}

func TestRouteDiscoveryMultiMatchRewrite(t *testing.T) {
	_, registry, ds := commonSetup(t)
	addConfig(registry, multiMatchRewriteRouteRuleV2, t)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-multi-match-rewrite-v1alpha2.json", t)
}

func TestRouteDiscoveryWebsocket(t *testing.T) {
	for _, websocketConfig := range []fileConfig{websocketRouteRule, websocketRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, websocketConfig, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-websocket.json", t)

		url = fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
		response = makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/lds-websocket.json", t)
	}
}

func TestRouteDiscoveryIngress(t *testing.T) {
	_, registry, ds := commonSetup(t)
	addIngressRoutes(registry, t)

	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress.json", t)

	url = fmt.Sprintf("/v1/routes/443/%s/%s", "istio-proxy", mock.Ingress.ServiceNode())
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-ingress-ssl.json", t)
}

func TestRouteDiscoveryIngressWeighted(t *testing.T) {
	for _, weightConfig := range []fileConfig{weightedRouteRule, weightedRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addIngressRoutes(registry, t)
		addConfig(registry, weightConfig, t)

		// TODO: v1alpha2 only
		addConfig(registry, destinationRuleWorld, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.Ingress.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-ingress-weighted.json", t)
	}
}

func TestRouteDiscoveryRouterError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.GetSidecarServiceInstancesError = errors.New("mock GetSidecarServiceInstances() error")
	url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.Router.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestRouteDiscoveryRouterWeighted(t *testing.T) {
	for _, weightConfig := range []fileConfig{weightedRouteRule, weightedRouteRuleV2} {
		_, registry, ds := commonSetup(t)
		addConfig(registry, weightConfig, t)

		// TODO: v1alpha2 only
		addConfig(registry, destinationRuleWorld, t)

		url := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.Router.ServiceNode())
		response := makeDiscoveryRequest(ds, "GET", url, t)
		compareResponse(response, "testdata/rds-router-weighted.json", t)
	}
}

func TestExternalServicesDiscoveryMode(t *testing.T) {
	testCases := []struct {
		name string
		file fileConfig
	}{
		{name: "http-none", file: externalServiceRule},
		{name: "http-dns", file: externalServiceRuleDNS},
		{name: "http-static", file: externalServiceRuleStatic},
		{name: "tcp-none", file: externalServiceRuleTCP},
		{name: "tcp-dns", file: externalServiceRuleTCPDNS},
		{name: "tcp-static", file: externalServiceRuleTCPStatic},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, registry, ds := commonSetup(t)

			if testCase.name != "none" {
				addConfig(registry, testCase.file, t)
			}

			url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response := makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s.json", testCase.name), t)

			url = fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/cds-v0-%s.json", testCase.name), t)

			port := 80
			if strings.Contains(testCase.name, "tcp") {
				port = 444
			}
			url = fmt.Sprintf("/v1/routes/%d/%s/%s", port, "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/rds-v0-%s.json", testCase.name), t)
		})
	}
}

func TestExternalServicesRoutingRules(t *testing.T) {
	testCases := []struct {
		name  string
		files []fileConfig
	}{
		{name: "weighted-external-service", files: []fileConfig{externalServiceRuleStatic, destinationRuleExternal, externalServiceRouteRule}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, registry, ds := commonSetup(t)

			for _, file := range testCase.files {
				addConfig(registry, file, t)
			}

			url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response := makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s.json", testCase.name), t)

			url = fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/cds-v0-%s.json", testCase.name), t)

			port := 80
			url = fmt.Sprintf("/v1/routes/%d/%s/%s", port, "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/rds-v0-%s.json", testCase.name), t)
		})
	}
}

func TestListenerDiscoverySidecar(t *testing.T) {
	testCases := []struct {
		name string
		file fileConfig
	}{
		{name: "none"},
		/* these configs do not affect listeners
		{
			name: "cb",
			file: cbPolicy,
		},
		{
			name: "redirect",
			file: redirectRouteRule,
		},
		{
			name: "rewrite",
			file: rewriteRouteRule,
		},
		{
			name: "websocket",
			file: websocketRouteRule,
		},
		{
			name: "timeout",
			file: timeoutRouteRule,
		},
		*/
		{
			name: "weighted",
			file: weightedRouteRule,
		},
		{
			name: "weighted",
			file: weightedRouteRuleV2,
		},
		{
			name: "fault",
			file: faultRouteRule,
		},
		{
			name: "fault",
			file: faultRouteRuleV2,
		},
		{
			name: "multi-match-fault",
			file: multiMatchFaultRouteRuleV2,
		},
		{
			name: "egress-rule",
			file: egressRule,
		},
		{
			name: "egress-rule-tcp",
			file: egressRuleTCP,
		},
		{
			name: "egress-rule", // verify the output matches egress
			file: externalServiceRule,
		},
		{
			name: "egress-rule-tcp", // verify the output matches egress
			file: externalServiceRuleTCP,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, registry, ds := commonSetup(t)

			if testCase.name != "none" {
				addConfig(registry, destinationRuleWorld, t) // TODO: v1alpha2 only
				addConfig(registry, testCase.file, t)
			}

			// test with no auth
			url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response := makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s.json", testCase.name), t)

			url = fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v1-%s.json", testCase.name), t)

			// test with no mixer
			mesh := makeMeshConfig()
			mesh.MixerAddress = ""
			ds = makeDiscoveryService(t, registry, &mesh)
			url = fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s-nomixer.json", testCase.name), t)

			url = fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v1-%s-nomixer.json", testCase.name), t)

			// test with auth
			mesh = makeMeshConfig()
			mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
			ds = makeDiscoveryService(t, registry, &mesh)
			url = fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v0-%s-auth.json", testCase.name), t)

			url = fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
			response = makeDiscoveryRequest(ds, "GET", url, t)
			compareResponse(response, fmt.Sprintf("testdata/lds-v1-%s-auth.json", testCase.name), t)
		})
	}
}

func TestListenerDiscoverySidecarAuthOptIn(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)

	// Auth opt-in on port 80
	mock.HelloService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-v0-none-auth-optin.json", t)
	mock.HelloService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_INHERIT
}

func TestListenerDiscoverySidecarAuthOptOut(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
	registry := memory.Make(model.IstioConfigTypes)

	// Auth opt-out on port 80
	mock.HelloService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_NONE
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-v0-none-auth-optout.json", t)
	mock.HelloService.Ports[0].AuthenticationPolicy = meshconfig.AuthenticationPolicy_INHERIT
}

func TestRouteDiscoverySidecarError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestRouteDiscoverySidecarError2(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.GetSidecarServiceInstancesError = errors.New("mock GetSidecarServiceInstances() error")
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV1.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestListenerDiscoveryIngress(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	addConfig(registry, egressRule, t)
	addConfig(registry, egressRuleTCP, t)

	addIngressRoutes(registry, t)
	ds := makeDiscoveryService(t, registry, &mesh)
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.Ingress.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-ingress.json", t)

	mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
	ds = makeDiscoveryService(t, registry, &mesh)
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-ingress.json", t)
}

func TestListenerDiscoverySidecarError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestListenerDiscoverySidecarError2(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.GetSidecarServiceInstancesError = errors.New("mock GetSidecarServiceInstances() error")
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestListenerDiscoveryHttpProxy(t *testing.T) {
	mesh := makeMeshConfig()
	mesh.ProxyListenPort = 0
	mesh.ProxyHttpPort = 15002
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	addConfig(registry, egressRule, t)

	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-httpproxy.json", t)
	url = fmt.Sprintf("/v1/routes/%s/%s/%s", RDSAll, "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/rds-httpproxy.json", t)
}

func TestListenerDiscoveryRouterError(t *testing.T) {
	_, _, ds := commonSetup(t)
	mockDiscovery.ServicesError = errors.New("mock Services() error")
	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.Router.ServiceNode())
	response := getDiscoveryResponse(ds, "GET", url, t)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unexpected error response from discovery: got %v, want %v",
			response.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestListenerDiscoveryRouter(t *testing.T) {
	mesh := makeMeshConfig()
	registry := memory.Make(model.IstioConfigTypes)
	ds := makeDiscoveryService(t, registry, &mesh)
	addConfig(registry, egressRule, t)

	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.Router.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-router.json", t)

	mesh.AuthPolicy = meshconfig.MeshConfig_MUTUAL_TLS
	ds = makeDiscoveryService(t, registry, &mesh)
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-router-auth.json", t)
}

func TestDiscoveryCache(t *testing.T) {
	_, _, ds := commonSetup(t)

	sds := "/v1/registration/" + mock.HelloService.Key(mock.HelloService.Ports[0], nil)
	cds := fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	rds := fmt.Sprintf("/v1/routes/80/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
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

func TestDiscoveryService_AvailabilityZone(t *testing.T) {
	tests := []struct {
		name             string
		sidecarInstances []*model.ServiceInstance
		err              error
		want             string
	}{
		{
			name: "golden path returns region/zone",
			sidecarInstances: []*model.ServiceInstance{
				{AvailabilityZone: "region/zone"},
			},
			want: "region/zone",
		},
		{
			name: "when no AZ return blank",
			sidecarInstances: []*model.ServiceInstance{
				{},
			},
			want: "",
		},
		{
			name:             "when unable to find the given cluster node tell us",
			sidecarInstances: []*model.ServiceInstance{},
			want:             "AvailabilityZone couldn't find the given cluster node",
		},
		{
			name: "when GetSidecarServiceInstances errors return that error",
			err:  errors.New("bang"),
			want: "AvailabilityZone bang",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ds := commonSetup(t)
			url := fmt.Sprintf("/v1/az/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
			mock.Discovery.WantGetSidecarServiceInstances = tt.sidecarInstances
			mockDiscovery.GetSidecarServiceInstancesError = tt.err
			response := makeDiscoveryRequest(ds, "GET", url, t)
			if tt.want != string(response) {
				t.Errorf("wanted %v but received %v", tt.want, string(response))
			}
		})
	}
}

func TestMixerclientServiceConfig(t *testing.T) {
	_, registry, ds := commonSetup(t)

	addConfig(registry, mixerclientAPISpec, t)
	addConfig(registry, mixerclientAPISpecBinding, t)
	addConfig(registry, mixerclientQuotaSpec, t)
	addConfig(registry, mixerclientQuotaSpecBinding, t)
	addConfig(registry, mixerclientAuthSpec, t)
	addConfig(registry, mixerclientAuthSpecBinding, t)

	url := fmt.Sprintf("/v1/listeners/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response := makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/lds-mixerclient-filter.json", t)

	url = fmt.Sprintf("/v1/clusters/%s/%s", "istio-proxy", mock.HelloProxyV0.ServiceNode())
	response = makeDiscoveryRequest(ds, "GET", url, t)
	compareResponse(response, "testdata/cds-mixerclient-filter.json", t)
}
