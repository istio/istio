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
	"bytes"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
)

var (
	// MockTestServer is used for the unit tests. Will be started once, terminated at the
	// end of the suite.
	MockTestServer *bootstrap.Server

	// MockPilotURL is the URL for the pilot http endpoint
	MockPilotURL string

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

	fsRoot string
	stop   chan struct{}
)

var (
	// IstioTop has the top of the istio tree, matches the env variable from make.
	IstioTop = os.Getenv("TOP")

	// IstioSrc is the location if istio source ($TOP/src/istio.io/istio
	IstioSrc = os.Getenv("ISTIO_GO")

	// IstioBin is the location of the binary output directory
	IstioBin = os.Getenv("ISTIO_BIN")

	// IstioOut is the location of the output directory ($TOP/out)
	IstioOut = os.Getenv("ISTIO_OUT")

	// EnvoyOutWriter captures envoy output
	// Redirect out and err from envoy to buffer - coverage tests get confused if we write to out.
	// TODO: use files
	EnvoyOutWriter bytes.Buffer

	// EnvoyErrWriter captures envoy errors
	EnvoyErrWriter bytes.Buffer
)

func init() {
	if IstioTop == "" {
		// Assume it is run inside istio.io/istio
		current, _ := os.Getwd()
		idx := strings.Index(current, "/src/istio.io/istio")
		if idx > 0 {
			IstioTop = current[0:idx]
		} else {
			IstioTop = current // launching from GOTOP (for example in goland)
		}
	}
	if IstioSrc == "" {
		IstioSrc = IstioTop + "/src/istio.io/istio"
	}
	if IstioOut == "" {
		IstioOut = IstioTop + "/out"
	}
	if IstioBin == "" {
		IstioBin = IstioTop + "/out/" + runtime.GOOS + "_" + runtime.GOARCH + "/release"
	}
}

// EnsureTestServer will ensure a pilot server is running in process and initializes
// the MockPilotUrl and MockPilotGrpcAddr to allow connections to the test pilot.
func EnsureTestServer() *bootstrap.Server {
	if MockTestServer == nil {
		err := setup()
		if err != nil {
			log.Fatal("Failed to start in-process server", err)
		}
	}
	return MockTestServer
}

func setup() error {
	// TODO: point to test data directory
	// Setting FileDir (--configDir) disables k8s client initialization, including for registries,
	// and uses a 100ms scan. Must be used with the mock registry (or one of the others)
	// This limits the options -
	stop = make(chan struct{})

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
			RdsRefreshDelay: ptypes.DurationProto(10 * time.Millisecond),
		},
		Config: bootstrap.ConfigArgs{
			KubeConfig: IstioSrc + "/.circleci/config",
		},
		Service: bootstrap.ServiceArgs{
			// Using the Mock service registry, which provides the hello and world services.
			Registries: []string{
				string(serviceregistry.MockRegistry)},
		},
	}
	// Static testdata, should include all configs we want to test.
	args.Config.FileDir = IstioSrc + "/tests/testdata/config"

	bootstrap.PilotCertDir = IstioSrc + "/tests/testdata/certs/pilot"

	// Create and setup the controller.
	s, err := bootstrap.NewServer(args)
	if err != nil {
		return err
	}

	MockTestServer = s

	// Start the server.
	_, err = s.Start(stop)
	if err != nil {
		return err
	}

	// Extract the port from the network address.
	_, port, err := net.SplitHostPort(s.HTTPListeningAddr.String())
	if err != nil {
		return err
	}
	MockPilotURL = "http://localhost:" + port
	MockPilotHTTPPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.GRPCListeningAddr.String())
	if err != nil {
		return err
	}
	MockPilotGrpcAddr = "localhost:" + port
	MockPilotGrpcPort, _ = strconv.Atoi(port)

	_, port, err = net.SplitHostPort(s.SecureGRPCListeningAddr.String())
	if err != nil {
		return err
	}
	MockPilotSecureAddr = "localhost:" + port
	MockPilotSecurePort, _ = strconv.Atoi(port)

	// Wait a bit for the server to come up.
	// TODO(nmittler): Change to polling health endpoint once https://github.com/istio/istio/pull/2002 lands.
	time.Sleep(time.Second)

	return nil
}

// Teardown will cleanup the temp dir and remove the test data.
func Teardown() {
	close(stop)

	// Remove the temp dir.
	_ = os.RemoveAll(fsRoot)
}
