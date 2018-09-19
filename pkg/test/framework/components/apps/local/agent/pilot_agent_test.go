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
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"istio.io/istio/pkg/test/application"

	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	proxy_envoy "istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/envoy"
)

const (
	timeout       = 10 * time.Second
	retryInterval = 500 * time.Millisecond
	domain        = "svc.local"
	namespace     = "istio-system"
)

func TestAgent(t *testing.T) {
	p, configStore, pilotStopFn := newPilot(namespace, t)
	defer pilotStopFn()

	discoveryAddr := p.GRPCListeningAddr.(*net.TCPAddr)

	// Configure the agent factory with Pilot's discovery address
	agentFactory := (&PilotAgentFactory{
		DiscoveryAddress: discoveryAddr,
	}).NewAgent

	appFactory := (&echo.Factory{
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
	}).NewApplication

	// Create a few agents.
	agents := []Agent{
		newAgent("a", agentFactory, appFactory, configStore, t),
		newAgent("b", agentFactory, appFactory, configStore, t),
		newAgent("c", agentFactory, appFactory, configStore, t),
	}
	defer func() {
		for _, a := range agents {
			a.Close()
		}
	}()

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

	logConfigs(agents)

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
		out += fmt.Sprintf("NM: %s Config: %s\n", a.GetConfig().Name, dump)
	}

	f := bufio.NewWriter(os.Stdout)
	f.WriteString(out)
	f.Flush()
}

func newAgent(serviceName string, factory Factory, appFactory application.Factory, configStore model.ConfigStore, t *testing.T) Agent {
	t.Helper()
	a, err := factory(model.ConfigMeta{
		Name:      serviceName,
		Namespace: namespace,
		Domain:    domain,
	}, appFactory, configStore)
	if err != nil {
		t.Fatal(err)
	}

	msg := fmt.Sprintf("NM: %s ports:\n", serviceName)
	for _, p := range a.GetPorts() {
		msg += fmt.Sprintf("   [%v]: %d->%d\n", p.Protocol, p.ProxyPort, p.ApplicationPort)
	}
	fmt.Println(msg)

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
		Header: &proto.Header{
			Key:   "Host",
			Value: serviceName,
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

func newPilot(namespace string, t *testing.T) (*bootstrap.Server, model.ConfigStore, func()) {
	t.Helper()

	// Use an in-memory config store.
	configController := memory.NewController(memory.Make(model.IstioConfigTypes))

	mesh := model.DefaultMeshConfig()
	bootstrapArgs := bootstrap.PilotArgs{
		Namespace: namespace,
		DiscoveryOptions: proxy_envoy.DiscoveryServiceOptions{
			HTTPAddr:       ":0",
			MonitoringAddr: ":0",
			GrpcAddr:       ":0",
			SecureGrpcAddr: ":0",
		},
		MeshConfig: &mesh,
		Config: bootstrap.ConfigArgs{
			Controller: configController,
		},
		// Use the config store for service entries as well.
		Service: bootstrap.ServiceArgs{
			Registries: []string{
				string(serviceregistry.ConfigRegistry),
			},
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
	return server, configController, stopFn
}

func forwardRequestToAgent(a Agent, req *proto.ForwardEchoRequest) ([]*echo.ParsedResponse, error) {
	grpcPortA, err := FindFirstPortForProtocol(a, model.ProtocolGRPC)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", grpcPortA.ApplicationPort), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := proto.NewEchoTestServiceClient(conn)
	resp, err := client.ForwardEcho(context.Background(), req)
	if err != nil {
		return nil, err
	}
	parsedResponses := echo.ParseForwardedResponse(resp)
	if len(parsedResponses) != 1 {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(parsedResponses))
	}
	return parsedResponses, nil
}
