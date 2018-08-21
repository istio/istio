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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"code.cloudfoundry.org/copilot/api"
	"code.cloudfoundry.org/copilot/testhelpers"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"google.golang.org/grpc"

	"istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
	"istio.io/istio/tests/e2e/tests/pilot/cloudfoundry/mock"
	"istio.io/istio/tests/util"
)

const (
	pilotDebugPort     = 5555
	pilotGrpcPort      = 15010
	copilotPort        = 5556
	edgeServicePort    = 8080
	sidecarServicePort = 15022

	cfRouteOne          = "public.example.com"
	cfRouteTwo          = "public2.example.com"
	cfInternalRoute     = "something.apps.internal"
	cfPath              = "/some/path"
	publicPort          = 10080
	backendPort         = 61005
	backendPort2        = 61006
	internalBackendPort = 6868
)

func pilotURL(path string) string {
	return (&url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", pilotDebugPort),
		Path:   path,
	}).String()
}

var gatewayConfig = fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: cloudfoundry-ingress
spec:
  servers:
  - port:
      name: http
      number: %d  # load balancer will forward traffic here
      protocol: http
    hosts:
    - "*.example.com"
`, publicPort)

func TestWildcardHostEdgeRouterWithMockCopilot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	runFakeApp(backendPort)
	t.Logf("1st backend is running on port %d", backendPort)

	runFakeApp(backendPort2)
	t.Logf("2nd backend is running on port %d", backendPort2)

	copilotAddr := fmt.Sprintf("127.0.0.1:%d", copilotPort)
	testState := newTestState(copilotAddr, edgeServicePort)
	defer testState.tearDown()
	copilotTLSConfig := testState.creds.ServerTLSConfig()

	quitCopilotServer := make(chan struct{})
	testState.addCleanupTask(func() { close(quitCopilotServer) })

	t.Log("starting mock copilot grpc server...")
	mockCopilot, err := bootMockCopilotInBackground(copilotAddr, copilotTLSConfig, quitCopilotServer)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mockCopilot.PopulateRoute(cfRouteOne, "127.0.0.1", backendPort, cfPath)
	mockCopilot.PopulateRoute(cfRouteTwo, "127.0.0.1", backendPort2, "")

	err = testState.copilotConfig.Save(testState.copilotConfigFilePath)
	g.Expect(err).To(gomega.BeNil())

	t.Log("saving gateway config...")
	err = ioutil.WriteFile(filepath.Join(testState.istioConfigDir, "gateway.yml"), []byte(gatewayConfig), 0600)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("building pilot...")
	pilotSession, err := runPilot(testState.copilotConfigFilePath, testState.istioConfigDir, pilotGrpcPort, pilotDebugPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testState.addCleanupTask(func() {
		pilotSession.Terminate()
		g.Eventually(pilotSession, "5s").Should(gexec.Exit())
	})

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

	t.Log("checking if pilot received routes from copilot")
	g.Eventually(func() (string, error) {
		// this really should be json but the endpoint cannot
		// be unmarshaled, json is invalid
		return curlPilot(pilotURL("/debug/endpointz"))
	}).Should(gomega.ContainSubstring(cfRouteOne))

	t.Log("checking if pilot is creating the correct listener data")
	g.Eventually(func() (string, error) {
		return curlPilot(pilotURL("/debug/configz"))
	}).Should(gomega.ContainSubstring("gateway"))

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
	gateway.Ports().PilotGrpcPort = pilotGrpcPort
	gateway.Ports().PilotHTTPPort = pilotDebugPort
	gateway.EnvoyConfigOpt = map[string]interface{}{
		"NodeID": nodeIDGateway,
	}
	gateway.EnvoyTemplate = string(tmpl)
	gateway.EnvoyParams = []string{
		"--service-node", nodeIDGateway,
		"--service-cluster", "x",
	}

	t.Log("run edge router envoy...")
	if err := gateway.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
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

		if !strings.Contains(respData, hostRoute.Host) {
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

	runFakeApp(internalBackendPort)
	t.Logf("internal backend is running on port %d", internalBackendPort)

	copilotAddr := fmt.Sprintf("127.0.0.1:%d", copilotPort)
	testState := newTestState(copilotAddr, sidecarServicePort)
	defer testState.tearDown()
	copilotTLSConfig := testState.creds.ServerTLSConfig()

	quitCopilotServer := make(chan struct{})
	testState.addCleanupTask(func() { close(quitCopilotServer) })

	t.Log("starting mock copilot grpc server...")
	mockCopilot, err := bootMockCopilotInBackground(copilotAddr, copilotTLSConfig, quitCopilotServer)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mockCopilot.PopulateInternalRoute(internalBackendPort, cfInternalRoute, "127.1.1.1", "127.0.0.1")

	err = testState.copilotConfig.Save(testState.copilotConfigFilePath)
	g.Expect(err).To(gomega.BeNil())

	t.Log("building pilot...")
	pilotSession, err := runPilot(testState.copilotConfigFilePath, testState.istioConfigDir, pilotGrpcPort, pilotDebugPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testState.addCleanupTask(func() {
		pilotSession.Terminate()
		g.Eventually(pilotSession, "5s").Should(gexec.Exit())
	})

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

	t.Log("create a new envoy test environment")
	tmpl, err := ioutil.ReadFile(util.IstioSrc + "/tests/testdata/cf_bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}

	nodeIDSidecar := "sidecar~127.1.1.1~x~x"

	sidecar := env.NewTestSetup(26, t)
	sidecar.SetNoMixer(true)
	sidecar.SetNoProxy(true)
	sidecar.SetNoBackend(true)
	sidecar.IstioSrc = util.IstioSrc
	sidecar.IstioOut = util.IstioOut
	sidecar.Ports().PilotGrpcPort = pilotGrpcPort
	sidecar.Ports().PilotHTTPPort = pilotDebugPort
	sidecar.EnvoyConfigOpt = map[string]interface{}{
		"NodeID": nodeIDSidecar,
	}
	sidecar.EnvoyTemplate = string(tmpl)
	sidecar.EnvoyParams = []string{
		"--service-node", nodeIDSidecar,
		"--service-cluster", "x",
	}

	t.Log("run sidecar envoy...")
	if err := sidecar.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
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

type testState struct {
	// generated credentials for copilot server and client
	creds testhelpers.MTLSCredentials

	// path on disk to store the copilot config.yaml
	copilotConfigFilePath string

	// path to directory holding istio config
	istioConfigDir string

	copilotConfig *cloudfoundry.Config

	cleanupTasks []func()
	mutex        sync.Mutex
}

func (testState *testState) addCleanupTask(task func()) {
	testState.mutex.Lock()
	defer testState.mutex.Unlock()
	testState.cleanupTasks = append(testState.cleanupTasks, task)
}

func newTestState(mockCopilotServerAddress string, servicePort int) *testState {
	creds := testhelpers.GenerateMTLS()
	clientTLSFiles := creds.CreateClientTLSFiles()
	return &testState{
		creds: creds,
		copilotConfigFilePath: filepath.Join(creds.TempDir, "config.yaml"),
		istioConfigDir:        testhelpers.TempDir(),
		copilotConfig: &cloudfoundry.Config{
			Copilot: cloudfoundry.CopilotConfig{
				ServerCACertPath: clientTLSFiles.ServerCA,
				ClientCertPath:   clientTLSFiles.ClientCert,
				ClientKeyPath:    clientTLSFiles.ClientKey,
				Address:          mockCopilotServerAddress,
				PollInterval:     10 * time.Second,
			},
			ServicePort: servicePort,
		},
	}
}

func (testState *testState) tearDown() {
	for i := len(testState.cleanupTasks) - 1; i >= 0; i-- {
		testState.cleanupTasks[i]()
	}
}

func bootMockCopilotInBackground(listenAddress string, tlsConfig *tls.Config, quit <-chan struct{}) (*mock.CopilotHandler, error) {
	handler := &mock.CopilotHandler{}
	l, err := tls.Listen("tcp", listenAddress, tlsConfig)
	if err != nil {
		return nil, err
	}
	grpcServer := grpc.NewServer()
	api.RegisterIstioCopilotServer(grpcServer, handler)
	errChan := make(chan error)
	go func() {
		errChan <- grpcServer.Serve(l)
	}()
	go func() {
		select {
		case e := <-errChan:
			fmt.Printf("grpc server errored: %s", e)
			return
		case <-quit:
			grpcServer.Stop()
			return
		}
	}()
	return handler, nil
}

func runFakeApp(port int) {
	fakeAppHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseData := map[string]interface{}{
			"hello":                "world",
			"received-host-header": r.Host,
			"received-headers":     r.Header,
		}
		json.NewEncoder(w).Encode(responseData)
	})
	go http.ListenAndServe(fmt.Sprintf(":%d", port), fakeAppHandler)
}

func runPilot(copilotConfigFile, istioConfigDir string, grpcPort, debugPort int) (*gexec.Session, error) {
	path, err := gexec.Build("istio.io/istio/pilot/cmd/pilot-discovery")
	if err != nil {
		return nil, err
	}

	pilotCmd := exec.Command(path, "discovery",
		"--configDir", istioConfigDir,
		"--registries", "CloudFoundry",
		"--cfConfig", copilotConfigFile,
		"--meshConfig", "/dev/null",
		"--grpcAddr", fmt.Sprintf(":%d", grpcPort),
		"--port", fmt.Sprintf("%d", debugPort),
	)

	return gexec.Start(pilotCmd, os.Stdout, os.Stderr) // change these to os.Stdout when debugging
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
