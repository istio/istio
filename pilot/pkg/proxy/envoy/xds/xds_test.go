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
package xds_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

// This file contains common helpers and initialization for the local/unit tests
// for XDS. Tests start a Pilot, configured with an in-memory endpoint registry, and
// using a file-based config, sourced from tests/testdata.
// A single instance of pilot is used by all tests - similar with the e2e environment.
// initLocalPilotTestEnv() must be called at the start of each test to ensure the
// environment is configured. Tests making modifications to services should use
// unique names for the service.
// The tests can also use a local envoy process - see TestEnvoy as example, to verify
// envoy accepts the config. Most tests are changing and checking the state of pilot.
//
// The pilot is accessible as pilotServer, which is an instance of bootstrap.Server.
// The server has a field EnvoyXdsServer which is the configured instance of the XDS service.
//
// DiscoveryServer.MemRegistry has a memory registry that can be used by tests,
// implemented in debug.go file.

var (
	testEnv        *env.TestSetup
	initMutex      sync.Mutex
	initEnvoyMutex sync.Mutex

	envoyStarted = false
	// Use 'service3' and 'app3' for pilot local tests.

	localIP = "10.3.0.3"
)

const (
	// 10.10.0.0/24 is service CIDR range

	// 10.0.0.0/9 is instance CIDR range
	app3Ip    = "10.2.0.1"
	gatewayIP = "10.3.0.1"
	ingressIP = "10.3.0.2"
)

// Common code for the xds testing.
// The tests in this package use an in-process pilot using mock service registry and
// envoy.

// Additional servers may be added here.

// One set of pilot/envoy is used for all tests, similar with the larger integration
// tests in real docker/k8s environments

// Common test environment. This is a singleton, the env will be
// used for multiple tests, for local integration testing.
func startEnvoy(t *testing.T) {
	initEnvoyMutex.Lock()
	defer initEnvoyMutex.Unlock()

	if envoyStarted {
		return
	}

	tmplB, err := ioutil.ReadFile(env.IstioSrc + "/tests/testdata/bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}
	testEnv.EnvoyTemplate = string(tmplB)
	testEnv.Dir = env.IstioSrc
	nodeID := sidecarID(app3Ip, "app3")
	testEnv.EnvoyParams = []string{"--service-cluster", "serviceCluster", "--service-node", nodeID}
	testEnv.EnvoyConfigOpt = map[string]interface{}{
		"NodeID":  nodeID,
		"BaseDir": env.IstioSrc + "/tests/testdata/local",
		// Same value used in the real template
		"meta_json_str": fmt.Sprintf(`"BASE": "%s", ISTIO_VERSION: 1.5.0`, env.IstioSrc+"/tests/testdata/local"),
	}

	if err := testEnv.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}

	envoyStarted = true
}

func sidecarID(ip, deployment string) string {
	return fmt.Sprintf("sidecar~%s~%s-644fc65469-96dza.testns~testns.svc.cluster.local", ip, deployment)
}

func gatewayID(ip string) string { //nolint: unparam
	return fmt.Sprintf("router~%s~istio-gateway-644fc65469-96dzt.istio-system~istio-system.svc.cluster.local", ip)
}

// localPilotTestEnv builds a pilot testing environment and it initializes with registry with the passed in init function.
func localPilotTestEnv(
	t *testing.T,
	initFunc func(*bootstrap.Server),
	additionalArgs ...func(*bootstrap.PilotArgs)) (*bootstrap.Server, util.TearDownFunc) { //nolint: unparam
	initMutex.Lock()
	defer initMutex.Unlock()

	additionalArgs = append(additionalArgs, func(args *bootstrap.PilotArgs) {
		args.Plugins = bootstrap.DefaultPlugins
	})
	server, tearDown := util.EnsureTestServer(additionalArgs...)
	testEnv = env.NewTestSetup(env.XDSTest, t)
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = env.IstioSrc
	testEnv.IstioOut = env.IstioOut

	localIP = getLocalIP()

	// Run the initialization function.
	initFunc(server)

	// Trigger a push, to initiate push context with contents of registry.
	server.EnvoyXdsServer.Push(&model.PushRequest{Full: true})

	// Wait till a push is propagated.
	time.Sleep(200 * time.Millisecond)

	// Add a dummy client connection to validate that push is triggered.
	dummyClient := adsConnectAndWait(t, 0x0a0a0a0a)
	defer dummyClient.Close()

	return server, tearDown
}

