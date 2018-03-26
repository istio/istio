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
package v2_test

import (
	"testing"

	testenv "istio.io/istio/mixer/test/client/env"

	"flag"
	"io/ioutil"
	"os"
	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
	"istio.io/istio/tests/util"
	"istio.io/istio/pilot/pkg/model"
)

var (
	// mixer-style test environment, includes mixer and envoy configs.
	testEnv *testenv.TestSetup
)

// Common code for the xds testing.
// The tests in this package use an in-process pilot using mock service registry and
// envoy, mixer setup using mixer local testing framework.

// Additional servers may be added here.

// One set of pilot/mixer/envoy is used for all tests, similar with the larger integration
// tests in real docker/k8s environments

// Common test environment for all tests.  Must be called - mixer setup requires a t param.
func initTest(t *testing.T) {
	if testEnv != nil {
		return
	}
	initMocks()

	testEnv = testenv.NewTestSetup(testenv.XDSTest, t)
	tmplB, err := ioutil.ReadFile(util.IstioSrc + "/tests/testdata/bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}
	testEnv.EnvoyTemplate = string(tmplB)
	testEnv.EnvoyParams = []string{"--service-cluster", "serviceCluster", "--service-node", "sidecar~1.2.3.4~test.default.cluster.local~", "--v2-config-only"}
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = util.IstioSrc
	testEnv.IstioOut = util.IstioOut

	// Mixer will push stats every 1 sec
	testenv.SetStatsUpdateInterval(testEnv.MfConfig(), 1)
	if err := testEnv.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
}

func initMocks() *bootstrap.Server {
	server := util.EnsureTestServer()

	hostname := "hello.default.svc.cluster.local"
	svc := mock.MakeService(hostname, "10.1.0.0")
	// The default service created by istio/test/util does not have a h2 port.
	// Add a H2 port to test CDS.
	// TODO: move me to discovery.go in istio/test/util
	port := &model.Port{
		Name:                 "h2port",
		Port:                 6666,
		Protocol:             model.ProtocolGRPC,
		AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
	}
	svc.Ports = append(svc.Ports, port)
	server.MemoryServiceDiscovery.AddService(hostname, svc)

	server.MemoryServiceDiscovery.AddService("hello.default.svc.cluster.local",
		mock.MakeService("hello.default.svc.cluster.local", "10.1.0.0"))

	return server
}

func TestMain(m *testing.M) {
	flag.Parse()

	defer testEnv.TearDown()

	// Run all tests.
	os.Exit(m.Run())
}
