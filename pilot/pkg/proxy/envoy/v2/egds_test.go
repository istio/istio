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
	"time"
	"fmt"
	"testing"
)

func TestEndpointGroupBuild(t *testing.T) {
	time.Sleep(5 * time.Second)

	fmt.Printf("Hello, world!")
	// _, tearDown := initLocalPilotWithEgdsTestEnv(t)
	// defer tearDown()
}

// func initLocalPilotWithEgdsTestEnv(t *testing.T) (*bootstrap.Server, util.TearDownFunc) {
// 	initMutex.Lock()
// 	defer initMutex.Unlock()

// 	server, tearDown := util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
// 		args.Plugins = bootstrap.DefaultPlugins
// 		args.MeshConfig.EgdsGroupSize = 2
// 	})

// 	testEnv = testenv.NewTestSetup(testenv.XDSTest, t)
// 	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
// 	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
// 	testEnv.IstioSrc = env.IstioSrc
// 	testEnv.IstioOut = env.IstioOut

// 	localIP = getLocalIP()

// 	// Service and endpoints for hello.default - used in v1 pilot tests
// 	hostname := host.Name("hello.default.svc.cluster.local")
// 	server.EnvoyXdsServer.MemRegistry.AddService(hostname, &model.Service{
// 		Hostname: hostname,
// 		Address:  "10.10.0.3",
// 		Ports:    testPorts(0),
// 		Attributes: model.ServiceAttributes{
// 			Name:      "service3",
// 			Namespace: "default",
// 		},
// 	})

// 	for i := 0; i < 7; i++ {
// 		server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
// 			Endpoint: &model.IstioEndpoint{
// 				Address:         fmt.Sprintf("127.0.0.%d", i),
// 				EndpointPort:    uint32(testEnv.Ports().BackendPort),
// 				ServicePortName: "http",
// 				Locality:        "az",
// 				ServiceAccount:  "hello-sa",
// 			},
// 			ServicePort: &model.Port{
// 				Name:     "http",
// 				Port:     80,
// 				Protocol: protocol.HTTP,
// 			},
// 		})
// 	}

// 	for i := 7; i < 15; i++ {
// 		server.EnvoyXdsServer.MemRegistry.AddInstance(hostname, &model.ServiceInstance{
// 			Endpoint: &model.IstioEndpoint{
// 				Address:         fmt.Sprintf("127.0.0.%d", i),
// 				EndpointPort:    uint32(testEnv.Ports().BackendPort),
// 				ServicePortName: "http",
// 				Locality:        "za",
// 				ServiceAccount:  "hello-za",
// 			},
// 			ServicePort: &model.Port{
// 				Name:     "http",
// 				Port:     80,
// 				Protocol: protocol.HTTP,
// 			},
// 		})
// 	}

// 	// Update cache
// 	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
// 	// TODO: channel to notify when the push is finished and to notify individual updates, for
// 	// debug and for the canary.
// 	time.Sleep(2 * time.Second)

// 	return server, tearDown
// }
