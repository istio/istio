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

	"github.com/gogo/protobuf/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/env"
)

var (
	// MockPilotGrpcAddr is the address to be used for grpc connections.
	MockPilotGrpcAddr string

	// MockPilotSecureAddr is the address to be used for secure grpc connections.
	MockPilotSecureAddr string

	// MockPilotSecurePort is the secure port
	MockPilotSecurePort int

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

	// Create a test pilot discovery service configured to watch the tempDir.
	args := bootstrap.PilotArgs{
		Namespace: "testing",
		DiscoveryOptions: envoy.DiscoveryServiceOptions{
			HTTPAddr:        httpAddr,
			GrpcAddr:        ":0",
			SecureGrpcAddr:  ":0",
			EnableCaching:   true,
			EnableProfiling: true,
		},
		//TODO: start mixer first, get its address
		Mesh: bootstrap.MeshArgs{
			MixerAddress:    "istio-mixer.istio-system:9091",
			RdsRefreshDelay: types.DurationProto(10 * time.Millisecond),
		},
		Config: bootstrap.ConfigArgs{
			KubeConfig: env.IstioSrc + "/.circleci/config",
		},
		Service: bootstrap.ServiceArgs{
			// Using the Mock service registry, which provides the hello and world services.
			Registries: []string{
				string(serviceregistry.MockRegistry)},
		},
		MCPMaxMessageSize: bootstrap.DefaultMCPMaxMsgSize,
		KeepaliveOptions:  keepalive.DefaultOption(),
		ForceStop:         true,
		// TODO: add the plugins, so local tests are closer to reality and test full generation
		// Plugins:           bootstrap.DefaultPlugins,
	}
	// Static testdata, should include all configs we want to test.
	args.Config.FileDir = env.IstioSrc + "/tests/testdata/config"

	bootstrap.PilotCertDir = env.IstioSrc + "/tests/testdata/certs/pilot"

	for _, apply := range additionalArgs {
		apply(&args)
	}

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
	_, port, err := net.SplitHostPort(s.HTTPListeningAddr.String())
	if err != nil {
		return nil, nil, err
	}
	httpURL := "http://localhost:" + port
	MockPilotHTTPPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.GRPCListeningAddr.String())
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
	MockPilotSecurePort, _ = strconv.Atoi(port)

	// Wait a bit for the server to come up.
	err = wait.Poll(500*time.Millisecond, 5*time.Second, func() (bool, error) {
		client := &http.Client{Timeout: 1 * time.Second}
		resp, err := client.Get(httpURL + "/ready")
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		ioutil.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK {
			return true, nil
		}
		return false, nil
	})
	return s, func() {
		close(stop)
	}, err
}
