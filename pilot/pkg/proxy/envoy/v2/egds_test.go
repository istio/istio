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
	"fmt"
	"istio.io/istio/pkg/adsc"
	"testing"
	"time"

	testenv "istio.io/istio/mixer/test/client/env"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

func addInitSvcAndEndpoints(server *bootstrap.Server) *model.Service {
	// Service and endpoints for hello.default - used in v1 pilot tests
	hostname := host.Name("hello.default.svc.cluster.local")
	svc := &model.Service{
		Hostname: hostname,
		Address:  "10.10.0.3",
		Ports:    testPorts(0),
		Attributes: model.ServiceAttributes{
			Name:      "service3",
			Namespace: "default",
		},
	}

	server.EnvoyXdsServer.MemRegistry.AddService(hostname, svc)

	for i := 0; i < 7; i++ {
		server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         fmt.Sprintf("127.0.0.%d", i),
				EndpointPort:    uint32(testEnv.Ports().BackendPort),
				ServicePortName: "http",
				Locality:        "az",
				ServiceAccount:  "hello-sa",
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
	}

	for i := 7; i < 15; i++ {
		server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         fmt.Sprintf("127.0.0.%d", i),
				EndpointPort:    uint32(testEnv.Ports().BackendPort),
				ServicePortName: "http",
				Locality:        "za",
				ServiceAccount:  "hello-za",
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
	}

	// Update cache
	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	time.Sleep(2 * time.Second)

	return svc
}

func iniPilotServerWithEgds(t *testing.T) (*bootstrap.Server, util.TearDownFunc) {
	initMutex.Lock()
	defer initMutex.Unlock()

	server, tearDown := util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
		args.Plugins = bootstrap.DefaultPlugins
		args.MeshConfig.EgdsGroupSize = 2
	})

	testEnv = testenv.NewTestSetup(testenv.XDSTest, t)
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = env.IstioSrc
	testEnv.IstioOut = env.IstioOut

	localIP = getLocalIP()

	return server, tearDown
}

func adsConnectWithEgdsAndWait(t *testing.T, ip int) *adsc.ADSC {
	adscConn, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		IP: testIP(uint32(ip)),
	})
	if err != nil {
		t.Fatal("Error connecting ", err)
	}
	adscConn.Watch()
	_, err = adscConn.Wait(10*time.Second, "eds", "lds", "cds", "rds", "egds")
	if err != nil {
		t.Fatal("Error getting initial config ", err)
	}

	if len(adscConn.GetEndpoints()) == 0 {
		t.Fatal("No endpoints")
	}
	return adscConn
}

func TestEndpointGroupReshard(t *testing.T) {
	server, tearDown := iniPilotServerWithEgds(t)
	defer tearDown()

	svc := addInitSvcAndEndpoints(server)
	hostname := svc.Hostname

	shards := server.EnvoyXdsServer.EndpointShardsByService[string(hostname)][svc.Attributes.Namespace].Shards
	clusterCount := len(shards)
	if clusterCount != 1 {
		t.Errorf("Expect cluster count 1, got %d", clusterCount)
	}

	memClusterID := "v2-debug"
	if _, f := shards[memClusterID]; !f {
		t.Errorf("Unable to find the memory cluster with clusterID: %s", memClusterID)
	}

	// Now test the group count. We've 15 endpoints for service "hello.default.svc.cluster.local" and every group
	// size is set to 2, so the group count for a cluster should be 8
	groupCount := len(shards[memClusterID].IstioEndpointGroups)
	if groupCount != 8 {
		t.Errorf("In correct group sharding count. Except 8, got %d", groupCount)
	}

	// Now increase the endpoints but do not trigger reshard event
	for i := 15; i < 25; i++ {
		server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         fmt.Sprintf("127.0.0.%d", i),
				EndpointPort:    uint32(testEnv.Ports().BackendPort),
				ServicePortName: "http",
				Locality:        "za",
				ServiceAccount:  "hello-za",
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
	}

	// Update cache
	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	time.Sleep(2 * time.Second)

	shards = server.EnvoyXdsServer.EndpointShardsByService[string(hostname)][svc.Attributes.Namespace].Shards
	memClusterID = "v2-debug"
	groupCount = len(shards[memClusterID].IstioEndpointGroups)
	if groupCount != 8 {
		t.Errorf("In correct group sharding count. Except 8, got %d", groupCount)
	}

	// And the all endpoints for a group should exist
	endpointCount := len(shards[memClusterID].IstioEndpoints)
	if endpointCount != 25 {
		t.Errorf("In correct endpoint count. Except 25, got %d", groupCount)
	}

	// Now increase the endpoints and trigger reshard event
	for i := 25; i < 35; i++ {
		server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
			Endpoint: &model.IstioEndpoint{
				Address:         fmt.Sprintf("127.0.0.%d", i),
				EndpointPort:    uint32(testEnv.Ports().BackendPort),
				ServicePortName: "http",
				Locality:        "za",
				ServiceAccount:  "hello-za",
			},
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			},
		})
	}

	// Update cache
	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	time.Sleep(2 * time.Second)

	shards = server.EnvoyXdsServer.EndpointShardsByService[string(hostname)][svc.Attributes.Namespace].Shards
	memClusterID = "v2-debug"
	groupCount = len(shards[memClusterID].IstioEndpointGroups)
	if groupCount != 18 {
		t.Errorf("In correct group sharding count. Except 18, got %d", groupCount)
	}

	// And the all endpoints for a group should exist
	endpointCount = len(shards[memClusterID].IstioEndpoints)
	if endpointCount != 35 {
		t.Errorf("In correct endpoint count. Except 35, got %d", groupCount)
	}
}

