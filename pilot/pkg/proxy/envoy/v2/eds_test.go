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
	"context"
	"log"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"

	"istio.io/istio/tests/util"
)

// Make a direct EDS grpc request to pilot, verify the result is as expected.
func directRequest(t *testing.T) {
	conn, err := grpc.Dial(util.MockPilotGrpcAddr, grpc.WithInsecure())
	if err != nil {
		t.Fatal("Connection failed", err)
	}

	xds := xdsapi.NewEndpointDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamEndpoints(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}

	err = edsstr.Send(&xdsapi.DiscoveryRequest{
		ResourceNames: []string{"hello.default.svc.cluster.local|http"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}

	res1, err := edsstr.Recv()
	if err != nil {
		t.Fatal("Recv failed", err)
	}

	if res1.TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res1.Resources[0].TypeUrl)
	}
	cla := &xdsapi.ClusterLoadAssignment{}
	err = cla.Unmarshal(res1.Resources[0].Value)
	if err != nil {
		t.Fatal("Failed to parse proto ", err)
	}
	// TODO: validate VersionInfo and nonce once we settle on a scheme

	ep := cla.Endpoints
	if len(ep) == 0 {
		t.Fatal("No endpoints")
	}
	lbe := ep[0].LbEndpoints
	if len(lbe) == 0 {
		t.Fatal("No lb endpoints")
	}
	if "10.1.1.0" != lbe[0].Endpoint.Address.GetSocketAddress().Address {
		t.Error("Expecting 10.1.1.10 got ", lbe[0].Endpoint.Address.GetSocketAddress().Address)
	}
	t.Log(cla.String(), res1.String())

	// This should happen in 15 seconds, for the periodic refresh
	// TODO: verify push works
	res1, err = edsstr.Recv()
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}
	t.Log(res1.String())

	_ = edsstr.CloseSend()
}

func TestEds(t *testing.T) {
	server := util.EnsureTestServer()

	server.MemoryServiceDiscovery.AddService("hello.default.svc.cluster.local",
		mock.MakeService("hello.default.svc.cluster.local", "10.1.0.0"))

	// Verify services are set
	srv, err := server.ServiceController.Services()
	if err != nil {
		t.Fatal("Starting pilot", err)
	}
	log.Println(srv)

	err = startEnvoy()
	if err != nil {
		t.Error("Failed to start envoy", err)
	}

	t.Run("DirectRequest", func(t *testing.T) {
		directRequest(t)
	})
}

func startEnvoy() error {
	err := util.RunEnvoy("xds", "tests/testdata/envoy_local.json")
	return err
}
