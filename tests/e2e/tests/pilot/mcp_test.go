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
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"

	"code.cloudfoundry.org/copilot/testhelpers"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/model"
	mockmcp "istio.io/istio/tests/e2e/tests/pilot/mock/mcp"
	"istio.io/istio/tests/util"
)

const (
	pilotDebugPort = 5555
	pilotGrpcPort  = 15010
	gatewayPort    = "4321"
	mcpServerPort  = ":15014"
)

func TestPilotMCPClient(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Log("building & starting mock mcp server...")
	mcpServer, err := runMcpServer(g, t)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("building pilot...")
	istioConfigDir := testhelpers.TempDir()
	pilotSession, err := runPilot(istioConfigDir, mcpServerPort, pilotGrpcPort, pilotDebugPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

	t.Log("run edge router envoy...")
	gateway := runEnvoy(t, pilotGrpcPort, pilotDebugPort)

	defer func() {
		mcpServer.Close()
		pilotSession.Kill()
		gateway.TearDown()
	}()

	t.Log("check that envoy is listening on the configured port...")
	endpoint := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%s", "8099"), // this the gateway port that mock mcp server sends
	}

	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		t.Fatalf("Failed to make request to envoy: %v", err)
	}

	g.Eventually(func() error {
		_, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		return nil
	}, "180s", "1s").Should(gomega.Succeed())
}

func runMcpServer(g *gomega.GomegaWithT, t *testing.T) (*mockmcp.Server, error) {
	supportedTypes := []string{
		fmt.Sprintf("type.googleapis.com/%s", model.VirtualService.MessageName),
		fmt.Sprintf("type.googleapis.com/%s", model.Gateway.MessageName),
		fmt.Sprintf("type.googleapis.com/%s", model.ServiceEntry.MessageName),
		fmt.Sprintf("type.googleapis.com/%s", model.EnvoyFilter.MessageName),
	}

	server, err := mockmcp.NewServer(mcpServerPort, supportedTypes)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func runPilot(istioConfigDir, mcpServerPort string, grpcPort, debugPort int) (*gexec.Session, error) {
	path, err := gexec.Build("istio.io/istio/pilot/cmd/pilot-discovery")
	if err != nil {
		return nil, err
	}

	pilotCmd := exec.Command(path, "discovery",
		"--configDir", istioConfigDir,
		"--registries", "Mock",
		"--meshConfig", "/dev/null",
		"--grpcAddr", fmt.Sprintf(":%d", grpcPort),
		"--httpAddr", fmt.Sprintf(":%d", debugPort),
		"--mcpServerAddr", mcpServerPort,
	)

	return gexec.Start(pilotCmd, os.Stdout, os.Stderr) // change these to os.Stdout when debugging
}

func runEnvoy(t *testing.T, grpcPort, debugPort uint16) *env.TestSetup {
	t.Log("create a new envoy test environment")
	tmpl, err := ioutil.ReadFile(util.IstioSrc + "/tests/testdata/cf_bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}
	nodeIDGateway := "router~x~x~x"

	gateway := env.NewTestSetup(25, t)
	gateway.SetNoMixer(true)
	gateway.SetNoProxy(true)
	gateway.SetNoBackend(true)
	gateway.IstioSrc = util.IstioSrc
	gateway.IstioOut = util.IstioOut
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