// initLocalPilotTestEnv creates a local, in process Pilot with XDSv2 support and a set
// of common test configs. This is a singleton server, reused for all tests in this package.
//
// The server will have a set of pre-defined instances and services, and read CRDs from the
// common tests/testdata directory.
func initLocalPilotTestEnv(t *testing.T) (*bootstrap.Server, util.TearDownFunc) {
	return localPilotTestEnv(t, func(server *bootstrap.Server) {
		// Service and endpoints for hello.default - used in v1 pilot tests
		hostname := host.Name("hello.default.svc.cluster.local")
		server.EnvoyXdsServer.MemRegistry.AddService(hostname, &model.Service{
			Hostname: hostname,
			Address:  "10.10.0.3",
			Ports:    testPorts(0),
			Attributes: model.ServiceAttributes{
				Name:      "local",
				Namespace: "default",
			},
		})

		server.EnvoyXdsServer.MemRegistry.SetEndpoints(string(hostname), "default", []*model.IstioEndpoint{
			{
				Address:         "127.0.0.1",
				EndpointPort:    uint32(testEnv.Ports().BackendPort),
				ServicePortName: "http",
				Locality:        model.Locality{Label: "az"},
				ServiceAccount:  "hello-sa",
			},
		})

		// "local" service points to the current host and the in-process mixer http test endpoint
		hostname = "local.default.svc.cluster.local"
		server.EnvoyXdsServer.MemRegistry.AddService(hostname, &model.Service{
			Hostname: hostname,
			Address:  "10.10.0.4",
			Ports: []*model.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: protocol.HTTP,
				}},
			Attributes: model.ServiceAttributes{
				Name:      "local",
				Namespace: "default",
			},
		})

		server.EnvoyXdsServer.MemRegistry.SetEndpoints(string(hostname), "default", []*model.IstioEndpoint{
			{
				Address:         localIP,
				EndpointPort:    uint32(testEnv.Ports().BackendPort),
				ServicePortName: "http",
				Locality:        model.Locality{Label: "az"},
			},
		})

		// Explicit test service, in the v2 memory registry. Similar with mock.MakeService,
		// but easier to read.
		hostname = "service3.default.svc.cluster.local"
		server.EnvoyXdsServer.MemRegistry.AddService(hostname, &model.Service{
			Hostname: hostname,
			Address:  "10.10.0.1",
			Ports:    testPorts(0),
			Attributes: model.ServiceAttributes{
				Name:      "service3",
				Namespace: "default",
			},
		})

		svc3Endpoints := make([]*model.IstioEndpoint, len(testPorts(0)))
		for i, p := range testPorts(0) {
			svc3Endpoints[i] = &model.IstioEndpoint{
				Address:         app3Ip,
				EndpointPort:    uint32(p.Port),
				ServicePortName: p.Name,
				Locality:        model.Locality{Label: "az"},
			}
		}

		server.EnvoyXdsServer.MemRegistry.SetEndpoints(string(hostname), "default", svc3Endpoints)

		// Mock ingress service
		server.EnvoyXdsServer.MemRegistry.AddService("istio-ingress.istio-system.svc.cluster.local", &model.Service{
			Hostname: "istio-ingress.istio-system.svc.cluster.local",
			Address:  "10.10.0.2",
			Ports: []*model.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: protocol.HTTP,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: protocol.HTTPS,
				},
			},
			// TODO: set attribute for this service. It may affect TestLDSIsolated as we now having service defined in istio-system namespaces
		})
		server.EnvoyXdsServer.MemRegistry.AddInstance("istio-ingress.istio-system.svc.cluster.local", &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         ingressIP,
				EndpointPort:    80,
				ServicePortName: "http",
				Locality:        model.Locality{Label: "az"},
				Labels:          labels.Instance{constants.IstioLabel: constants.IstioIngressLabelValue},
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
		server.EnvoyXdsServer.MemRegistry.AddInstance("istio-ingress.istio-system.svc.cluster.local", &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         ingressIP,
				EndpointPort:    443,
				ServicePortName: "https",
				Locality:        model.Locality{Label: "az"},
				Labels:          labels.Instance{constants.IstioLabel: constants.IstioIngressLabelValue},
			},
			ServicePort: &model.Port{
				Name:     "https",
				Port:     443,
				Protocol: protocol.HTTPS,
			},
		})

		// RouteConf Service4 is using port 80, to test that we generate multiple clusters (regression)
		// service4 has no endpoints
		server.EnvoyXdsServer.MemRegistry.AddHTTPService("service4.default.svc.cluster.local", "10.1.0.4", 80)
	})
}

// nolint: unparam
func testPorts(base int) []*model.Port {
	return []*model.Port{
		{
			Name:     "http",
			Port:     base + 80,
			Protocol: protocol.HTTP,
		}, {
			Name:     "http-status",
			Port:     base + 81,
			Protocol: protocol.HTTP,
		}, {
			Name:     "custom",
			Port:     base + 90,
			Protocol: protocol.TCP,
		}, {
			Name:     "mongo",
			Port:     base + 100,
			Protocol: protocol.Mongo,
		}, {
			Name:     "redis",
			Port:     base + 110,
			Protocol: protocol.Redis,
		}, {
			Name:     "mysql",
			Port:     base + 120,
			Protocol: protocol.MySQL,
		}, {
			Name:     "h2port",
			Port:     base + 66,
			Protocol: protocol.GRPC,
		}}
}

// Test XDS with real envoy.
func TestEnvoy(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer func() {
		if testEnv != nil {
			testEnv.TearDown()
		}
		tearDown()
	}()
	startEnvoy(t)
	// Make sure tcp port is ready before starting the test.
	env.WaitForPort(testEnv.Ports().TCPProxyPort)

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
			t.Error("Failed cds updates")
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
	proxyURL, _ := url.Parse("http://localhost:17002")

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

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

// newEndpointWithAccount is a helper for IstioEndpoint creation. Creates endpoints with
// port name "http", with the given IP, service account and a 'version' label.
// nolint: unparam
func newEndpointWithAccount(ip, account, version string) []*model.IstioEndpoint {
	return []*model.IstioEndpoint{
		{
			Address:         ip,
			ServicePortName: "http-main",
			EndpointPort:    80,
			Labels:          map[string]string{"version": version},
			UID:             "uid1",
			ServiceAccount:  account,
		},
	}
}
