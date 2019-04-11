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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"

	"istio.io/istio/pkg/test/env"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/tests/util"
)

// The connect and reconnect tests are removed - ADS already has coverage, and the
// StreamEndpoints is not used in 1.0+

const (
	asdcLocality  = "region1/zone1/subzone1"
	asdc2Locality = "region2/zone2/subzone2"
)

func TestEds(t *testing.T) {
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	// will be checked in the direct request test
	addUdsEndpoint(server)

	// enable locality load balancing and add relevant endpoints in order to test
	os.Setenv("PILOT_ENABLE_LOCALITY_LOAD_BALANCING", "ON")
	addLocalityEndpoints(server)

	// Add the test ads clients to list of service instances in order to test the context dependent locality coloring.
	addTestClientEndpoints(server)

	adsc := adsConnectAndWait(t, 0x0a0a0a0a)
	defer adsc.Close()
	adsc2 := adsConnectAndWait(t, 0x0a0a0a0b)
	defer adsc2.Close()

	t.Run("TCPEndpoints", func(t *testing.T) {
		testTCPEndpoints("127.0.0.1", adsc, t)
		testEdsz(t)
	})
	t.Run("LocalityPrioritizedEndpoints", func(t *testing.T) {
		testLocalityPrioritizedEndpoints(adsc, adsc2, t)
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
	// Test using 0.8 request, without per/route mixer. Typically this is
	// 30% faster than 1.0 config style. Keeping the test to track fixes and
	// verify we fix the regression.
	t.Run("MultipleRequest08", func(t *testing.T) {
		// TODO: bump back up to 50 - regression in msater
		multipleRequest(server, false, 20, 5, 20*time.Second,
			map[string]string{}, t)
	})
	t.Run("MultipleRequest", func(t *testing.T) {
		multipleRequest(server, false, 20, 5, 20*time.Second, nil, t)
	})
	// 5 pushes for 100 clients, using EDS incremental only.
	t.Run("MultipleRequestIncremental", func(t *testing.T) {
		multipleRequest(server, true, 50, 5, 20*time.Second, nil, t)
	})
	t.Run("CDSSave", func(t *testing.T) {
		// Moved from cds_test, using new client
		if len(adsc.Clusters) == 0 {
			t.Error("No clusters in ADS response")
		}
		strResponse, _ := json.MarshalIndent(adsc.Clusters, " ", " ")
		_ = ioutil.WriteFile(env.IstioOut+"/cdsv2_sidecar.json", strResponse, 0644)

	})
}

func TestEDSOverlapping(t *testing.T) {

	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	// add endpoints with multiple ports with the same port number
	addOverlappingEndpoints(server)

	adsc := adsConnectAndWait(t, 0x0a0a0a0a)
	defer adsc.Close()
	testOverlappingPorts(server, adsc, t)
}

func adsConnectAndWait(t *testing.T, ip int) *adsc.ADSC {
	adsc, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		IP: testIP(uint32(ip)),
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

func addTestClientEndpoints(server *bootstrap.Server) {
	server.EnvoyXdsServer.MemRegistry.AddService("test-1.default", &model.Service{
		Hostname: "test-1.default",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("test-1.default", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: fmt.Sprintf("10.10.10.10"),
			Port:    80,
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
			Locality: asdcLocality,
		},
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("test-1.default", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: fmt.Sprintf("10.10.10.11"),
			Port:    80,
			ServicePort: &model.Port{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
			Locality: asdc2Locality,
		},
	})
	server.EnvoyXdsServer.Push(true, nil)
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testTCPEndpoints(expected string, adsc *adsc.ADSC, t *testing.T) {
	testEndpoints(expected, "outbound|8080||eds.test.svc.cluster.local", adsc, t)
}

// Verify server sends the endpoint. This check for a single endpoint with the given
// address.
func testEndpoints(expected string, cluster string, adsc *adsc.ADSC, t *testing.T) {
	lbe, f := adsc.EDS[cluster]
	if !f || len(lbe.Endpoints) == 0 {
		t.Fatalf("No lb endpoints for %v, %v", cluster, adsc.EndpointsJSON())
	}
	var found []string
	for _, lbe := range lbe.Endpoints {
		for _, e := range lbe.LbEndpoints {
			addr := e.GetEndpoint().Address.GetSocketAddress().Address
			found = append(found, addr)
			if expected == addr {
				return
			}
		}
	}
	t.Errorf("Expecting %s got %v", expected, found)
	if len(found) != 1 {
		t.Error("Expecting 1, got ", len(found))
	}
}

func testLocalityPrioritizedEndpoints(adsc *adsc.ADSC, adsc2 *adsc.ADSC, t *testing.T) {
	verifyLocalityPriorities(asdcLocality, adsc.EDS["outbound|80||locality.cluster.local"].GetEndpoints(), t)
	verifyLocalityPriorities(asdc2Locality, adsc2.EDS["outbound|80||locality.cluster.local"].GetEndpoints(), t)
}

// Tests that Services with multiple ports sharing the same port number are properly sent endpoints.
// Real world use case for this is kube-dns, which uses port 53 for TCP and UDP.
func testOverlappingPorts(server *bootstrap.Server, adsc *adsc.ADSC, t *testing.T) {
	// Test initial state
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)

	server.EnvoyXdsServer.Push(false, map[string]struct{}{
		"overlapping.cluster.local": {},
	})
	_, _ = adsc.Wait("", 5*time.Second)

	// After the incremental push, we should still see the endpoint
	testEndpoints("10.0.0.53", "outbound|53||overlapping.cluster.local", adsc, t)
}

func verifyLocalityPriorities(proxyLocality string, eps []endpoint.LocalityLbEndpoints, t *testing.T) {
	items := strings.SplitN(proxyLocality, "/", 3)
	region, zone, subzone := items[0], items[1], items[2]
	for _, ep := range eps {
		if ep.GetLocality().Region == region {
			if ep.GetLocality().Zone == zone {
				if ep.GetLocality().SubZone == subzone {
					if ep.GetPriority() != 0 {
						t.Errorf("expected endpoint pool from same locality to have priority of 0, got %v", ep.GetPriority())
					}
				} else if ep.GetPriority() != 1 {
					t.Errorf("expected endpoint pool from a different subzone to have priority of 1, got %v", ep.GetPriority())
				}
			} else {
				if ep.GetPriority() != 2 {
					t.Errorf("expected endpoint pool from a different zone to have priority of 2, got %v", ep.GetPriority())
				}
			}
		} else {
			if ep.GetPriority() != 3 {
				t.Errorf("expected endpoint pool from a different region to have priority of 3, got %v", ep.GetPriority())
			}
		}
	}
}

// Verify server sends UDS endpoints
func testUdsEndpoints(_ *bootstrap.Server, adsc *adsc.ADSC, t *testing.T) {
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
				Name:     "http-main",
				Port:     8080,
				Protocol: model.ProtocolHTTP,
			},
			Locality: "az",
		},
		ServiceAccount: "hello-sa",
		Labels:         map[string]string{"version": "v1"},
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
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))

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
		newEndpointWithAccount("127.0.0.3", "account2", "v1"))
	upd, err = adsc.Wait("", 5*time.Second)
	if upd != "cds" || err != nil {
		t.Fatal("Expecting full push after service account update", err, upd)
	}
	adsc.Wait("rds", 5*time.Second)
	// LDS also asks for an update
	adsc.Wait("rds", 5*time.Second)
	testTCPEndpoints("127.0.0.3", adsc, t)

	// Update the endpoint again, no SA change - expect incremental
	server.EnvoyXdsServer.MemRegistry.SetEndpoints(edsIncSvc,
		newEndpointWithAccount("127.0.0.4", "account2", "v1"))

	upd, err = adsc.Wait("", 5*time.Second)
	if upd != "eds" || err != nil {
		t.Fatal("Expecting inc push ", err, upd)
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
func multipleRequest(server *bootstrap.Server, inc bool, nclients,
	nPushes int, to time.Duration, _ map[string]string, t *testing.T) {
	wgConnect := &sync.WaitGroup{}
	wg := &sync.WaitGroup{}
	errChan := make(chan error, nclients)

	// Bad client - will not read any response. This triggers Write to block, which should
	// be detected
	// This is not using adsc, which consumes the events automatically.
	ads, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	err = sendCDSReq(sidecarID(testIP(0x0a120001), "app3"), ads)
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	n := nclients
	wg.Add(n)
	wgConnect.Add(n)
	rcvPush := int32(0)
	rcvClients := int32(0)
	for i := 0; i < n; i++ {
		current := i
		go func(id int) {
			defer wg.Done()
			// Connect and get initial response
			adsc, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
				IP: testIP(uint32(0x0a100000 + id)),
			})
			if err != nil {
				errChan <- errors.New("failed to connect" + err.Error())
				wgConnect.Done()
				return
			}
			defer adsc.Close()
			adsc.Watch()
			_, err = adsc.Wait("rds", 5*time.Second)
			if err != nil {
				errChan <- errors.New("failed to get initial rds" + err.Error())
				wgConnect.Done()
				return
			}

			if len(adsc.EDS) == 0 {
				errChan <- errors.New("no endpoints")
				wgConnect.Done()
				return
			}

			wgConnect.Done()

			// Check we received all pushes
			log.Println("Waiting for pushes ", id)
			for j := 0; j < nPushes; j++ {
				// The time must be larger than write timeout: if we run all tests
				// and some are leaving uncleaned state the push will be slower.
				_, err := adsc.Wait("eds", 15*time.Second)
				atomic.AddInt32(&rcvPush, 1)
				if err != nil {
					log.Println("Recv failed", err, id, j)
					errChan <- fmt.Errorf("failed to receive a response in 15 s %v %v %v",
						err, id, j)
					return
				}
			}
			log.Println("Received all pushes ", id)
			atomic.AddInt32(&rcvClients, 1)

			adsc.Close()
		}(current)
	}
	ok := waitTimeout(wgConnect, to)
	if !ok {
		t.Fatal("Failed to connect")
	}
	log.Println("Done connecting")

	// All clients are connected - this can start pushing changes.
	for j := 0; j < nPushes; j++ {
		if inc {
			// This will be throttled - we want to trigger a single push
			//server.EnvoyXdsServer.MemRegistry.SetEndpoints(edsIncSvc,
			//	newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
			updates := map[string]struct{}{
				edsIncSvc: {},
			}
			server.EnvoyXdsServer.AdsPushAll(strconv.Itoa(j), server.EnvoyXdsServer.Env.PushContext, false, updates)
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

	close(errChan)

	// moved from ads_test, which had a duplicated test.
	for e := range errChan {
		t.Error(e)
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
			Locality: "localhost",
		},
		Labels: map[string]string{"socket": "unix"},
	})

	server.EnvoyXdsServer.Push(true, nil)
}

