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
	"io/ioutil"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "net/http/pprof"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/tests/util"
)

// The connect and reconnect tests are removed - ADS already has coverage, and the
// StreamEndpoints is not used in 1.0+

func TestEds(t *testing.T) {
	initLocalPilotTestEnv(t)
	server := util.MockTestServer

	// will be checked in the direct request test
	addUdsEndpoint(server)

	adsc := adsConnectAndWait(t, 0x0a0a0a0a)
	defer adsc.Close()

	t.Run("TCPEndpoints", func(t *testing.T) {
		testTCPEndpoints("127.0.0.1", adsc, t)
	})
	t.Run("UDSEndpoints", func(t *testing.T) {
		testUdsEndpoints(server, adsc, t)
	})
	t.Run("PushIncremental", func(t *testing.T) {
		edsUpdateInc(server, adsc, t)
	})
	t.Run("Push", func(t *testing.T) {
		edsUpdates(server, adsc, t)
	})
	t.Run("MultipleRequest", func(t *testing.T) {
		multipleRequest(server, false, 100, 10, 20*time.Second, t)
	})
	t.Run("MultipleRequestIncremental", func(t *testing.T) {
		multipleRequest(server, true, 100, 10, 20*time.Second, t)
	})
	t.Run("edsz", func(t *testing.T) {
		testEdsz(t)
	})

}

func adsConnectAndWait(t *testing.T, ip int) *adsc.ADSC {
	adsc, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		IP: testIp(uint32(ip)),
	})
	if err != nil {
		t.Fatal("Error connecting ", err)
	}
	adsc.Watch()
	_, err = adsc.Wait("rds", 10*time.Second)
	if err != nil {
		t.Fatal("Error getting initial config ", err)
	}

	if len(adsc.EDS) == 0 {
		t.Fatal("No endpoints")
	}
	return adsc
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testTCPEndpoints(expected string, adsc *adsc.ADSC, t *testing.T) {
	lbe, f := adsc.EDS["outbound|8080||eds.test.svc.cluster.local"]
	if !f || len(lbe.Endpoints) == 0 {
		t.Fatal("No lb endpoints ", adsc.EndpointsJSON())
	}
	total := 0
	for _, lbe := range lbe.Endpoints {
		for _, e := range lbe.LbEndpoints {
			total++
			if expected == e.Endpoint.Address.GetSocketAddress().Address {
				return
			}
		}
	}
	t.Errorf("Expecting %s got %v", expected, lbe.Endpoints[0].LbEndpoints)
	if total != 1 {
		t.Error("Expecting 1, got ", total)
	}
}

// Verify server sends UDS endpoints
func testUdsEndpoints(server *bootstrap.Server, adsc *adsc.ADSC, t *testing.T) {
	// Check the UDS endpoint ( used to be separate test - but using old unused GRPC method)
	// The new test also verifies CDS is pusing the UDS cluster, since adsc.EDS is
	// populated using CDS response
	lbe, f := adsc.EDS["outbound|0||localuds.cluster.local"]
	if !f || len(lbe.Endpoints) == 0 {
		t.Error("No UDS lb endpoints")
	} else {
		ep0 := lbe.Endpoints[0]
		if len(ep0.LbEndpoints) != 1 {
			t.Fatalf("expected 1 LB endpoint but got %d", len(ep0.LbEndpoints))
		}
		lbep := ep0.LbEndpoints[0]
		path := lbep.GetEndpoint().GetAddress().GetPipe().GetPath()
		if path != udsPath {
			t.Fatalf("expected Pipe to %s, got %s", udsPath, path)
		}
	}
}

// Update
func edsUpdates(server *bootstrap.Server, adsc *adsc.ADSC, t *testing.T) {

	// Old style (non-incremental)
	server.EnvoyXdsServer.MemRegistry.AddInstance(edsIncSvc, &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: "127.0.0.3",
			Port:    int(testEnv.Ports().BackendPort),
			ServicePort: &model.Port{
				Name:     "http",
				Port:     8080,
				Protocol: model.ProtocolHTTP,
			},
		},
		ServiceAccount:   "hello-sa",
		Labels:           map[string]string{"version": "v1"},
		AvailabilityZone: "az",
	})

	v2.AdsPushAll(server.EnvoyXdsServer)
	// will trigger recompute and push

	_, err := adsc.Wait("eds", 5*time.Second)
	if err != nil {
		t.Fatal("EDS push failed", err)
	}
	testTCPEndpoints("127.0.0.3", adsc, t)
}

