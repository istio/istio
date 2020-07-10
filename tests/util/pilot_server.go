// Copyright Istio Authors
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
	"fmt"
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
	"istio.io/istio/pkg/util/gogoprotomarshal"

	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	// MockPilotGrpcAddr is the address to be used for grpc connections.
	MockPilotGrpcAddr string

	MockPilotSGrpcAddr string

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
		log.Errora("Failed to vim  in-process server: ", err)
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

	// Create tmp mesh config file
	meshFile, err := ioutil.TempFile("", "mesh.yaml")
	if err != nil {
		return nil, nil, fmt.Errorf("creating tmp mesh config file failed: %v", err)
	}
	defer meshFile.Close()
	meshConfig := mesh.DefaultMeshConfig()

	// Secure GRPC address
	meshConfig.DefaultConfig.DiscoveryAddress = "localhost:15012"

	meshConfig.EnableAutoMtls.Value = false
	tearFunc := func() {
		os.Remove(meshFile.Name())
	}
	data, err := gogoprotomarshal.ToYAML(&meshConfig)
	if err != nil {
		return nil, tearFunc, fmt.Errorf("meshConfig marshal failed: %v", err)
	}
	meshFile.Write([]byte(data))

	// Use namespace istio-system, as used in production.
	additionalArgs = append([]func(p *bootstrap.PilotArgs){func(p *bootstrap.PilotArgs) {
		p.Namespace = "istio-system"
		p.ServerOptions = bootstrap.DiscoveryServerOptions{
			HTTPAddr:        httpAddr,
			GRPCAddr:        ":0",
			EnableProfiling: true,
		}
		p.RegistryOptions = bootstrap.RegistryOptions{
			KubeConfig: env.IstioSrc + "/tests/util/kubeconfig",
			// Static testdata, should include all configs we want to test.
			FileDir: env.IstioSrc + "/tests/testdata/config",
		}
		p.MCPOptions.MaxMessageSize = 1024 * 1024 * 4
		p.KeepaliveOptions = keepalive.DefaultOption()
		p.MeshConfigFile = meshFile.Name()

		// TODO: add the plugins, so local tests are closer to reality and test full generation
		// Plugins:           bootstrap.DefaultPlugins,
	}}, additionalArgs...)
	args := bootstrap.NewPilotArgs(additionalArgs...)

	// Create a test Istiod Server.
	s, err := bootstrap.NewServer(args)
	if err != nil {
		return nil, tearFunc, err
	}

	stop := make(chan struct{})
	// Start the server.
	if err := s.Start(stop); err != nil {
		return nil, tearFunc, err
	}

	// Extract the port from the network address.
	_, port, err := net.SplitHostPort(s.HTTPListener.Addr().String())
	if err != nil {
		return nil, tearFunc, err
	}
	httpURL := "http://localhost:" + port
	MockPilotHTTPPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.GRPCListener.Addr().String())
	if err != nil {
		return nil, tearFunc, err
	}
	MockPilotGrpcAddr = "localhost:" + port
	MockPilotGrpcPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.SecureGrpcListener.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	MockPilotSGrpcAddr = "localhost:" + port

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
		tearFunc()
		close(stop)
	}, err
}
