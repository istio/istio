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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	"istio.io/istio/pilot/test/integration/cloudfoundry/mock"
	"istio.io/istio/pkg/log"
)

const (
	appPort           = 9000
	pilotPort         = 5555
	copilotAddr       = "127.0.0.1:5000"
	pilotAddr         = "127.0.0.1:5555"
	registrationRoute = "http://127.0.0.1:5555/v1/registration"
	listenersRoute    = "http://127.0.0.1:5555/v1/listeners/x/router~x~x~x"
	appEnvoyListener  = "http://127.0.0.1:6666"
)

func TestEdgeRouterWithMockCopilot(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fakeApp(appPort)
	t.Logf("fake app is running on port %d", appPort)

	testState := newTestState(copilotAddr)
	defer testState.tearDown()
	copilotTLSConfig := testState.creds.ServerTLSConfig()

	quitCopilotServer := make(chan struct{})

	testState.addCleanupTask(func() { close(quitCopilotServer) })

	t.Log("starting mock copilot grpc server...")

	mockCopilot, err := bootMockCopilotInBackground(copilotAddr, copilotTLSConfig, quitCopilotServer)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mockCopilot.PopulateRoute("abc.com", "127.0.0.1", appPort)

	err = testState.config.Save(testState.configFilePath)
	g.Expect(err).To(gomega.BeNil())

	t.Log("building pilot...")

	pilotSession, err := runPilot(testState.configFilePath, pilotPort)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testState.addCleanupTask(func() {
		pilotSession.Terminate()
		g.Eventually(pilotSession, "5s").Should(gexec.Exit())
	})

	t.Log("checking if pilot ready")
	g.Eventually(pilotSession.Out, "10s").Should(gbytes.Say(`READY`))

	t.Log("checking if pilot received routes from copilot")
	g.Eventually(func() (string, error) {
		return curlPilot(registrationRoute)
	}).Should(gomega.ContainSubstring("abc.com"))

	g.Eventually(func() (string, error) {
		return curlPilot(listenersRoute)
	}).Should(gomega.ContainSubstring("http_connection_manager"))

	t.Log("run envoy...")

	err = testState.runEnvoy(pilotAddr)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("curling the app with expected host header")

	g.Eventually(func() error {
		respData, err := curlApp(appEnvoyListener, "abc.com")
		if err != nil {
			return err
		}
		if respData != "hello" {
			return fmt.Errorf("unexpected response data: %s", respData)
		}
		return nil
	}, "30s", "100ms").Should(gomega.Succeed())
}

type testState struct {
	// generated credentials for server and client
	creds testhelpers.MTLSCredentials

	// path on disk to store the config.yaml
	configFilePath string

	config *cloudfoundry.Config

	cleanupTasks []func()
	mutex        sync.Mutex
}

func (testState *testState) addCleanupTask(task func()) {
	testState.mutex.Lock()
	defer testState.mutex.Unlock()
	testState.cleanupTasks = append(testState.cleanupTasks, task)
}

func newTestState(mockServerAddress string) *testState {
	creds := testhelpers.GenerateMTLS()
	clientTLSFiles := creds.CreateClientTLSFiles()
	return &testState{
		creds:          creds,
		configFilePath: filepath.Join(creds.TempDir, "config.yaml"),
		config: &cloudfoundry.Config{
			Copilot: cloudfoundry.CopilotConfig{
				ServerCACertPath: clientTLSFiles.ServerCA,
				ClientCertPath:   clientTLSFiles.ClientCert,
				ClientKeyPath:    clientTLSFiles.ClientKey,
				Address:          mockServerAddress,
				PollInterval:     10 * time.Second,
			},
			ServicePort: 6666,
		},
	}
}

func (testState *testState) runEnvoy(discoveryAddr string) error {
	config := model.DefaultProxyConfig()
	dir := os.Getenv("ISTIO_BIN")
	if len(dir) == 0 {
		return fmt.Errorf("envoy binary dir empty")
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
		os.RemoveAll(config.ConfigPath)
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

func fakeApp(port int) {
	fakeAppHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`hello`))
	})
	go http.ListenAndServe(fmt.Sprintf(":%d", port), fakeAppHandler)

}

func runPilot(cfConfig string, port int) (*gexec.Session, error) {
	path, err := gexec.Build("istio.io/istio/pilot/cmd/pilot-discovery")
	if err != nil {
		return nil, err
	}

	pilotCmd := exec.Command(path, "discovery",
		"--configDir", "/dev/null",
		"--registries", "CloudFoundry",
		"--cfConfig", cfConfig,
		"--meshConfig", "/dev/null",
		"--port", fmt.Sprintf("%d", port),
	)
	return gexec.Start(pilotCmd, nil, nil)
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
