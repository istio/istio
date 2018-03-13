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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
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

	"istio.io/istio/pilot/pkg/model"
	envoy "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/tests/pilot/cloudfoundry/mock"
)

const (
	pilotPort   = 5555
	copilotPort = 5556

	cfRouteOne   = "public.example.com"
	cfRouteTwo   = "public2.example.com"
	publicPort   = 10080
	backendPort  = 61005
	backendPort2 = 61006
)

func pilotURL(path string) string {
	return (&url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", pilotPort),
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
	testState := newTestState(copilotAddr)
	defer testState.tearDown()
	copilotTLSConfig := testState.creds.ServerTLSConfig()

	quitCopilotServer := make(chan struct{})
	testState.addCleanupTask(func() { close(quitCopilotServer) })
	t.Log("starting mock copilot grpc server...")
	mockCopilot, err := bootMockCopilotInBackground(copilotAddr, copilotTLSConfig, quitCopilotServer)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mockCopilot.PopulateRoute(cfRouteOne, "127.0.0.1", backendPort)
	mockCopilot.PopulateRoute(cfRouteTwo, "127.0.0.1", backendPort2)

	err = testState.copilotConfig.Save(testState.copilotConfigFilePath)
	g.Expect(err).To(gomega.BeNil())

	t.Log("saving gateway config...")

	err = ioutil.WriteFile(filepath.Join(testState.istioConfigDir, "gateway.yml"), []byte(gatewayConfig), 0600)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("building pilot...")

	pilotSession, err := runPilot(testState.copilotConfigFilePath, testState.istioConfigDir, pilotPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testState.addCleanupTask(func() {
		pilotSession.Terminate()
		g.Eventually(pilotSession, "5s").Should(gexec.Exit())
	})

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

	t.Log("checking if pilot received routes from copilot")
	g.Eventually(func() (string, error) {
		return curlPilot(pilotURL("/v1/registration"))
	}).Should(gomega.ContainSubstring(cfRouteOne))

	t.Log("checking if pilot is creating the correct listeners")
	g.Eventually(func() (string, error) {
		return curlPilot(pilotURL("/v1/listeners/x/router~x~x~x"))
	}).Should(gomega.ContainSubstring("http_connection_manager"))

	t.Log("run envoy...")

	err = testState.runEnvoy(fmt.Sprintf("127.0.0.1:%d", pilotPort))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("curling the app with expected host header")

	g.Eventually(func() error {
		respData, err := curlApp(fmt.Sprintf("http://127.0.0.1:%d", publicPort), cfRouteOne)
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
		respData, err := curlApp(fmt.Sprintf("http://127.0.0.1:%d", publicPort), cfRouteTwo)
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

func newTestState(mockCopilotServerAddress string) *testState {
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
			ServicePort: 8080, // does not matter right now, since CF only supports 1 port
		},
	}
}

func (testState *testState) runEnvoy(discoveryAddr string) error {
	config := model.DefaultProxyConfig()
	dir := os.Getenv("ISTIO_BIN")
	if dir == "" {
		dir = os.Getenv("ISTIO_OUT")
	}
	if dir == "" {
		return fmt.Errorf("envoy bin dir empty, set either ISTIO_BIN or ISTIO_OUT")
	}

	config.BinaryPath = path.Join(dir, "envoy")

	config.ConfigPath = "tmp"
	config.DiscoveryAddress = discoveryAddr
	config.ServiceCluster = "x"

	envoyConfig := envoy.BuildConfig(config, nil)
	envoyProxy := envoy.NewProxy(config, "router~x~x~x", string(log.ErrorLevel))
	abortCh := make(chan error, 1)

	cleanupSignal := errors.New("test cleanup")
	testState.addCleanupTask(func() {
		abortCh <- cleanupSignal
		os.RemoveAll(config.ConfigPath) // nolint: errcheck
	})

	go func() {
		err := envoyProxy.Run(envoyConfig, 0, abortCh)
		if err != nil && err != cleanupSignal {
			panic(fmt.Sprintf("running envoy: %s", err))
		}
	}()

	return nil
}

func (testState *testState) tearDown() {
	for i := len(testState.cleanupTasks) - 1; i >= 0; i-- {
		testState.cleanupTasks[i]()
	}
}

func bootMockCopilotInBackground(listenAddress string, tlsConfig *tls.Config, quit <-chan struct{}) (*mock.CopilotHandler, error) {
	handler := &mock.CopilotHandler{
		RoutesResponseData: make(map[string]*api.BackendSet),
	}
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
		json.NewEncoder(w).Encode(responseData) // nolint: errcheck
	})
	go http.ListenAndServe(fmt.Sprintf(":%d", port), fakeAppHandler) // nolint: errcheck
}

func runPilot(copilotConfigFile, istioConfigDir string, port int) (*gexec.Session, error) {
	path, err := gexec.Build("istio.io/istio/pilot/cmd/pilot-discovery")
	if err != nil {
		return nil, err
	}

	pilotCmd := exec.Command(path, "discovery",
		"--configDir", istioConfigDir,
		"--registries", "CloudFoundry",
		"--cfConfig", copilotConfigFile,
		"--meshConfig", "/dev/null",
		"--port", fmt.Sprintf("%d", port),
	)
	return gexec.Start(pilotCmd, nil, nil) // change these to os.Stdout when debugging
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

func curlApp(endpoint, hostHeader string) (string, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Host = hostHeader
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
