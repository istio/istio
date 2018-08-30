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
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

func connect(t *testing.T) xdsapi.EndpointDiscoveryService_StreamEndpointsClient {
	req := &xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: sidecarId(app3Ip, "app3"),
		},
		ResourceNames: []string{"outbound|80||hello.default.svc.cluster.local"},
	}
	return connectWithRequest(req, t)
}

func reconnect(res *xdsapi.DiscoveryResponse, t *testing.T) xdsapi.EndpointDiscoveryService_StreamEndpointsClient {
	req := &xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: sidecarId(app3Ip, "app3"),
		},
		VersionInfo:   res.VersionInfo,
		ResponseNonce: res.Nonce,
		ResourceNames: []string{"outbound|80||hello.default.svc.cluster.local"},
	}
	return connectWithRequest(req, t)
}

func connectWithRequest(r *xdsapi.DiscoveryRequest, t *testing.T) xdsapi.EndpointDiscoveryService_StreamEndpointsClient {
	conn, err := grpc.Dial(util.MockPilotGrpcAddr, grpc.WithInsecure())
	if err != nil {
		t.Fatal("Connection failed", err)
	}

	xds := xdsapi.NewEndpointDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamEndpoints(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}
	err = edsstr.Send(r)
	if err != nil {
		t.Fatal("Send failed", err)
	}
	return edsstr
}

// Regression for envoy restart and overlapping connections
func TestReconnectWithNonce(t *testing.T) {
	_ = initLocalPilotTestEnv(t)
	edsstr := connect(t)
	res, _ := edsstr.Recv()

	// closes old process
	_ = edsstr.CloseSend()

	edsstr = reconnect(res, t)
	res, _ = edsstr.Recv()
	_ = edsstr.CloseSend()

	t.Log("Received ", res)
}

// Regression for envoy restart and overlapping connections
func TestReconnect(t *testing.T) {
	s := initLocalPilotTestEnv(t)
	edsstr := connect(t)
	_, _ = edsstr.Recv()

	// envoy restarts and reconnects
	edsstr2 := connect(t)
	_, _ = edsstr2.Recv()

	// closes old process
	_ = edsstr.CloseSend()

	time.Sleep(1 * time.Second)

	// event happens
	v2.AdsPushAll(s.EnvoyXdsServer)

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

// Make a direct EDS grpc request to pilot, verify the result is as expected.
func directRequest(server *bootstrap.Server, t *testing.T) {
	edsstr, cla := connectAndSend(1, t)
	defer edsstr.CloseSend()
	// TODO: validate VersionInfo and nonce once we settle on a scheme

	ep := cla.Endpoints
	if len(ep) == 0 {
		t.Fatal("No endpoints")
	}
	lbe := ep[0].LbEndpoints
	if len(lbe) == 0 {
		t.Fatal("No lb endpoints")
	}
	if "127.0.0.1" != lbe[0].Endpoint.Address.GetSocketAddress().Address {
		t.Error("Expecting 127.0.0.1 got ", lbe[0].Endpoint.Address.GetSocketAddress().Address)
	}

	server.EnvoyXdsServer.MemRegistry.AddInstance("hello.default.svc.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: "127.0.0.2",
			Port:    int(testEnv.Ports().BackendPort),
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
		Labels:           map[string]string{"version": "v1"},
		AvailabilityZone: "az",
	})

	v2.AdsPushAll(server.EnvoyXdsServer)
	// will trigger recompute and push

	// This should happen in 15 seconds, for the periodic refresh
	// TODO: verify push works
	_, err := edsstr.Recv()
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}

	// Need to run the debug test before we close - close will remove the cluster since
	// nobody is watching.
	testEdsz(t)
}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
// id should be unique to avoid interference between tests.
func connectAndSend(id uint32, t *testing.T) (xdsapi.EndpointDiscoveryService_StreamEndpointsClient,
	*xdsapi.ClusterLoadAssignment) {

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
			Id: sidecarId(testIp(uint32(0x0a100000+id)), "app3"),
		},
		ResourceNames: []string{"outbound|80||hello.default.svc.cluster.local"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}

	res1, err := edsstr.Recv()
	if err != nil {
		t.Fatal("Recv failed", err)
	}
	cla, err := getLoadAssignment(res1)
	if err != nil {
		t.Fatal("Invalid EDS ", err)
	}
	return edsstr, cla
}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
func multipleRequest(server *bootstrap.Server, t *testing.T) {
	wgConnect := &sync.WaitGroup{}
	wg := &sync.WaitGroup{}

	// Bad client - will not read any response. This triggers Write to block, which should
	// be detected
	ads, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}

	err = sendCDSReq(sidecarId(testIp(0x0a120001), "app3"), ads)
	if err != nil {
		t.Fatal(err)
	}

	// 1000 clients * 100 pushes = ~4 sec
	n := 10 // clients
	nPushes := 10
	wg.Add(n)
	wgConnect.Add(n)
	rcvPush := int32(0)
	rcvClients := int32(0)
	for i := 0; i < n; i++ {
		current := i
		go func(id int) {
			// Connect and get initial response
			edsstr, _ := connectAndSend(uint32(id+2), t)
			defer edsstr.CloseSend()
			wgConnect.Done()
			defer wg.Done()
			// Check we received all pushes
			log.Println("Waiting for pushes ", id)
			for j := 0; j < nPushes; j++ {
				// The time must be larger than write timeout: if we run all tests
				// and some are leaving uncleaned state the push will be slower.
				_, err := edsReceive(edsstr, 15*time.Second)
				atomic.AddInt32(&rcvPush, 1)
				if err != nil {
					log.Println("Recv failed", err, id, j)
					t.Error("Recv failed ", err, id, j)
					return
				}
			}
			log.Println("Received all pushes ", id)
			atomic.AddInt32(&rcvClients, 1)
		}(current)
	}
	ok := waitTimeout(wgConnect, 10*time.Second)
	if !ok {
		t.Fatal("Failed to connect")
	}
	log.Println("Done connecting")
	for j := 0; j < nPushes; j++ {
		v2.AdsPushAll(server.EnvoyXdsServer)
		log.Println("Push done ", j)
	}

	ok = waitTimeout(wg, 30*time.Second)
	if !ok {
		t.Errorf("Failed to receive all responses %d %d", rcvClients, rcvPush)
		buf := make([]byte, 1<<16)
		runtime.Stack(buf, true)
		fmt.Printf("%s", buf)
	}
}

