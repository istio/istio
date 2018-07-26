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

package pilot_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	envoy_admin_v2alpha "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	routeapi "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	proxy_envoy "istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/test/framework/environments/local/envoy"
	"istio.io/istio/pkg/test/framework/environments/local/envoy/agent"
	"istio.io/istio/pkg/test/framework/environments/local/envoy/agent/pilot"
	"istio.io/istio/pkg/test/service/echo"
	"istio.io/istio/pkg/test/service/echo/proto"
)

const (
	timeout       = 10 * time.Second
	retryInterval = 500 * time.Millisecond
	domain        = "svc.local"
	namespace     = "default"
)

var (
	fqd = fmt.Sprintf("%s.%s", namespace, domain)
)

func TestAgent(t *testing.T) {
	p, configStore, pilotStopFn := newPilot(namespace, t)
	defer pilotStopFn()

	discoveryAddr := p.GRPCListeningAddr.(*net.TCPAddr)

	// Configure the agent factory with Pilot's discovery address
	agentFactory := (&pilot.Factory{
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
	agents := []agent.Agent{
		newAgent("A", agentFactory, appFactory, configStore, t),
		newAgent("B", agentFactory, appFactory, configStore, t),
		newAgent("C", agentFactory, appFactory, configStore, t),
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
				if isAgentConfiguredForService(src, target, t) {
					break
				}

				if time.Now().After(endTime) {
					logConfigs(agents)
					t.Fatal("failed to configure Envoys")
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

func logConfigs(agents []agent.Agent) {
	out := ""
	for _, a := range agents {
		dump, _ := envoy.GetConfigDumpStr(a.GetAdminPort())
		out += fmt.Sprintf("NM: %s Config: %s\n", a.GetConfig().Name, dump)
	}

	f := bufio.NewWriter(os.Stdout)
	f.WriteString(out)
	f.Flush()
}

func newAgent(serviceName string, factory agent.Factory, appFactory agent.ApplicationFactory, configStore model.ConfigStore, t *testing.T) agent.Agent {
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

func makeHTTPRequest(src agent.Agent, dst agent.Agent, protocol model.Protocol, t *testing.T) {
	t.Helper()

	// Get the port information for the desired protocol on the destination agent.
	dstPort, err := agent.FindFirstPortForProtocol(dst, protocol)
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

func isAgentConfiguredForService(src agent.Agent, target agent.Agent, t *testing.T) bool {
	t.Helper()

	cfg, err := envoy.GetConfigDump(src.GetAdminPort())
	if err != nil {
		t.Fatal(err)
	}

	for _, port := range target.GetPorts() {
		clusterName := fmt.Sprintf("outbound|%d||%s.%s", port.ProxyPort, target.GetConfig().Name, fqd)
		if !isClusterPresent(cfg, clusterName) {
			return false
		}

		var listenerName string
		if port.Protocol.IsHTTP() {
			listenerName = fmt.Sprintf("0.0.0.0_%d", port.ProxyPort)
		} else {
			listenerName = fmt.Sprintf("127.0.0.1_%d", port.ProxyPort)
		}
		if !isOutboundListenerPresent(cfg, listenerName) {
			return false
		}

		if port.Protocol.IsHTTP() {
			if !isOutboundRoutePresent(cfg, clusterName) {
				return false
			}
		}
	}

	return true
}

func isClusterPresent(cfg *envoy_admin_v2alpha.ConfigDump, clusterName string) bool {
	clusters := envoy_admin_v2alpha.ClustersConfigDump{}
	if err := clusters.Unmarshal(cfg.Configs["clusters"].Value); err != nil {
		return false
	}

	for _, c := range clusters.DynamicActiveClusters {
		if c.Cluster == nil {
			continue
		}
		if c.Cluster.Name == clusterName || (c.Cluster.EdsClusterConfig != nil && c.Cluster.EdsClusterConfig.ServiceName == clusterName) {
			return true
		}
	}
	return false
}

func isOutboundListenerPresent(cfg *envoy_admin_v2alpha.ConfigDump, listenerName string) bool {
	listeners := envoy_admin_v2alpha.ListenersConfigDump{}
	if err := listeners.Unmarshal(cfg.Configs["listeners"].Value); err != nil {
		return false
	}

	for _, l := range listeners.DynamicActiveListeners {
		if l.Listener != nil && l.Listener.Name == listenerName {
			return true
		}
	}
	return false
}

func isOutboundRoutePresent(cfg *envoy_admin_v2alpha.ConfigDump, clusterName string) bool {
	routes := envoy_admin_v2alpha.RoutesConfigDump{}
	if err := routes.Unmarshal(cfg.Configs["routes"].Value); err != nil {
		return false
	}

	// Look for a route that targets the given outbound cluster.
	for _, r := range routes.DynamicRouteConfigs {
		if r.RouteConfig != nil {
			for _, vh := range r.RouteConfig.VirtualHosts {
				for _, route := range vh.Routes {
					actionRoute, ok := route.Action.(*routeapi.Route_Route)
					if !ok {
						continue
					}

					cluster, ok := actionRoute.Route.ClusterSpecifier.(*routeapi.RouteAction_Cluster)
					if !ok {
						continue
					}

					if cluster.Cluster == clusterName {
						return true
					}
				}
			}
		}
	}
	return false
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
	_, err = server.Start(stop)
	if err != nil {
		t.Fatal(err)
	}

	stopFn := func() {
		stop <- struct{}{}
	}
	return server, configController, stopFn
}

func forwardRequestToAgent(a agent.Agent, req *proto.ForwardEchoRequest) ([]*echo.ParsedResponse, error) {
	grpcPortA, err := agent.FindFirstPortForProtocol(a, model.ProtocolGRPC)
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
