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

package istioagent

import (
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
)

// TestXdsLeak is a regression test for https://github.com/istio/istio/issues/34097
func TestDeltaXdsLeak(t *testing.T) {
	proxy := setupXdsProxyWithDownstreamOptions(t, []grpc.ServerOption{grpc.StreamInterceptor(xdstest.SlowServerInterceptor(time.Second, time.Second))})
	f := xdstest.NewMockServer(t)
	setDialOptions(proxy, f.Listener)
	proxy.istiodDialOptions = append(proxy.istiodDialOptions, grpc.WithStreamInterceptor(xdstest.SlowClientInterceptor(0, time.Second*10)))
	conn := setupDownstreamConnection(t, proxy)
	downstream := deltaStream(t, conn)
	sendDeltaDownstreamWithoutResponse(t, downstream)
	for i := 0; i < 15; i++ {
		// Send a bunch of responses from Istiod. These should not block, even though there are more sends than responseChan can hold
		f.SendDeltaResponse(&discovery.DeltaDiscoveryResponse{TypeUrl: v3.ClusterType})
	}
	// Exit test, closing the connections. We should not have any goroutine leaks (checked by leak.CheckMain)
}

// sendDownstreamWithoutResponse sends a response without waiting for a response
func sendDeltaDownstreamWithoutResponse(t *testing.T, downstream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient) {
	t.Helper()
	err := downstream.Send(&discovery.DeltaDiscoveryRequest{
		TypeUrl: v3.ClusterType,
		Node: &core.Node{
			Id: "sidecar~0.0.0.0~debug~cluster.local",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Validates basic xds proxy flow by proxying one CDS requests end to end.
func TestDeltaXdsProxyBasicFlow(t *testing.T) {
	proxy := setupXdsProxy(t)
	f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	setDialOptions(proxy, f.BufListener)
	conn := setupDownstreamConnection(t, proxy)
	downstream := deltaStream(t, conn)
	sendDeltaDownstreamWithNode(t, downstream, model.NodeMetadata{
		Namespace:   "default",
		InstanceIPs: []string{"1.1.1.1"},
	})
}

func deltaStream(t *testing.T, conn *grpc.ClientConn) discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient {
	t.Helper()
	adsClient := discovery.NewAggregatedDiscoveryServiceClient(conn)
	downstream, err := adsClient.DeltaAggregatedResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return downstream
}

func sendDeltaDownstreamWithNode(t *testing.T, downstream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient, meta model.NodeMetadata) {
	t.Helper()
	node := &core.Node{
		Id:       "sidecar~1.1.1.1~debug~cluster.local",
		Metadata: meta.ToStruct(),
	}
	err := downstream.Send(&discovery.DeltaDiscoveryRequest{TypeUrl: v3.ClusterType, Node: node})
	if err != nil {
		t.Fatal(err)
	}
	res, err := downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.TypeUrl != v3.ClusterType {
		t.Fatalf("Expected to get cluster response but got %v", res)
	}
	err = downstream.Send(&discovery.DeltaDiscoveryRequest{TypeUrl: v3.ListenerType, Node: node})
	if err != nil {
		t.Fatal(err)
	}
	res, err = downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.TypeUrl != v3.ListenerType {
		t.Fatalf("Expected to get listener response but got %v", res)
	}
}