func TestPushEdsWithEgds(t *testing.T) {
	server, tearDown := iniPilotServerWithEgds(t)
	defer tearDown()

	svc := addInitSvcAndEndpoints(server)

	// Receive full push
	adscConn := adsConnectWithEgdsAndWait(t, 0x0a0a0a0a)
	defer adscConn.Close()

	// We remove 2 endpoints from each locality
	istioEndpoints := make([]*model.IstioEndpoint, 0, 10)
	for i := 9; i < 15; i++ {
		ep := &model.IstioEndpoint{
			Address:         fmt.Sprintf("127.0.0.%d", i),
			EndpointPort:    uint32(testEnv.Ports().BackendPort),
			ServicePortName: "http",
			Locality:        "za",
			ServiceAccount:  "hello-za",
		}

		istioEndpoints = append(istioEndpoints, ep)
	}

	for i := 2; i < 7; i++ {
		ep := &model.IstioEndpoint{
			Address:         fmt.Sprintf("127.0.0.%d", i),
			EndpointPort:    uint32(testEnv.Ports().BackendPort),
			ServicePortName: "http",
			Locality:        "az",
			ServiceAccount:  "hello-sa",
		}

		istioEndpoints = append(istioEndpoints, ep)
	}

	server.EnvoyXdsServer.MemRegistry.SetEndpoints(string(svc.Hostname), svc.Attributes.Namespace, istioEndpoints)

	// We should receive EGDS updates
	_, err := adscConn.Wait(10*time.Second, "egds")
	if err != nil {
		t.Errorf("failed while receiving EGDS updates. Reason: %s", err)
	}
}

func TestBuildEndpointGroup(t *testing.T) {
	server, tearDown := iniPilotServerWithEgds(t)
	defer tearDown()

	svc := addInitSvcAndEndpoints(server)
	hostname := svc.Hostname

	namcespaceCount := len(server.EnvoyXdsServer.EndpointShardsByService[string(hostname)])
	if namcespaceCount != 1 {
		t.Errorf("Expect namespace count 1, got %d", namcespaceCount)
	}

	shards := server.EnvoyXdsServer.EndpointShardsByService[string(hostname)][svc.Attributes.Namespace].Shards
	clusterCount := len(shards)
	if clusterCount != 1 {
		t.Errorf("Expect cluster count 1, got %d", clusterCount)
	}

	memClusterID := "v2-debug"
	if _, f := shards[memClusterID]; !f {
		t.Errorf("Unable to find the memory cluster with clusterID: %s", memClusterID)
	}

	// Now test the group count. We've 15 endpoints for service "hello.default.svc.cluster.local" and every group
	// size is set to 2, so the group count for a cluster should be 8
	groupCount := len(shards[memClusterID].IstioEndpointGroups)
	if groupCount != 8 {
		t.Errorf("In correct group sharding count. Except 8, got %d", groupCount)
	}

	// And the all endpoints for a group should exist
	endpointCount := len(shards[memClusterID].IstioEndpoints)
	if endpointCount != 15 {
		t.Errorf("In correct endpoint count. Except 15, got %d", groupCount)
	}

	// Now test the endpoint group name
	for ix := 0; ix < 8; ix++ {
		// The first group contains the version and its first value is 1
		groupName := fmt.Sprintf("%s|%s|%s|%d-1", hostname, svc.Attributes.Namespace, memClusterID, ix)
		if endpoints, f := shards[memClusterID].IstioEndpointGroups[groupName]; !f {
			t.Errorf("Expect group name %s not found", groupName)
		} else {
			// Endpoint count in every group should not have endpoints double than designed
			if len(endpoints)-4 > 0 {
				t.Errorf("Got too many endpoints in a group, was %d, designed %d", len(endpoints), 2)
			}
		}
	}
}
