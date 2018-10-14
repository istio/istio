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

package pilot

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	_ "istio.io/istio/galley/pkg/metadata"
	mixerEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	mcpserver "istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/test/env"
	mockmcp "istio.io/istio/tests/e2e/tests/pilot/mock/mcp"
	"istio.io/istio/tests/util" // Import the resource package to pull in all proto types.
)

const (
	pilotDebugPort = 5555
	pilotGrpcPort  = 15010
	mcpServerAddr  = "127.0.0.1:15014"
)

var fakeCreateTime *types.Timestamp

func TestPilotMCPClient(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	var err error
	fakeCreateTime, err = types.TimestampProto(time.Date(2018, time.January, 1, 12, 15, 30, 5e8, time.UTC))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("building & starting mock mcp server...")
	mcpServer, err := runMcpServer()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer mcpServer.Close()

	pilot := initLocalPilotTestEnv(t, mcpServerAddr, pilotGrpcPort, pilotDebugPort)
	defer pilot.Close()

	g.Eventually(func() (string, error) {
		return curlPilot(fmt.Sprintf("http://127.0.0.1:%d/debug/configz", pilotDebugPort))
	}, "30s", "1s").Should(gomega.ContainSubstring("gateway"))

	t.Log("run edge router envoy...")
	gateway := runEnvoy(t, pilotGrpcPort, pilotDebugPort)
	defer gateway.TearDown()

	t.Log("check that envoy is listening on the configured gateway...")
	gatewayResource := fmt.Sprintf("127.0.0.1:%s", "8099")
	g.Eventually(func() error {
		_, err := net.Dial("tcp", gatewayResource)
		return err
	}, "180s", "1s").Should(gomega.Succeed())
}

func mcpServerResponse(req *mcp.MeshConfigRequest) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc) {
	var cancelFunc mcpserver.CancelWatchFunc
	cancelFunc = func() {
		log.Printf("watch canceled for %s\n", req.GetTypeUrl())
	}
	if req.GetTypeUrl() == fmt.Sprintf("type.googleapis.com/%s", model.Gateway.MessageName) {
		marshaledFirstGateway, err := proto.Marshal(firstGateway)
		if err != nil {
			log.Fatalf("marshaling gateway %s\n", err)
		}
		marshaledSecondGateway, err := proto.Marshal(secondGateway)
		if err != nil {
			log.Fatalf("marshaling gateway %s\n", err)
		}

		return &mcpserver.WatchResponse{
			Version: req.GetVersionInfo(),
			TypeURL: req.GetTypeUrl(),
			Envelopes: []*mcp.Envelope{
				{
					Metadata: &mcp.Metadata{
						Name:       "some-name",
						CreateTime: fakeCreateTime,
					},
					Resource: &types.Any{
						TypeUrl: req.GetTypeUrl(),
						Value:   marshaledFirstGateway,
					},
				},
				{
					Metadata: &mcp.Metadata{
						Name:       "some-other-name",
						CreateTime: fakeCreateTime,
					},
					Resource: &types.Any{
						TypeUrl: req.GetTypeUrl(),
						Value:   marshaledSecondGateway,
					},
				},
			},
		}, cancelFunc
	}
	return &mcpserver.WatchResponse{
		Version:   req.GetVersionInfo(),
		TypeURL:   req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{},
	}, cancelFunc
}

func runMcpServer() (*mockmcp.Server, error) {
	supportedTypes := make([]string, len(model.IstioConfigTypes))
	for i, m := range model.IstioConfigTypes {
		supportedTypes[i] = fmt.Sprintf("type.googleapis.com/%s", m.MessageName)
	}

	server, err := mockmcp.NewServer(mcpServerAddr, supportedTypes, mcpServerResponse)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func runEnvoy(t *testing.T, grpcPort, debugPort uint16) *mixerEnv.TestSetup {
	t.Log("create a new envoy test environment")
	tmpl, err := ioutil.ReadFile(env.IstioSrc + "/tests/testdata/cf_bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}
	nodeIDGateway := "router~x~x~x"

	gateway := mixerEnv.NewTestSetup(25, t)
	gateway.SetNoMixer(true)
	gateway.SetNoProxy(true)
	gateway.SetNoBackend(true)
	gateway.IstioSrc = env.IstioSrc
	gateway.IstioOut = env.IstioOut
	gateway.Ports().PilotGrpcPort = grpcPort
	gateway.Ports().PilotHTTPPort = debugPort
	gateway.EnvoyConfigOpt = map[string]interface{}{
		"NodeID": nodeIDGateway,
	}
	gateway.EnvoyTemplate = string(tmpl)
	gateway.EnvoyParams = []string{
		"--service-node", nodeIDGateway,
		"--service-cluster", "x",
	}
	if err := gateway.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	return gateway
}

func initLocalPilotTestEnv(t *testing.T, mcpAddr string, grpcPort, debugPort int) io.Closer {
	mixerEnv.NewTestSetup(mixerEnv.PilotMCPTest, t)
	debugAddr := fmt.Sprintf("127.0.0.1:%d", debugPort)
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", grpcPort)
	_, cancel := util.EnsureTestServer(addMcpAddrs(mcpAddr), setupPilotDiscoveryHTTPAddr(debugAddr), setupPilotDiscoveryGrpcAddr(grpcAddr))
	return cancel
}

func addMcpAddrs(mcpServerAddr string) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		arg.MCPServerAddrs = []string{"mcp://" + mcpServerAddr}
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

var firstGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "http-8099",
				Number:   8099,
				Protocol: "http",
			},
			Hosts: []string{
				"bar.example.com",
			},
		},
	},
}

var secondGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "tcp-880",
				Number:   880,
				Protocol: "tcp",
			},
			Hosts: []string{
				"foo.example.org",
			},
		},
	},
}
