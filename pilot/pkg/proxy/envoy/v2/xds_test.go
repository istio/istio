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

	"fmt"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
	"istio.io/istio/tests/util"
)

var (
	// mixer-style test environment, includes mixer and envoy configs.
	testEnv *testenv.TestSetup

	// service1 and service2 are used by mixer tests. Use 'service3' and 'app3' for pilot
	// local tests.

	app3Ip = "10.2.0.1"
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
	testEnv.EnvoyParams = []string{"--service-cluster", "serviceCluster", "--service-node", sidecarId(app3Ip, "app3"), "--v2-config-only"}
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

func sidecarId(ip, deployment string) string {
	return fmt.Sprintf("sidecar~%s~%s-644fc65469-96dza.testns~testns.svc.cluster.local", ip, deployment)
}

func ingressId() string {
	return fmt.Sprintf("ingress~~istio-ingress-644fc65469-96dzt.istio-system~istio-system.svc.cluster.local")
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

	// Explicit test service, in the v2 memory registry. Similar with mock.MakeService,
	// but easier to read.
	server.EnvoyXdsServer.MemRegistry.AddService("service3", &model.Service{
		Hostname: "service3.default.svc.cluster.local",
		Address:  "10.1.0.1",
		Ports:    testPorts(1000),
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("service3", "app3", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: app3Ip,
			Port:    2080,
			ServicePort: &model.Port{
				Name:                 "http-main",
				Port:                 1080,
				Protocol:             model.ProtocolHTTP,
				AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
			},
		},
		Labels:           map[string]string{"version": "1"},
		AvailabilityZone: "az",
	})

	return server
}

func testPorts(base int) []*model.Port {
	return []*model.Port{
		{
			Name:                 "http-main",
			Port:                 base + 80,
			Protocol:             model.ProtocolHTTP,
			AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
		}, {
			Name:                 "http-status",
			Port:                 base + 81,
			Protocol:             model.ProtocolHTTP,
			AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
		}, {
			Name:                 "custom",
			Port:                 base + 90,
			Protocol:             model.ProtocolTCP,
			AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
		}, {
			Name:                 "mongo",
			Port:                 base + 100,
			Protocol:             model.ProtocolMongo,
			AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
		},
		{
			Name:                 "redis",
			Port:                 base + 110,
			Protocol:             model.ProtocolRedis,
			AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
		}, {
			Name:                 "h2port",
			Port:                 base + 66,
			Protocol:             model.ProtocolGRPC,
			AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT,
		}}
}

func TestEnvoy(t *testing.T) {
	initTest(t)
	// Make sure tcp port is ready before starting the test.
	testenv.WaitForPort(testEnv.Ports().TCPProxyPort)

	//url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().TCPProxyPort)

}



func TestMain(m *testing.M) {
	flag.Parse()

	defer testEnv.TearDown()

	// Run all tests.
	os.Exit(m.Run())
}
