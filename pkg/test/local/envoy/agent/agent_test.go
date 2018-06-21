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

package agent_test

import (
	"fmt"
	"net"
	"testing"
	"time"

	envoy_admin_v2alpha "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	envoyv1 "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/test/local/envoy"
	"istio.io/istio/pkg/test/local/envoy/agent"
	"istio.io/istio/pkg/test/local/envoy/agent/echo"
	"istio.io/istio/pkg/test/local/envoy/agent/pilot"
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
	agents := []*agent.Agent{
		{
			ServiceName: "A",
			ConfigStore: configStore,
			AppFactory: (&echo.Factory{
				Ports: []echo.PortConfig{
					{
						Name:     "http-1",
						Protocol: model.ProtocolHTTP,
					},
					{
						Name:     "http-2",
						Protocol: model.ProtocolHTTP,
					},
				},
			}).NewApplication,
			ProxyFactory: (&pilot.Factory{
				Domain:           domain,
				Namespace:        namespace,
				DiscoveryAddress: discoveryAddr,
			}).NewProxiedApplication,
		},
		{
			ServiceName: "B",
			ConfigStore: configStore,
			AppFactory: (&echo.Factory{
				Ports: []echo.PortConfig{
					{
						Name:     "http-1",
						Protocol: model.ProtocolHTTP,
					},
					{
						Name:     "http-2",
						Protocol: model.ProtocolHTTP,
					},
				},
			}).NewApplication,
			ProxyFactory: (&pilot.Factory{
				Domain:           domain,
				Namespace:        namespace,
				DiscoveryAddress: discoveryAddr,
			}).NewProxiedApplication,
		},
	}

	// Start the agents
	for _, a := range agents {
		// Start the agent
		if err := a.Start(); err != nil {
			t.Fatal(err)
		}
		defer a.Stop()
	}

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
					t.Fatal("failed to configure Envoys")
				}
				time.Sleep(retryInterval)
			}
		}
	}

	// For now, just print the final configurations.
	for _, a := range agents {
		printConfig(a)
	}
}

func printConfig(a *agent.Agent) {
	str, err := envoy.GetConfigDumpStr(a.GetProxy().GetAdminPort())
	if err == nil {
		fmt.Println(fmt.Sprintf("Agent Config for service %s: %s", a.GetProxy().GetConfig().Name, str))
	}
}

func isAgentConfiguredForService(src *agent.Agent, target *agent.Agent, t *testing.T) bool {
	t.Helper()

	cfg, err := envoy.GetConfigDump(src.GetProxy().GetAdminPort())
	if err != nil {
		t.Fatal(err)
	}

	for _, port := range target.GetProxy().GetPorts() {
		// TODO(nmittler): Verify inbound/outbound listeners exist for the target service port
		if !isClusterPresent(cfg, target.GetProxy().GetConfig().Name, fqd, uint32(port.ProxyPort), t) {
			return false
		}
	}

	return true
}

func isClusterPresent(cfg *envoy_admin_v2alpha.ConfigDump, serviceName, domain string, servicePort uint32, t *testing.T) bool {
	t.Helper()
	clusters := envoy_admin_v2alpha.ClustersConfigDump{}
	if err := clusters.Unmarshal(cfg.Configs["clusters"].Value); err != nil {
		t.Fatal(err)
	}

	edsServiceName := fmt.Sprintf("outbound|%d||%s.%s", servicePort, serviceName, domain)
	for _, c := range clusters.DynamicActiveClusters {
		if c.Cluster != nil && c.Cluster.EdsClusterConfig != nil && c.Cluster.EdsClusterConfig.ServiceName == edsServiceName {
			return true
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
		DiscoveryOptions: envoyv1.DiscoveryServiceOptions{
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
