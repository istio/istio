// Copyright 2018 Istio Authors
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

package integration_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mixerEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	srmemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
	mcptesting "istio.io/istio/pkg/mcp/testing"
	"istio.io/istio/pkg/mcp/testing/groups"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

const (
	pilotDebugPort     = 5555
	pilotGrpcPort      = 15010
	sidecarServicePort = 15022

	cfRouteOne      = "public.example.com"
	cfRouteTwo      = "public2.example.com"
	cfInternalRoute = "something.apps.internal"
	cfPath          = "/some/path"
	subsetOne       = "capi-guid-1"
	subsetTwo       = "capi-guid-2"
	publicPort      = 10080
	app1ListenPort  = 61005
	app2ListenPort  = 61006
	app3ListenPort  = 6868

	ingressGatewaySvc = "cloudfoundry-ingress.istio-system.svc.cluster.local"
)

var gatewaySvc = srmemory.MakeService(ingressGatewaySvc, "11.0.0.1")
var gatewayInstance = srmemory.MakeIP(gatewaySvc, 0)

func pilotURL(path string) string {
	return (&url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", pilotDebugPort),
		Path:   path,
	}).String()
}

var fakeCreateTime2 = time.Date(2018, time.January, 1, 2, 3, 4, 5, time.UTC)

type mockController struct{}

func (c *mockController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	return nil
}

func (c *mockController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	return nil
}

func (c *mockController) Run(<-chan struct{}) {}

func TestWildcardHostEdgeRouterWithMockCopilot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	runFakeApp(app1ListenPort)
	t.Logf("1st backend is running on port %d", app1ListenPort)

	runFakeApp(app2ListenPort)
	t.Logf("2nd backend is running on port %d", app2ListenPort)

	t.Log("starting mock copilot grpc server...")
	var err error
	_, err = types.TimestampProto(time.Date(2018, time.January, 1, 12, 15, 30, 5e8, time.UTC))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	copilotMCPServer, err := startMCPCopilot()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer copilotMCPServer.Close()

	sn := snapshot.NewInMemoryBuilder()

	for _, m := range model.IstioConfigTypes {
		sn.SetVersion(m.Collection, "v0")
	}

	sn.SetEntry(model.Gateway.Collection, "cloudfoundry-ingress", "v1", fakeCreateTime2, nil, nil, gateway)

	sn.SetEntry(model.VirtualService.Collection, "vs-1", "v1", fakeCreateTime2, nil, nil,
		virtualService(8060, "cloudfoundry-ingress", "/some/path", cfRouteOne, subsetOne))
	sn.SetEntry(model.VirtualService.Collection, "vs-2", "v1", fakeCreateTime2, nil, nil,
		virtualService(8070, "cloudfoundry-ingress", "", cfRouteTwo, subsetTwo))

	sn.SetEntry(model.DestinationRule.Collection, "dr-1", "v1", fakeCreateTime2, nil, nil,
		destinationRule(cfRouteOne, subsetOne))
	sn.SetEntry(model.DestinationRule.Collection, "dr-2", "v1", fakeCreateTime2, nil, nil,
		destinationRule(cfRouteTwo, subsetTwo))

	sn.SetEntry(model.ServiceEntry.Collection, "se-1", "v1", fakeCreateTime2, nil, nil,
		serviceEntry(8060, app1ListenPort, nil, cfRouteOne, subsetOne))
	sn.SetEntry(model.ServiceEntry.Collection, "se-2", "v1", fakeCreateTime2, nil, nil,
		serviceEntry(8070, app2ListenPort, nil, cfRouteTwo, subsetTwo))

	copilotMCPServer.Cache.SetSnapshot(groups.Default, sn.Build())

	server, tearDown := initLocalPilotTestEnv(t, copilotMCPServer.Port, pilotGrpcPort, pilotDebugPort)
	defer tearDown()

	// register a service for gateway
	discovery := srmemory.NewDiscovery(
		map[model.Hostname]*model.Service{
			ingressGatewaySvc: gatewaySvc,
		}, 1)

	registry := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockCloudFoundryAdapter"),
		ClusterID:        "mockCloudFoundryAdapter",
		ServiceDiscovery: discovery,
		Controller:       &mockController{},
	}

	server.ServiceController.AddRegistry(registry)

	t.Log("checking if pilot received routes from copilot")
	g.Eventually(func() (string, error) {
		// this really should be json but the endpoint cannot
		// be unmarshaled, json is invalid
		pilotURL := pilotURL("/debug/endpointz")
		return curlPilot(pilotURL)
	}, "30s", "5s").Should(gomega.ContainSubstring(cfRouteOne))

	t.Log("checking if pilot is creating the correct listener data")
	g.Eventually(func() (string, error) {
		return curlPilot(pilotURL("/debug/configz"))
	}).Should(gomega.ContainSubstring("gateway"))

	t.Log("run edge router envoy...")
	gateway := runEnvoy(t, "router~"+gatewayInstance+"~x~x", pilotGrpcPort, pilotDebugPort)
	defer gateway.TearDown()

	t.Log("curling the app with expected host header")
	g.Eventually(func() error {
		hostRoute := url.URL{
			Host: cfRouteOne,
		}

		endpoint := url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("127.0.0.1:%d", publicPort),
			Path:   cfPath,
		}

		respData, err := curlApp(endpoint, hostRoute)
		if err != nil {
			return err
		}

		if !strings.Contains(respData, "hello") {
			return fmt.Errorf("unexpected response data: %s", respData)
		}

		if !strings.Contains(respData, cfRouteOne) {
			return fmt.Errorf("unexpected response data: %s", respData)
		}
		return nil
	}, "300s", "1s").Should(gomega.Succeed())

	g.Eventually(func() error {
		hostRoute := url.URL{
			Host: cfRouteTwo,
		}

		endpoint := url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("127.0.0.1:%d", publicPort),
		}

		respData, err := curlApp(endpoint, hostRoute)
		if err != nil {
			return err
		}
		if !strings.Contains(respData, "hello") {
			return fmt.Errorf("unexpected response data: %s", respData)
		}
		if !strings.Contains(respData, cfRouteTwo) {
			return fmt.Errorf("unexpected response data: %s", respData)
		}
		return nil
	}, "300s", "1s").Should(gomega.Succeed())
}

