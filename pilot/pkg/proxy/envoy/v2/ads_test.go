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
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

// Regression for envoy restart and overlapping connections
func TestAdsReconnectWithNonce(t *testing.T) {
	_ = initLocalPilotTestEnv(t)
	edsstr, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	err = sendEDSReq([]string{"outbound|1080||service3.default.svc.cluster.local"}, sidecarId(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := adsReceive(edsstr, 5*time.Second)

	// closes old process
	_ = edsstr.CloseSend()

	edsstr, err = connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer edsstr.CloseSend()

	err = sendEDSReqReconnect([]string{"service3.default.svc.cluster.local|http"}, edsstr, res)
	if err != nil {
		t.Fatal(err)
	}
	err = sendEDSReq([]string{"outbound|1080||service3.default.svc.cluster.local"}, sidecarId(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, _ = adsReceive(edsstr, 5*time.Second)
	_ = edsstr.CloseSend()

	t.Log("Received ", res)
}

// Regression for envoy restart and overlapping connections
func TestAdsReconnect(t *testing.T) {
	s := initLocalPilotTestEnv(t)
	edsstr, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	err = sendCDSReq(sidecarId(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = adsReceive(edsstr, 5*time.Second)

	// envoy restarts and reconnects
	edsstr2, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer edsstr2.CloseSend()
	err = sendCDSReq(sidecarId(app3Ip, "app3"), edsstr2)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = adsReceive(edsstr2, 5*time.Second)

	// closes old process
	_ = edsstr.CloseSend()

	time.Sleep(1 * time.Second)

	// event happens
	v2.AdsPushAll(s.EnvoyXdsServer)
	// will trigger recompute and push (we may need to make a change once diff is implemented

	m, err := adsReceive(edsstr2, 3*time.Second)
	if err != nil {
		t.Fatal("Recv failed", err)
	}
	t.Log("Received ", m)
}

func TestTLS(t *testing.T) {
	initLocalPilotTestEnv(t)
	edsstr, err := connectADSS(util.MockPilotSecureAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer edsstr.CloseSend()
	err = sendCDSReq(sidecarId(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adsReceive(edsstr, 3*time.Second)
	if err != nil {
		t.Error("Failed to receive with TLS connection ", err)
	}
}

func TestAdsClusterUpdate(t *testing.T) {
	server := initLocalPilotTestEnv(t)
	edsstr, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}

	var sendEDSReqAndVerify = func(clusterName string) {
		err = sendEDSReq([]string{clusterName}, sidecarId("1.1.1.1", "app3"), edsstr)
		if err != nil {
			t.Fatal(err)
		}
		res, err := adsReceive(edsstr, 5*time.Second)
		if err != nil {
			t.Fatal("Recv failed", err)
		}

		if res.TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
			t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res.TypeUrl)
		}
		if res.Resources[0].TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
			t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res.Resources[0].TypeUrl)
		}

		cla, err := getLoadAssignment(res)
		if err != nil {
			t.Fatal("Invalid EDS response ", err)
		}
		if cla.ClusterName != clusterName {
			t.Error(fmt.Sprintf("Expecting %s got ", clusterName), cla.ClusterName)
		}
	}

	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.2.0.1", 1080)

	cluster1 := "outbound|80||adsupdate.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster1)

	// register a second endpoint
	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate2.default.svc.cluster.local",
		"http-status", 2080, "10.2.0.2", 1081)

	cluster2 := "outbound|80||adsupdate2.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster2)
}

func TestAdsUpdate(t *testing.T) {
	server := initLocalPilotTestEnv(t)
	edsstr, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}

	// Old style cluster.
	// TODO: convert tests (except eds) to new style.
	server.EnvoyXdsServer.MemRegistry.AddService("adsupdate.default.svc.cluster.local", &model.Service{
		Hostname: "adsupdate.default.svc.cluster.local",
		Address:  "10.11.0.1",
		Ports:    testPorts(0),
	})
	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.2.0.1", 1080)

	err = sendEDSReq([]string{"outbound|2080||adsupdate.default.svc.cluster.local"}, sidecarId("1.1.1.1", "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}

	res1, err := adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal("Recv failed", err)
	}

	if res1.TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res1.Resources[0].TypeUrl)
	}
	cla, err := getLoadAssignment(res1)
	if err != nil {
		t.Fatal("Invalid EDS response ", err)
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
	if "10.2.0.1" != lbe[0].Endpoint.Address.GetSocketAddress().Address {
		t.Error("Expecting 10.2.0.1 got ", lbe[0].Endpoint.Address.GetSocketAddress().Address)
	}
	strResponse, _ := model.ToJSONWithIndent(res1, " ")
	_ = ioutil.WriteFile(env.IstioOut+"/edsv2_sidecar.json", []byte(strResponse), 0644)

	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.1.7.1", 1080)

	// will trigger recompute and push for all clients - including some that may be closing
	// This reproduced the 'push on closed connection' bug.
	v2.AdsPushAll(server.EnvoyXdsServer)

	res1, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}
	strResponse, _ = model.ToJSONWithIndent(res1, " ")
	_ = ioutil.WriteFile(env.IstioOut+"/edsv2_update.json", []byte(strResponse), 0644)
	_ = edsstr.CloseSend()
}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
func TestAdsMultiple(t *testing.T) {
	server := initLocalPilotTestEnv(t)
	errChan := make(chan error, 100)

	wg := &sync.WaitGroup{}
	wgConnect := &sync.WaitGroup{}

	n := 10
	nPushes := 10

	wg.Add(n)
	wgConnect.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			edsstr, err := connectADS(util.MockPilotGrpcAddr)
			if err != nil {
				errChan <- err
			}

			err = sendEDSReq([]string{"outbound|1080||service3.default.svc.cluster.local"}, sidecarId(testIp(uint32(0x0a200000+i)), "app3"), edsstr)
			if err != nil {
				errChan <- err
			}

			res1, err := adsReceive(edsstr, 5*time.Second)
			if err != nil {
				errChan <- err
			}
			wgConnect.Done()

			cla, err := getLoadAssignment(res1)
			if err != nil {
				errChan <- err
			}

			ep := cla.Endpoints
			if len(ep) == 0 {
				t.Fatal("No endpoints")
				errChan <- fmt.Errorf("No endpoints received")
			}
			lbe := ep[0].LbEndpoints
			if len(lbe) == 0 {
				errChan <- fmt.Errorf("No lb endpoints received")
			}

			for j := 0; j < nPushes; j++ {
				_, err = adsReceive(edsstr, 5*time.Second)
				if err != nil {
					errChan <- fmt.Errorf("Receive 2 failed: %s", err)
				}
			}
			_ = edsstr.CloseSend()
			wg.Done()
		}()
	}
	ok := waitTimeout(wgConnect, 10*time.Second)
	if !ok {
		t.Fatal("Failed to connect")
	}

	// will trigger recompute and push for all clients - including some that may be closing
	// This reproduced the 'push on closed connection' bug.
	for j := 0; j < nPushes; j++ {
		_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("service3.default.svc.cluster.local",
			"http-main", 2080, "10.1.7.1", 1080)
		v2.AdsPushAll(server.EnvoyXdsServer)
		log.Println("Push done ", j)
	}

	ok = waitTimeout(wg, 20*time.Second)
	if !ok {
		t.Fatal("Failed to receive all responses")
	}

	close(errChan)

	for e := range errChan {
		t.Fatal(e)
	}
}