// This test must be run in isolation, can't be parallelized with any other v2 test.
// It makes different kind of updates, and checks that incremental or full push happens.
// In particular:
// - just endpoint changes -> incremental
// - service account changes -> full ( in future: CDS only )
// - label changes -> full
func edsUpdateInc(server *bootstrap.Server, adsc *adsc.ADSC, t *testing.T) {

	// TODO: set endpoints for a different cluster (new shard)

	// Verify initial state
	testTCPEndpoints("127.0.0.1", adsc, t)

	adsc.WaitClear() // make sure there are no pending pushes.

	// Equivalent with the event generated by K8S watching the Service.
	// Will trigger a push.
	server.EnvoyXdsServer.MemRegistry.SetEndpoints(edsIncSvc,
		endpoints("127.0.0.2", "hello-sa", "v1"))

	upd, err := adsc.Wait("", 5*time.Second)
	if err != nil {
		t.Fatal("Incremental push failed", err)
	}
	if upd != "eds" {
		t.Error("Expecting EDS only update, got", upd)
		_, err = adsc.Wait("lds", 5*time.Second)
		if err != nil {
			t.Fatal("Incremental push failed", err)
		}
	}

	testTCPEndpoints("127.0.0.2", adsc, t)

	// Update the endpoint with different SA - expect full
	server.EnvoyXdsServer.MemRegistry.SetEndpoints(edsIncSvc,
		endpoints("127.0.0.3", "account2", "v1"))
	upd, err = adsc.Wait("", 5*time.Second)
	if upd != "cds" || err != nil {
		t.Fatal("Expecting full push after service account update", err, upd)
	}
	adsc.Wait("lds", 5*time.Second)
	testTCPEndpoints("127.0.0.3", adsc, t)

	// Update the endpoint again, no SA change - expect incremental
	server.EnvoyXdsServer.MemRegistry.SetEndpoints(edsIncSvc,
		endpoints("127.0.0.4", "account2", "v1"))

	upd, err = adsc.Wait("", 5*time.Second)
	if upd != "eds" || err != nil {
		t.Fatal("Expecting full push after service account update", err, upd)
	}
	testTCPEndpoints("127.0.0.4", adsc, t)

	// Update the endpoint with different label - expect full
	server.EnvoyXdsServer.WorkloadUpdate("127.0.0.4", map[string]string{"version": "v2"}, nil)

	upd, err = adsc.Wait("", 5*time.Second)
	if upd != "cds" || err != nil {
		t.Fatal("Expecting full push after label update", err, upd)
	}
	adsc.Wait("lds", 5*time.Second)
	testTCPEndpoints("127.0.0.4", adsc, t)

	// Update the endpoint again, no label change - expect incremental

}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
// This test includes a 'bad client' regression test, which fails to read on the
// stream.
func multipleRequest(server *bootstrap.Server, inc bool, nclients, nPushes int, to time.Duration, t *testing.T) {
	wgConnect := &sync.WaitGroup{}
	wg := &sync.WaitGroup{}

	// Bad client - will not read any response. This triggers Write to block, which should
	// be detected
	// This is not using adsc, which consumes the events automatically.
	ads, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}

	err = sendCDSReq(sidecarId(testIp(0x0a120001), "app3"), ads)
	if err != nil {
		t.Fatal(err)
	}

	n := nclients
	wg.Add(n)
	wgConnect.Add(n)
	rcvPush := int32(0)
	rcvClients := int32(0)
	for i := 0; i < n; i++ {
		current := i
		go func(id int) {
			// Connect and get initial response
			adsc, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
				IP: testIp(uint32(0x0a100000 + id)),
			})
			if err != nil {
				t.Fatal("Error connecting ", err)
			}
			defer adsc.Close()
			adsc.Watch()
			_, err = adsc.Wait("rds", 5*time.Second)
			if err != nil {
				t.Fatal("Error getting initial config ", err)
			}

			if len(adsc.EDS) == 0 {
				t.Fatal("No endpoints")
			}

			defer adsc.Close()
			wgConnect.Done()
			defer wg.Done()

			// Check we received all pushes
			log.Println("Waiting for pushes ", id)
			for j := 0; j < nPushes; j++ {
				// The time must be larger than write timeout: if we run all tests
				// and some are leaving uncleaned state the push will be slower.
				_, err := adsc.Wait("eds", 15*time.Second)
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
	ok := waitTimeout(wgConnect, to)
	if !ok {
		t.Fatal("Failed to connect")
	}
	log.Println("Done connecting")
	for j := 0; j < nPushes; j++ {
		if inc {
			server.EnvoyXdsServer.Env.EDSUpdates[edsIncSvc] =
				&model.ServiceShards{}

			server.EnvoyXdsServer.AdsPushAll("v1",
				server.EnvoyXdsServer.Env.PushContext,
				false, server.EnvoyXdsServer.Env.EDSUpdates)
		} else {
			v2.AdsPushAll(server.EnvoyXdsServer)
		}
		log.Println("Push done ", j)
	}

	ok = waitTimeout(wg, to)
	if !ok {
		t.Errorf("Failed to receive all responses %d %d", rcvClients, rcvPush)
		buf := make([]byte, 1<<16)
		runtime.Stack(buf, true)
		fmt.Printf("%s", buf)
	}
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

const udsPath = "/var/run/test/socket"

func addUdsEndpoint(server *bootstrap.Server) {
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

	server.EnvoyXdsServer.Push(true, nil)
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

	if !strings.Contains(statusStr, "\"outbound|8080||eds.test.svc.cluster.local\"") {
		t.Fatal("Mock eds service not found ", statusStr)
	}
}