func TestWildcardHostSidecarRouterWithMockCopilot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	runFakeApp(app3ListenPort)
	t.Logf("internal backend is running on port %d", app3ListenPort)

	copilotMCPServer, err := startMCPCopilot()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer copilotMCPServer.Close()

	sn := snapshot.NewInMemoryBuilder()
	for _, m := range model.IstioConfigTypes {
		sn.SetVersion(m.Collection, "v0")
	}
	sn.SetEntry(model.ServiceEntry.Collection, "se-1", "v1", fakeCreateTime2, nil, nil,
		serviceEntry(sidecarServicePort, app3ListenPort, []string{"127.1.1.1"}, cfInternalRoute, subsetOne))
	copilotMCPServer.Cache.SetSnapshot(groups.Default, sn.Build())

	_, tearDown := initLocalPilotTestEnv(t, copilotMCPServer.Port, pilotGrpcPort, pilotDebugPort)
	defer tearDown()

	g.Eventually(func() (string, error) {
		// this really should be json but the endpoint cannot
		// be unmarshaled, json is invalid
		pilotURL := pilotURL("/debug/endpointz")
		return curlPilot(pilotURL)
	}, "30s", "5s").Should(gomega.ContainSubstring("127.0.0.1"))

	sidecar := runEnvoy(t, "sidecar~127.1.1.1~x~x", pilotGrpcPort, pilotDebugPort)
	defer sidecar.TearDown()

	t.Log("curling the app with expected host header")

	g.Eventually(func() error {
		hostRoute := url.URL{
			Host: cfInternalRoute,
		}

		endpoint := url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("127.1.1.1:%d", sidecarServicePort),
		}

		respData, err := curlApp(endpoint, hostRoute)
		if err != nil {
			return err
		}
		if !strings.Contains(respData, "hello") {
			return fmt.Errorf("unexpected response data: %s", respData)
		}
		if !strings.Contains(respData, cfInternalRoute) {
			return fmt.Errorf("unexpected response data: %s", respData)
		}
		return nil
	}, "300s", "1s").Should(gomega.Succeed())
}

func startMCPCopilot() (*mcptesting.Server, error) {
	collections := make([]string, len(model.IstioConfigTypes))
	for i, m := range model.IstioConfigTypes {
		collections[i] = m.Collection
	}

	server, err := mcptesting.NewServer(0, source.CollectionOptionsFromSlice(collections))
	if err != nil {
		return nil, err
	}

	return server, nil
}

