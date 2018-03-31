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
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"google.golang.org/grpc"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"

	"istio.io/istio/tests/util"
)

const (
	typePrefix = "type.googleapis.com/envoy.api.v2."

	// Constants used for XDS
	endpointType = typePrefix + "ClusterLoadAssignment"
)

func connectADS(t *testing.T) ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient {
	conn, err := grpc.Dial(util.MockPilotGrpcAddr, grpc.WithInsecure())
	if err != nil {
		t.Fatal("Connection failed", err)
	}

	xds := ads.NewAggregatedDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}
	err = edsstr.Send(&xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: "sidecar~a~b~c",
		},
		TypeUrl: endpointType,
		ResourceNames: []string{"hello.default.svc.cluster.local|http"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}
	return edsstr
}

func connect(t *testing.T) xdsapi.EndpointDiscoveryService_StreamEndpointsClient {
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
		Node: &envoy_api_v2_core1.Node{
			Id: "sidecar~a~b~c",
		},
		ResourceNames: []string{"hello.default.svc.cluster.local|http"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}
	return edsstr
}

func reconnect(server *bootstrap.Server, res *xdsapi.DiscoveryResponse, t *testing.T) xdsapi.EndpointDiscoveryService_StreamEndpointsClient {
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
		Node: &envoy_api_v2_core1.Node{
			Id: "sidecar~a~b~c",
		},
		VersionInfo:   res.VersionInfo,
		ResponseNonce: res.Nonce,
		ResourceNames: []string{"hello.default.svc.cluster.local|http"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}
	return edsstr
}

// Regression for envoy restart and overlapping connections
func TestReconnectWithNonce(t *testing.T) {
	server := initLocalPilotTestEnv()
	edsstr := connect(t)
	res, _ := edsstr.Recv()

	// closes old process
	_ = edsstr.CloseSend()

	edsstr = reconnect(server, res, t)
	res, _ = edsstr.Recv()
	_ = edsstr.CloseSend()

	t.Log("Received ", res)
}

// Regression for envoy restart and overlapping connections
func TestReconnect(t *testing.T) {
	initLocalPilotTestEnv()
	edsstr := connect(t)
	_, _ = edsstr.Recv()

	// envoy restarts and reconnects
	edsstr2 := connect(t)
	_, _ = edsstr2.Recv()

	// closes old process
	_ = edsstr.CloseSend()

	time.Sleep(1 * time.Second)

	// event happens
	v2.PushAll()
	// will trigger recompute and push (we may need to make a change once diff is implemented

	done := make(chan struct{}, 1)
	go func() {
		t := time.NewTimer(3 * time.Second)
		select {
		case <-t.C:
			_ = edsstr2.CloseSend()
		case <-done:
			if !t.Stop() {
				<-t.C
			}
		}
	}()

	m, err := edsstr2.Recv()
	if err != nil {
		t.Fatal("Recv failed", err)
	}
	t.Log("Received ", m)
}

