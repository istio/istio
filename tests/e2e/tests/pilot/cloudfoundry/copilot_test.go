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
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mixerEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/model"
	mcpserver "istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/test/env"
	mockmcp "istio.io/istio/tests/e2e/tests/pilot/mock/mcp"
)

const (
	pilotDebugPort     = 5555
	pilotGrpcPort      = 15010
	copilotPort        = 5556
	copilotMCPPort     = 5557
	edgeServicePort    = 8080
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
)

func pilotURL(path string) string {
	return (&url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", pilotDebugPort),
		Path:   path,
	}).String()
}

var fakeCreateTime *types.Timestamp

func TestWildcardHostEdgeRouterWithMockCopilot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	runFakeApp(app1ListenPort)
	t.Logf("1st backend is running on port %d", app1ListenPort)

	runFakeApp(app2ListenPort)
	t.Logf("2nd backend is running on port %d", app2ListenPort)

	t.Log("starting mock copilot grpc server...")
	var err error
	fakeCreateTime, err = types.TimestampProto(time.Date(2018, time.January, 1, 12, 15, 30, 5e8, time.UTC))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	copilotMCPServer, err := startMCPCopilot(mcpServerResponse)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer copilotMCPServer.Close()

	t.Log("building pilot...")
	pilotSession, err := runPilot(pilotGrpcPort, pilotDebugPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer pilotSession.Terminate()

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

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
	gateway := runEnvoy(t, "router~x~x~x", pilotGrpcPort, pilotDebugPort)
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

	copilotMCPServer, err := startMCPCopilot(mcpSidecarServerResponse)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer copilotMCPServer.Close()

	t.Log("building pilot...")
	pilotSession, err := runPilot(pilotGrpcPort, pilotDebugPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer pilotSession.Terminate()

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

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

func startMCPCopilot(serverResponse func(req *mcp.MeshConfigRequest) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc)) (*mockmcp.Server, error) {
	supportedTypes := make([]string, len(model.IstioConfigTypes))
	for i, m := range model.IstioConfigTypes {
		supportedTypes[i] = fmt.Sprintf("type.googleapis.com/%s", m.MessageName)
	}

	server, err := mockmcp.NewServer(fmt.Sprintf("127.0.0.1:%d", copilotMCPPort), supportedTypes, serverResponse)
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

func runPilot(grpcPort, debugPort int) (*gexec.Session, error) {
	path, err := gexec.Build("istio.io/istio/pilot/cmd/pilot-discovery")
	if err != nil {
		return nil, err
	}

	pilotCmd := exec.Command(path, "discovery",
		"--configDir", "/dev/null",
		"--registries", "MCP",
		"--meshConfig", "/dev/null",
		"--grpcAddr", fmt.Sprintf(":%d", grpcPort),
		"--port", fmt.Sprintf("%d", debugPort),
		"--mcpServerAddrs", fmt.Sprintf("mcp://127.0.0.1:%d", copilotMCPPort),
	)

	return gexec.Start(pilotCmd, os.Stdout, os.Stderr) // change these to os.Stdout when debugging
}

func runEnvoy(t *testing.T, nodeId string, grpcPort, debugPort uint16) *mixerEnv.TestSetup {
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
		"NodeID": nodeId,
	}
	gateway.EnvoyTemplate = string(tmpl)
	gateway.EnvoyParams = []string{
		"--service-node", nodeId,
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

var gateway_test_allConfig = map[string]map[string]proto.Message{
	fmt.Sprintf("type.googleapis.com/%s", model.Gateway.MessageName): map[string]proto.Message{
		"cloudfoundry-ingress": gateway,
	},

	fmt.Sprintf("type.googleapis.com/%s", model.VirtualService.MessageName): map[string]proto.Message{
		"vs-1": virtualService(8060, "cloudfoundry-ingress", "/some/path", cfRouteOne, subsetOne),
		"vs-2": virtualService(8070, "cloudfoundry-ingress", "", cfRouteTwo, subsetTwo),
	},

	fmt.Sprintf("type.googleapis.com/%s", model.DestinationRule.MessageName): map[string]proto.Message{
		"dr-1": destinationRule(cfRouteOne, subsetOne),
		"dr-2": destinationRule(cfRouteTwo, subsetTwo),
	},

	fmt.Sprintf("type.googleapis.com/%s", model.ServiceEntry.MessageName): map[string]proto.Message{
		"se-1": serviceEntry(8060, app1ListenPort, nil, cfRouteOne, subsetOne),
		"se-2": serviceEntry(8070, app2ListenPort, nil, cfRouteTwo, subsetTwo),
	},
}

var sidecar_test_allConfig = map[string]map[string]proto.Message{
	fmt.Sprintf("type.googleapis.com/%s", model.ServiceEntry.MessageName): map[string]proto.Message{
		"se-1": serviceEntry(sidecarServicePort, app3ListenPort, []string{"127.1.1.1"}, cfInternalRoute, subsetOne),
	},
}

func mcpSidecarServerResponse(req *mcp.MeshConfigRequest) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc) {
	var cancelFunc mcpserver.CancelWatchFunc
	cancelFunc = func() {
		log.Printf("watch canceled for %s\n", req.GetTypeUrl())
	}

	namedMsgs, ok := sidecar_test_allConfig[req.GetTypeUrl()]
	if ok {
		return buildWatchResp(req, namedMsgs), cancelFunc
	}

	return &mcpserver.WatchResponse{
		Version:   req.GetVersionInfo(),
		TypeURL:   req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{},
	}, cancelFunc
}

func mcpServerResponse(req *mcp.MeshConfigRequest) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc) {
	var cancelFunc mcpserver.CancelWatchFunc
	cancelFunc = func() {
		log.Printf("watch canceled for %s\n", req.GetTypeUrl())
	}

	namedMsgs, ok := gateway_test_allConfig[req.GetTypeUrl()]
	if ok {
		return buildWatchResp(req, namedMsgs), cancelFunc
	}

	return &mcpserver.WatchResponse{
		Version:   req.GetVersionInfo(),
		TypeURL:   req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{},
	}, cancelFunc
}

func buildWatchResp(req *mcp.MeshConfigRequest, namedMsgs map[string]proto.Message) *mcpserver.WatchResponse {
	envelopes := []*mcp.Envelope{}
	for name, msg := range namedMsgs {
		marshaledMsg, err := proto.Marshal(msg)
		if err != nil {
			log.Fatalf("marshaling %s: %s\n", name, err)
		}
		envelopes = append(envelopes, &mcp.Envelope{
			Metadata: &mcp.Metadata{
				Name:       name,
				CreateTime: fakeCreateTime,
			},
			Resource: &types.Any{
				TypeUrl: req.GetTypeUrl(),
				Value:   marshaledMsg,
			},
		})
	}
	return &mcpserver.WatchResponse{
		Version:   req.GetVersionInfo(),
		TypeURL:   req.GetTypeUrl(),
		Envelopes: envelopes,
	}
}

var gateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
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