func edsReceive(eds xdsapi.EndpointDiscoveryService_StreamEndpointsClient, to time.Duration) (*xdsapi.DiscoveryResponse, error) {
	done := make(chan int, 1)
	t := time.NewTimer(to)
	defer func() {
		done <- 1
	}()
	go func() {
		select {
		case <-t.C:
			_ = eds.CloseSend() // will result in adsRecv closing as well, interrupting the blocking recv
		case <-done:
			_ = t.Stop()
		}
	}()
	return eds.Recv()
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

func udsRequest(server *bootstrap.Server, t *testing.T) {
	udsPath := "/var/run/test/socket"
	server.EnvoyXdsServer.MemRegistry.AddService("localuds.cluster.local", &model.Service{
		Hostname: "localuds.cluster.local",
		Ports: model.PortList{
			{
				Name:     "grpc",
				Port:     0,
				Protocol: model.ProtocolGRPC,
			},
		},
		MeshExternal: true,
		Resolution:   model.ClientSideLB,
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("localuds.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Family:  model.AddressFamilyUnix,
			Address: udsPath,
			Port:    0,
			ServicePort: &model.Port{
				Name:     "grpc",
				Port:     0,
				Protocol: model.ProtocolGRPC,
			},
		},
		Labels:           map[string]string{"socket": "unix"},
		AvailabilityZone: "localhost",
	})

	req := &xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: sidecarId(app3Ip, "app3"),
		},
		ResourceNames: []string{"outbound|0||localuds.cluster.local"},
	}
	edsstr := connectWithRequest(req, t)

	cla := &xdsapi.ClusterLoadAssignment{}
	// Race condition in the server might cause it to send a Resource with no endpoints at first, so retry once more if
	// that happens.
	for i := 0; i < 2; i++ {
		res, err := edsstr.Recv()
		if err != nil {
			t.Fatal("Recv2 failed", err)
		}
		t.Log(res.String())
		if len(res.Resources) != 1 {
			t.Fatalf("expected 1 resources got %d", len(res.Resources))
		}
		err = cla.Unmarshal(res.Resources[0].Value)
		if err != nil {
			t.Fatal("Failed to parse proto ", err)
		}
		t.Log(cla.String())
		if len(cla.Endpoints) != 1 {
			if i == 1 {
				// Second attempt, fail test case
				t.Fatalf("expected 1 endpoint but got %d", len(cla.Endpoints))
			}
			log.Println(fmt.Sprintf("expected 1 endpoint but got %d, retrying", len(cla.Endpoints)))
			continue
		}
		break
	}
	ep0 := cla.Endpoints[0]
	if len(ep0.LbEndpoints) != 1 {
		t.Fatalf("expected 1 LB endpoint but got %d", len(ep0.LbEndpoints))
	}
	lbep := ep0.LbEndpoints[0]
	path := lbep.GetEndpoint().GetAddress().GetPipe().GetPath()
	if path != udsPath {
		t.Fatalf("expected Pipe to %s, got %s", udsPath, path)
	}
}

func TestEds(t *testing.T) {
	initLocalPilotTestEnv(t)
	server := util.EnsureTestServer()

	t.Run("DirectRequest", func(t *testing.T) {
		directRequest(server, t)
	})
	t.Run("MultipleRequest", func(t *testing.T) {
		multipleRequest(server, t)
	})
	t.Run("UDSRequest", func(t *testing.T) {
		udsRequest(server, t)
	})
}

// Verify the endpoint debug interface is installed and returns some string.
// TODO: parse response, check if data captured matches what we expect.
// TODO: use this in integration tests.
// TODO: refine the output
// TODO: dump the ServiceInstances as well
func testEdsz(t *testing.T) {
	edszURL := fmt.Sprintf("http://localhost:%d/debug/edsz", testEnv.Ports().PilotHTTPPort)
	res, err := http.Get(edszURL)
	if err != nil {
		t.Fatalf("Failed to fetch %s", edszURL)
	}
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("Failed to read /edsz")
	}
	statusStr := string(data)

	if !strings.Contains(statusStr, "\"outbound|80||hello.default.svc.cluster.local\"") {
		t.Fatal("Mock hello service not found ", statusStr)
	}
}
