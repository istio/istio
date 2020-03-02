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

package util

import (
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/test/env"

	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	// MockPilotGrpcAddr is the address to be used for grpc connections.
	MockPilotGrpcAddr string

	// MockPilotSecureAddr is the address to be used for secure grpc connections.
	MockPilotSecureAddr string

	// MockPilotHTTPPort is the dynamic port for pilot http
	MockPilotHTTPPort int

	// MockPilotGrpcPort is the dynamic port for pilot grpc
	MockPilotGrpcPort int
)

// TearDownFunc is to be called to tear down a test server.
type TearDownFunc func()

// EnsureTestServer will ensure a pilot server is running in process and initializes
// the MockPilotUrl and MockPilotGrpcAddr to allow connections to the test pilot.
func EnsureTestServer(args ...func(*bootstrap.PilotArgs)) (*bootstrap.Server, TearDownFunc) {
	server, tearDown, err := setup(args...)
	if err != nil {
		log.Errora("Failed to start in-process server: ", err)
		panic(err)
	}
	return server, tearDown
}

func setup(additionalArgs ...func(*bootstrap.PilotArgs)) (*bootstrap.Server, TearDownFunc, error) {
	// TODO: point to test data directory
	// Setting FileDir (--configDir) disables k8s client initialization, including for registries,
	// and uses a 100ms scan. Must be used with the mock registry (or one of the others)
	// This limits the options -

	// When debugging a test or running locally it helps having a static port for /debug
	// "0" is used on shared environment (it's not actually clear if such thing exists since
	// we run the tests in isolated VMs)
	pilotHTTP := os.Getenv("PILOT_HTTP")
	if len(pilotHTTP) == 0 {
		pilotHTTP = "0"
	}
	httpAddr := ":" + pilotHTTP

	meshConfig := mesh.DefaultMeshConfig()

	bootstrap.PilotCertDir = env.IstioSrc + "/tests/testdata/certs/pilot"

	additionalArgs = append([]func(p *bootstrap.PilotArgs){func(p *bootstrap.PilotArgs) {
		p.Namespace = "testing"
		p.DiscoveryOptions = bootstrap.DiscoveryServiceOptions{
			HTTPAddr:        httpAddr,
			GrpcAddr:        ":0",
			SecureGrpcAddr:  ":0",
			EnableProfiling: true,
		}
		//TODO: start mixer first, get its address
		p.Mesh = bootstrap.MeshArgs{
			MixerAddress: "istio-mixer.istio-system:9091",
		}
		p.Config = bootstrap.ConfigArgs{
			KubeConfig: env.IstioSrc + "/tests/util/kubeconfig",
			// Static testdata, should include all configs we want to test.
			FileDir: env.IstioSrc + "/tests/testdata/config",
		}
		p.MeshConfig = &meshConfig
		p.MCPOptions.MaxMessageSize = 1024 * 1024 * 4
		p.KeepaliveOptions = keepalive.DefaultOption()
		p.ForceStop = true

		// TODO: add the plugins, so local tests are closer to reality and test full generation
		// Plugins:           bootstrap.DefaultPlugins,
	}}, additionalArgs...)
	// Create a test pilot discovery service configured to watch the tempDir.
	args := bootstrap.NewPilotArgs(additionalArgs...)

	// Create and setup the controller.
	s, err := bootstrap.NewServer(args)
	if err != nil {
		return nil, nil, err
	}

	stop := make(chan struct{})
	// Start the server.
	if err := s.Start(stop); err != nil {
		return nil, nil, err
	}

	// Extract the port from the network address.
	_, port, err := net.SplitHostPort(s.HTTPListener.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	httpURL := "http://localhost:" + port
	MockPilotHTTPPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.GRPCListener.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	MockPilotGrpcAddr = "localhost:" + port
	MockPilotGrpcPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.SecureGRPCListeningAddr.String())
	if err != nil {
		return nil, nil, err
	}
	MockPilotSecureAddr = "localhost:" + port

	// Wait a bit for the server to come up.
	err = wait.Poll(500*time.Millisecond, 5*time.Second, func() (bool, error) {
		client := &http.Client{Timeout: 1 * time.Second}
		resp, err := client.Get(httpURL + "/ready")
		if err != nil {
			return false, nil
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = ioutil.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK {
			return true, nil
		}
		return false, nil
	})
	return s, func() {
		close(stop)
	}, err
}
