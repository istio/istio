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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"testing"

	testenv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/util"
)

var (
	// mixer-style test environment, includes mixer and envoy configs.
	testEnv        *testenv.TestSetup
	pilotServer    *bootstrap.Server
	initMutex      sync.Mutex
	initEnvoyMutex sync.Mutex

	envoyStarted = false
	// service1 and service2 are used by mixer tests. Use 'service3' and 'app3' for pilot
	// local tests.

	// 10.10.0.0/24 is service CIDR range

	// 10.0.0.0/9 is instance CIDR range
	app3Ip    = "10.2.0.1"
	gatewayIP = "10.3.0.1"
	ingressIP = "10.3.0.2"
	localIp   = "10.3.0.3"
)

// Common code for the xds testing.
// The tests in this package use an in-process pilot using mock service registry and
// envoy, mixer setup using mixer local testing framework.

// Additional servers may be added here.

// One set of pilot/mixer/envoy is used for all tests, similar with the larger integration
// tests in real docker/k8s environments

// Common test environment, including Mixer and Watcher. This is a singleton, the env will be
// used for multiple tests, for local integration testing.
func startEnvoy(t *testing.T) {
	initEnvoyMutex.Lock()
	defer initEnvoyMutex.Unlock()

	if envoyStarted {
		return
	}

	tmplB, err := ioutil.ReadFile(util.IstioSrc + "/tests/testdata/bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}
	testEnv.EnvoyTemplate = string(tmplB)
	nodeId := sidecarId(app3Ip, "app3")
	testEnv.EnvoyParams = []string{"--service-cluster", "serviceCluster", "--service-node", nodeId}
	testEnv.EnvoyConfigOpt = map[string]interface{}{
		"NodeID": nodeId,
	}

	// Mixer will push stats every 1 sec
	testenv.SetStatsUpdateInterval(testEnv.MfConfig(), 1)
	if err := testEnv.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	envoyStarted = true
}

func sidecarId(ip, deployment string) string {
	return fmt.Sprintf("sidecar~%s~%s-644fc65469-96dza.testns~testns.svc.cluster.local", ip, deployment)
}

func gatewayId(ip string) string {
	return fmt.Sprintf("router~%s~istio-gateway-644fc65469-96dzt.istio-system~istio-system.svc.cluster.local", ip)
}

func ingressId(ip string) string {
	return fmt.Sprintf("ingress~%s~istio-ingress-7cd767fcb4-kl6gt.pilot-noauth-system~pilot-noauth-system.svc.cluster.local", ip)
}

// initLocalPilotTestEnv creates a local, in process Pilot with XDSv2 support and a set
// of common test configs. This is a singleton server, reused for all tests in this package.
//
// The server will have a set of pre-defined instances and services, and read CRDs from the
// common tests/testdata directory.
func initLocalPilotTestEnv(t *testing.T) *bootstrap.Server {
	initMutex.Lock()
	defer initMutex.Unlock()
	if pilotServer != nil {
		return pilotServer
	}
	testEnv = testenv.NewTestSetup(testenv.XDSTest, t)
	server := util.EnsureTestServer()
	pilotServer = server

	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = util.IstioSrc
	testEnv.IstioOut = util.IstioOut

	localIp = getLocalIP()

	// Service and endpoints for hello.default - used in v1 pilot tests
	hostname := model.Hostname("hello.default.svc.cluster.local")
	server.EnvoyXdsServer.MemRegistry.AddService(hostname, &model.Service{
		Hostname: hostname,
		Address:  "10.10.0.3",
		Ports:    testPorts(0),
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: "127.0.0.1",
			Port:    int(testEnv.Ports().BackendPort),
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
		AvailabilityZone: "az",
	})

	// "local" service points to the current host and the in-process mixer http test endpoint
	server.EnvoyXdsServer.MemRegistry.AddService("local.default.svc.cluster.local", &model.Service{
		Hostname: "local.default.svc.cluster.local",
		Address:  "10.10.0.4",
		Ports: []*model.Port{
			{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			}},
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("local.default.svc.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: localIp,
			Port:    int(testEnv.Ports().BackendPort),
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
		AvailabilityZone: "az",
	})

	// Explicit test service, in the v2 memory registry. Similar with mock.MakeService,
	// but easier to read.
	server.EnvoyXdsServer.MemRegistry.AddService("service3.default.svc.cluster.local", &model.Service{
		Hostname: "service3.default.svc.cluster.local",
		Address:  "10.10.0.1",
		Ports:    testPorts(0),
	})

	server.EnvoyXdsServer.MemRegistry.AddInstance("service3.default.svc.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: app3Ip,
			Port:    2080,
			ServicePort: &model.Port{
				Name:     "http-main",
				Port:     1080,
				Protocol: model.ProtocolHTTP,
			},
		},
		Labels:           map[string]string{"version": "v1"},
		AvailabilityZone: "az",
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("service3.default.svc.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: gatewayIP,
			Port:    2080,
			ServicePort: &model.Port{
				Name:     "http-main",
				Port:     1080,
				Protocol: model.ProtocolHTTP,
			},
		},
		Labels:           map[string]string{"version": "v2", "app": "my-gateway-controller"},
		AvailabilityZone: "az",
	})

	// Mock ingress service
	server.EnvoyXdsServer.MemRegistry.AddService("istio-ingress.istio-system.svc.cluster.local", &model.Service{
		Hostname: "istio-ingress.istio-system.svc.cluster.local",
		Address:  "10.10.0.2",
		Ports: []*model.Port{
			{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
			{
				Name:     "https",
				Port:     443,
				Protocol: model.ProtocolHTTPS,
			},
		},
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("istio-ingress.istio-system.svc.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: ingressIP,
			Port:    80,
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
		Labels:           model.IstioIngressWorkloadLabels,
		AvailabilityZone: "az",
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("istio-ingress.istio-system.svc.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: ingressIP,
			Port:    443,
			ServicePort: &model.Port{
				Name:     "https",
				Port:     443,
				Protocol: model.ProtocolHTTPS,
			},
		},
		Labels:           model.IstioIngressWorkloadLabels,
		AvailabilityZone: "az",
	})

	//RouteConf Service4 is using port 80, to test that we generate multiple clusters (regression)
	// service4 has no endpoints
	server.EnvoyXdsServer.MemRegistry.AddService("service4.default.svc.cluster.local", &model.Service{
		Hostname: "service4.default.svc.cluster.local",
		Address:  "10.1.0.4",
		Ports: []*model.Port{
			{
				Name:     "http-main",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
	})

	// Update cache
	server.EnvoyXdsServer.ClearCacheFunc()()

	return server
}

func testPorts(base int) []*model.Port {
	return []*model.Port{
		{
			Name:     "http",
			Port:     base + 80,
			Protocol: model.ProtocolHTTP,
		}, {
			Name:     "http-status",
			Port:     base + 81,
			Protocol: model.ProtocolHTTP,
		}, {
			Name:     "custom",
			Port:     base + 90,
			Protocol: model.ProtocolTCP,
		}, {
			Name:     "mongo",
			Port:     base + 100,
			Protocol: model.ProtocolMongo,
		},
		{
			Name:     "redis",
			Port:     base + 110,
			Protocol: model.ProtocolRedis,
		}, {
			Name:     "h2port",
			Port:     base + 66,
			Protocol: model.ProtocolGRPC,
		}}
}

// Test XDS with real envoy and with mixer.
func TestEnvoy(t *testing.T) {
	defer func() {
		if testEnv != nil {
			testEnv.TearDown()
		}
	}()

	initLocalPilotTestEnv(t)
	startEnvoy(t)
	// Make sure tcp port is ready before starting the test.
	testenv.WaitForPort(testEnv.Ports().TCPProxyPort)

	t.Run("envoyInit", envoyInit)
	t.Run("service", testService)
}

// envoyInit verifies envoy has accepted the config from pilot by checking the stats.
func envoyInit(t *testing.T) {
	statsURL := fmt.Sprintf("http://localhost:%d/stats?format=json", testEnv.Ports().AdminPort)
	res, err := http.Get(statsURL)
	if err != nil {
		t.Fatal("Failed to get stats, envoy not started")
	}
	statsBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal("Failed to get stats, envoy not started")
	}

	statsMap := stats2map(statsBytes)

	if statsMap["cluster_manager.cds.update_success"] < 1 {
		t.Error("Failed cds update")
	}
	// Other interesting values for CDS: cluster_added: 19, active_clusters
	// cds.update_attempt: 2, cds.update_rejected, cds.version
	for _, port := range testPorts(0) {
		stat := fmt.Sprintf("cluster.outbound|%d||service3.default.svc.cluster.local.update_success", port.Port)
		if statsMap[stat] < 1 {
			t.Error("Failed sds updates")
		}
	}

	if statsMap["cluster.xds-grpc.update_failure"] > 0 {
		t.Error("GRPC update failure")
	}

	if statsMap["listener_manager.lds.update_rejected"] > 0 {
		t.Error("LDS update failure")
	}
	if statsMap["listener_manager.lds.update_success"] < 1 {
		t.Error("LDS update failure")
	}
}

// Example of using a local test connecting to the in-process test service, using Envoy http proxy
// mode. This is also a test for http proxy (finally).
func testService(t *testing.T) {
	proxyUrl, _ := url.Parse("http://localhost:17002")

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}

	res, err := client.Get("http://local.default.svc.cluster.local")
	if err != nil {
		t.Error("Failed to access proxy", err)
		return
	}
	resdmp, _ := httputil.DumpResponse(res, true)
	t.Log(string(resdmp))
	if res.Status != "200 OK" {
		t.Error("Proxy failed ", res.Status)
	}
}

// EnvoyStat is used to parse envoy stats
type EnvoyStat struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// stats2map parses envoy stats.
func stats2map(stats []byte) map[string]int {
	s := struct {
		Stats []EnvoyStat `json:"stats"`
	}{}
	_ = json.Unmarshal(stats, &s)
	m := map[string]int{}
	for _, stat := range s.Stats {
		m[stat.Name] = stat.Value
	}
	return m
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
func TestMain(m *testing.M) {
	flag.Parse()
	// Run all tests.
	os.Exit(m.Run())
}
