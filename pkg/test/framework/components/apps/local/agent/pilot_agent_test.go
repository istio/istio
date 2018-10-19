//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package agent

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/environments/local/service"

	"istio.io/istio/pkg/test/application"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	proxyEnvoy "istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/envoy"
)

const (
	timeout       = 10 * time.Second
	retryInterval = 500 * time.Millisecond

	// Enable for debugging.
	debugLogsEnabled = false
)

func TestMinimal(t *testing.T) {
	testForApps(t, &echo.Factory{
		Ports: model.PortList{
			{
				Name:     "http",
				Protocol: model.ProtocolHTTP,
			},
			{
				Name:     "grpc",
				Protocol: model.ProtocolGRPC,
			},
		},
	}, "a", "b")
}

func TestFull(t *testing.T) {
	testForApps(t, &echo.Factory{
		Ports: model.PortList{
			{
				Name:     "http",
				Protocol: model.ProtocolHTTP,
			},
			{
				Name:     "http-two",
				Protocol: model.ProtocolHTTP,
			},
			{
				Name:     "tcp",
				Protocol: model.ProtocolTCP,
			},
			{
				Name:     "https",
				Protocol: model.ProtocolHTTPS,
			},
			{
				Name:     "http2-example",
				Protocol: model.ProtocolHTTP2,
			},
			{
				Name:     "grpc",
				Protocol: model.ProtocolGRPC,
			},
		},
	}, "a", "b", "c")
}

func testForApps(t *testing.T, appFactory *echo.Factory, serviceNames ...string) {
	t.Helper()

	serviceManager := service.NewManager()
	p, pilotStopFn := newPilot(serviceManager.ConfigStore, t)
	defer pilotStopFn()

	discoveryAddr := p.GRPCListeningAddr.(*net.TCPAddr)

	// Configure the agent factory with Pilot's discovery address
	envoyLogLevel := envoy.LogLevel("")
	if debugLogsEnabled {
		envoyLogLevel = envoy.LogLevelTrace
	}
	agentFactory := (&PilotAgentFactory{
		DiscoveryAddress: discoveryAddr,
		EnvoyLogLevel:    envoyLogLevel,
	}).NewAgent

	appFactoryFunc := appFactory.NewApplication

	// Create the agents.
	agents := make([]Agent, len(serviceNames))
	for i, serviceName := range serviceNames {
		agents[i] = newAgent(serviceName, serviceManager, agentFactory, appFactoryFunc, t)
		defer agents[i].Close()
	}

	// Wait for config for all services to be distributed to all Envoys.
	endTime := time.Now().Add(timeout)
	for _, src := range agents {
		for _, target := range agents {
			if src == target {
				continue
			}

			for {
				err := src.CheckConfiguredForService(target)
				if err == nil {
					break
				}

				if time.Now().After(endTime) {
					logConfigs(agents)
					t.Fatalf("failed to configure Envoys: %v", err)
				}
				time.Sleep(retryInterval)
			}
		}
	}

	if debugLogsEnabled {
		logConfigs(agents)
	}

	// Verify that we can send traffic between services.
	for _, src := range agents {
		for _, dst := range agents {
			if src == dst {
				continue
			}

			testName := fmt.Sprintf("%v_%s_%s", model.ProtocolHTTP, src.GetConfig().Name, dst.GetConfig().Name)
			t.Run(testName, func(t *testing.T) {
				makeHTTPRequest(src, dst, model.ProtocolHTTP, t)
			})
		}
	}
}

func logConfigs(agents []Agent) {
	out := ""
	for _, a := range agents {
		dump, _ := envoy.GetConfigDumpStr(a.GetAdminPort())
		out += fmt.Sprintf("App: %s Config: %s\n", a.GetConfig().Name, dump)
	}

	f := bufio.NewWriter(os.Stdout)
	f.WriteString(out)
	f.Flush()
}

func newAgent(serviceName string, serviceManager *service.Manager, factory Factory, appFactory application.Factory, t *testing.T) Agent {
	t.Helper()
	a, err := factory(serviceName, "", serviceManager, appFactory)
	if err != nil {
		t.Fatal(err)
	}

	// Enable for debugging.
	if debugLogsEnabled {
		msg := fmt.Sprintf("Service: %s ports:\n", serviceName)
		for _, p := range a.GetPorts() {
			msg += fmt.Sprintf("   [%v]: %d->%d\n", p.Protocol, p.ProxyPort, p.ApplicationPort)
		}
		fmt.Println(msg)
	}

	return a
}

func makeHTTPRequest(src Agent, dst Agent, protocol model.Protocol, t *testing.T) {
	t.Helper()

	// Get the port information for the desired protocol on the destination agent.
	dstPort, err := FindFirstPortForProtocol(dst, protocol)
	if err != nil {
		t.Fatal(err)
	}

	serviceName := dst.GetConfig().Name

	// Forward a request from the source service to the destination service.
	parsedResponses, err := forwardRequestToAgent(src, &proto.ForwardEchoRequest{
		Url:   fmt.Sprintf("http://%s:%d", serviceName, dstPort.ProxyPort),
		Count: 1,
		Headers: []*proto.Header{
			{
				Key:   "Host",
				Value: serviceName,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !parsedResponses[0].IsOK() {
		t.Fatalf("Unexpected response status code: %s", parsedResponses[0].Code)
	}
	if parsedResponses[0].Host != serviceName {
		t.Fatalf("Unexpected host: %s", parsedResponses[0].Host)
	}
	if parsedResponses[0].Port != strconv.Itoa(dstPort.ApplicationPort) {
		t.Fatalf("Unexpected port: %s", parsedResponses[0].Port)
	}
}

func newPilot(configStore model.ConfigStoreCache, t *testing.T) (*bootstrap.Server, func()) {
	t.Helper()

	mesh := model.DefaultMeshConfig()
	bootstrapArgs := bootstrap.PilotArgs{
		Namespace: service.Namespace,
		DiscoveryOptions: proxyEnvoy.DiscoveryServiceOptions{
			HTTPAddr:       ":0",
			MonitoringAddr: ":0",
			GrpcAddr:       ":0",
			SecureGrpcAddr: ":0",
		},
		MeshConfig: &mesh,
		Config: bootstrap.ConfigArgs{
			Controller: configStore,
		},
		// Use the config store for service entries as well.
		Service: bootstrap.ServiceArgs{
			// A ServiceEntry registry is added by default, which is what we want. Don't include any other registries.
			Registries: []string{},
		},
	}

	// Create the server for the discovery service.
	server, err := bootstrap.NewServer(bootstrapArgs)
	if err != nil {
		t.Fatal(err)
	}

	// Start the server
	stop := make(chan struct{})
	if err := server.Start(stop); err != nil {
		t.Fatal(err)
	}

	stopFn := func() {
		stop <- struct{}{}
	}
	return server, stopFn
}

func forwardRequestToAgent(a Agent, req *proto.ForwardEchoRequest) ([]*echo.ParsedResponse, error) {
	grpcPortA, err := FindFirstPortForProtocol(a, model.ProtocolGRPC)
	if err != nil {
		return nil, err
	}
	client, err := echo.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPortA.ApplicationPort))
	defer client.Close()
	if err != nil {
		return nil, err
	}

	resp, err := client.ForwardEcho(req)
	if err != nil {
		return nil, err
	}

	if len(resp) != 1 {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(resp))
	}
	return resp, nil
}
