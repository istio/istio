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

package server_test

import (
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/cmd/pilot-discovery/server"
	"istio.io/istio/pilot/proxy/envoy"
)

type env struct {
	stop   chan struct{}
	fsRoot string
}

func (e *env) setup() (int, error) {
	e.fsRoot = createTempDir()
	e.stop = make(chan struct{})

	// Create a test pilot discovery service configured to watch the tempDir.
	args := server.PilotArgs{
		Namespace: "testing",
		DiscoveryOptions: envoy.DiscoveryServiceOptions{
			Port:            0, // An unused port will be chosen
			EnableCaching:   true,
			EnableProfiling: true,
		},
		Mesh: server.MeshArgs{
			MixerAddress:    "istio-mixer.istio-system:9091",
			RdsRefreshDelay: ptypes.DurationProto(10 * time.Millisecond),
		},
		Config: server.ConfigArgs{
			FileDir: e.fsRoot,
		},
		Service: server.ServiceArgs{
			// Using the Mock service registry, which provides the hello and world services.
			Registries: []string{string(server.MockRegistry)},
		},
	}

	// Create and setup the controller.
	s, err := server.NewServer(args)
	if err != nil {
		return 0, err
	}

	// Start the server.
	addr, err := s.Start(e.stop)
	if err != nil {
		return 0, err
	}

	// Extract the port from the network address.
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(port)
}

func (e *env) teardown() {
	close(e.stop)

	// Remove the temp dir.
	os.RemoveAll(e.fsRoot)
}

func createTempDir() string {
	// Make the temporary directory
	dir, _ := ioutil.TempDir("/tmp/", "monitor")
	_ = os.MkdirAll(dir, os.ModeDir|os.ModePerm)
	return dir
}

// TestListServices verifies that the mock services are available on the Pilot discovery service
func TestListServices(t *testing.T) {
	e := env{}
	port, err := e.setup()
	if err != nil {
		t.Error(err)
	}
	defer e.teardown()

	// Wait a bit for the server to come up.
	// TODO(nmittler): Change to polling health endpoint once https://github.com/istio/istio/pull/2002 lands.
	time.Sleep(time.Second)

	url := "http://localhost:" + strconv.Itoa(port) + "/v1/registration"
	resp, err := http.Get(url)
	if err != nil {
		t.Error(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Response had unexpected status: %d", resp.StatusCode)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Failed reading response body")
	}

	bodyString := string(bodyBytes)

	// Verify that the hello and world mock services are available.
	if !strings.Contains(bodyString, "hello.default.svc.cluster.local") {
		t.Errorf("Response missing hello service")
	}
	if !strings.Contains(bodyString, "world.default.svc.cluster.local") {
		t.Errorf("Response missing world service")
	}
}