func addLocalityEndpoints(server *bootstrap.Server) {
	server.EnvoyXdsServer.MemRegistry.AddService("locality.cluster.local", &model.Service{
		Hostname: "locality.cluster.local",
		Ports: model.PortList{
			{
				Name:     "http",
				Port:     80,
				Protocol: model.ProtocolHTTP,
			},
		},
	})
	localities := []string{
		"region1/zone1/subzone1",
		"region1/zone1/subzone2",
		"region1/zone2/subzone1",
		"region2/zone1/subzone1",
		"region2/zone1/subzone2",
		"region2/zone2/subzone1",
		"region2/zone2/subzone2",
	}
	for i, locality := range localities {
		server.EnvoyXdsServer.MemRegistry.AddInstance("locality.cluster.local", &model.ServiceInstance{
			Endpoint: model.NetworkEndpoint{
				Address: fmt.Sprintf("10.0.0.%v", i),
				Port:    80,
				ServicePort: &model.Port{
					Name:     "http",
					Port:     80,
					Protocol: model.ProtocolHTTP,
				},
				Locality: locality,
			},
		})
	}
	server.EnvoyXdsServer.Push(true, nil)
}

func addOverlappingEndpoints(server *bootstrap.Server) {
	server.EnvoyXdsServer.MemRegistry.AddService("overlapping.cluster.local", &model.Service{
		Hostname: "overlapping.cluster.local",
		Ports: model.PortList{
			{
				Name:     "dns",
				Port:     53,
				Protocol: model.ProtocolUDP,
			},
			{
				Name:     "tcp-dns",
				Port:     53,
				Protocol: model.ProtocolTCP,
			},
		},
	})
	server.EnvoyXdsServer.MemRegistry.AddInstance("overlapping.cluster.local", &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: "10.0.0.53",
			Port:    53,
			ServicePort: &model.Port{
				Name:     "tcp-dns",
				Port:     53,
				Protocol: model.ProtocolTCP,
			},
		},
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