func runFakeApp(port int) {
	fakeAppHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseData := map[string]interface{}{
			"hello":                "world",
			"received-host-header": r.Host,
			"received-headers":     r.Header,
		}
		json.NewEncoder(w).Encode(responseData) // nolint: errcheck
	})
	go http.ListenAndServe(fmt.Sprintf(":%d", port), fakeAppHandler) // nolint: errcheck
}

func addMcpAddrs(mcpServerPort int) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		if arg.MeshConfig == nil {
			arg.MeshConfig = &meshconfig.MeshConfig{}
		}
		arg.MeshConfig.ConfigSources = []*meshconfig.ConfigSource{
			{Address: fmt.Sprintf("127.0.0.1:%d", mcpServerPort)},
		}
	}
}

func setupPilotDiscoveryHTTPAddr(http string) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		arg.DiscoveryOptions.HTTPAddr = http
	}
}

func setupPilotDiscoveryGrpcAddr(grpc string) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		arg.DiscoveryOptions.GrpcAddr = grpc
	}
}

func initLocalPilotTestEnv(t *testing.T, mcpPort, grpcPort, debugPort int) (*bootstrap.Server, util.TearDownFunc) {
	mixerEnv.NewTestSetup(mixerEnv.PilotMCPTest, t)
	debugAddr := fmt.Sprintf("127.0.0.1:%d", debugPort)
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", grpcPort)
	return util.EnsureTestServer(addMcpAddrs(mcpPort), setupPilotDiscoveryHTTPAddr(debugAddr), setupPilotDiscoveryGrpcAddr(grpcAddr))
}

func runEnvoy(t *testing.T, nodeID string, grpcPort, debugPort uint16) *mixerEnv.TestSetup {
	t.Log("create a new envoy test environment")
	tmpl, err := ioutil.ReadFile(env.IstioSrc + "/tests/testdata/cf_bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}

	gateway := mixerEnv.NewTestSetup(25, t)
	gateway.SetNoMixer(true)
	gateway.SetNoProxy(true)
	gateway.SetNoBackend(true)
	gateway.IstioSrc = env.IstioSrc
	gateway.IstioOut = env.IstioOut
	gateway.Ports().PilotGrpcPort = grpcPort
	gateway.Ports().PilotHTTPPort = debugPort
	gateway.EnvoyConfigOpt = map[string]interface{}{
		"NodeID": nodeID,
	}
	gateway.EnvoyTemplate = string(tmpl)
	gateway.EnvoyParams = []string{
		"--service-node", nodeID,
		"--service-cluster", "x",
	}
	if err := gateway.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	return gateway
}

func curlPilot(apiEndpoint string) (string, error) {
	resp, err := http.DefaultClient.Get(apiEndpoint)
	if err != nil {
		return "", err
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(respBytes), nil
}

func curlApp(endpoint, hostRoute url.URL) (string, error) {
	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		return "", err
	}

	req.Host = hostRoute.Host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected response code %d", resp.StatusCode)
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(respBytes), nil
}

var gateway = &networking.Gateway{
	Servers: []*networking.Server{
		{
			Port: &networking.Port{
				Name:     "http",
				Number:   publicPort,
				Protocol: "http",
			},
			Hosts: []string{
				"*.example.com",
			},
		},
	},
}

func virtualService(portNum uint32, gatewayName, path, host, subset string) *networking.VirtualService {
	vs := &networking.VirtualService{
		Gateways: []string{gatewayName},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host:   host,
							Subset: subset,
							Port: &networking.PortSelector{
								Port: &networking.PortSelector_Number{Number: portNum},
							},
						},
					},
				},
			},
		},
		Hosts: []string{host},
	}
	if path != "" {
		vs.Http[0].Match = []*networking.HTTPMatchRequest{
			{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: path},
				},
			},
		}
	}
	return vs
}

func destinationRule(host, subset string) *networking.DestinationRule {
	return &networking.DestinationRule{
		Host: host,
		Subsets: []*networking.Subset{
			{
				Name:   subset,
				Labels: map[string]string{"cfapp": subset},
			},
		},
	}
}

func serviceEntry(servicePort, backendPort uint32, vips []string, host, subset string) *networking.ServiceEntry {
	return &networking.ServiceEntry{
		Hosts:     []string{host},
		Addresses: vips,
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   servicePort,
				Protocol: "http",
			},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": backendPort,
				},
				Labels: map[string]string{"cfapp": subset},
			},
		},
	}
}
