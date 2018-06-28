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
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/tests/util"
)

// Regression for envoy restart and overlapping connections
func TestAdsReconnectWithNonce(t *testing.T) {
	_ = initLocalPilotTestEnv(t)
	edsstr := connectADS(t, util.MockPilotGrpcAddr)
	sendEDSReq(t, []string{"service3.default.svc.cluster.local|http"}, sidecarId(app3Ip, "app3"), edsstr)
	res, _ := adsReceive(edsstr, 5*time.Second)

	// closes old process
	_ = edsstr.CloseSend()

	edsstr = connectADS(t, util.MockPilotGrpcAddr)
	defer edsstr.CloseSend()
	sendEDSReqReconnect(t, []string{"service3.default.svc.cluster.local|http"}, edsstr, res)
	sendEDSReq(t, []string{"service3.default.svc.cluster.local|http"}, sidecarId(app3Ip, "app3"), edsstr)
	res, _ = adsReceive(edsstr, 5*time.Second)
	_ = edsstr.CloseSend()

	t.Log("Received ", res)
}

// Regression for envoy restart and overlapping connections
func TestAdsReconnect(t *testing.T) {
	initLocalPilotTestEnv(t)
	edsstr := connectADS(t, util.MockPilotGrpcAddr)
	sendCDSReq(t, sidecarId(app3Ip, "app3"), edsstr)
	_, _ = adsReceive(edsstr, 5*time.Second)

	// envoy restarts and reconnects
	edsstr2 := connectADS(t, util.MockPilotGrpcAddr)
	defer edsstr2.CloseSend()
	sendCDSReq(t, sidecarId(app3Ip, "app3"), edsstr2)
	_, _ = adsReceive(edsstr2, 5*time.Second)

	// closes old process
	_ = edsstr.CloseSend()

	time.Sleep(1 * time.Second)

	// event happens
	v2.PushAll()
	// will trigger recompute and push (we may need to make a change once diff is implemented

	m, err := adsReceive(edsstr2, 3*time.Second)
	if err != nil {
		t.Fatal("Recv failed", err)
	}
	t.Log("Received ", m)
}

func TestTLS(t *testing.T) {
	initLocalPilotTestEnv(t)
	edsstr := connectADSS(t, util.MockPilotSecureAddr)
	defer edsstr.CloseSend()
	sendCDSReq(t, sidecarId(app3Ip, "app3"), edsstr)
	_, err := adsReceive(edsstr, 3*time.Second)
	if err != nil {
		t.Error("Failed to receive with TLS connection ", err)
	}

	bootstrap.IstioCertDir = util.IstioSrc + "/tests/testdata/certs/default"
	c, err := bootstrap.Checkin(true, util.MockPilotSecureAddr, "cluster", sidecarId(app3Ip, "app3"),
		1*time.Second, 2)
	if err != nil {
		t.Fatal("Failed to checkin", err)
	}
	t.Log("AZ:", c.AvailabilityZone)
}

func TestAdsClusterUpdate(t *testing.T) {
	server := initLocalPilotTestEnv(t)
	edsstr := connectADS(t, util.MockPilotGrpcAddr)

	var sendEDSReqAndVerify = func(clusterName string) {
		sendEDSReq(t, []string{clusterName}, sidecarId("1.1.1.1", "app3"), edsstr)
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

	cluster1 := "adsupdate.default.svc.cluster.local|http-main"
	sendEDSReqAndVerify(cluster1)

	// register a second endpoint
	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate2.default.svc.cluster.local",
		"http-status", 2080, "10.2.0.2", 1081)

	cluster2 := "adsupdate2.default.svc.cluster.local|http-status"
	sendEDSReqAndVerify(cluster2)
}

func TestAdsUpdate(t *testing.T) {
	server := initLocalPilotTestEnv(t)
	edsstr := connectADS(t, util.MockPilotGrpcAddr)
	// Old style cluster.
	// TODO: convert tests (except eds) to new style.
	server.EnvoyXdsServer.MemRegistry.AddService("adsupdate.default.svc.cluster.local", &model.Service{
		Hostname: "adsupdate.default.svc.cluster.local",
		Address:  "10.11.0.1",
		Ports:    testPorts(0),
	})
	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.2.0.1", 1080)

	sendEDSReq(t, []string{"adsupdate.default.svc.cluster.local|http-main"},
		sidecarId("1.1.1.1", "app3"), edsstr)

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
	_ = ioutil.WriteFile(util.IstioOut+"/edsv2_sidecar.json", []byte(strResponse), 0644)

	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.1.7.1", 1080)

	// will trigger recompute and push for all clients - including some that may be closing
	// This reproduced the 'push on closed connection' bug.
	v2.PushAll()

	res1, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}
	strResponse, _ = model.ToJSONWithIndent(res1, " ")
	_ = ioutil.WriteFile(util.IstioOut+"/edsv2_update.json", []byte(strResponse), 0644)
	_ = edsstr.CloseSend()
}

// Make a direct EDS grpc request to pilot, verify the result is as expected.
func TestAdsMultiple(t *testing.T) {
	server := initLocalPilotTestEnv(t)

	wg := &sync.WaitGroup{}
	wgConnect := &sync.WaitGroup{}

	n := 10
	nPushes := 10

	wg.Add(n)
	wgConnect.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			edsstr := connectADS(t, util.MockPilotGrpcAddr)
			sendEDSReq(t, []string{"service3.default.svc.cluster.local|http-main"}, sidecarId(testIp(uint32(0x0a200000+i)), "app3"), edsstr)

			res1, err := adsReceive(edsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
			wgConnect.Done()

			cla, err := getLoadAssignment(res1)
			if err != nil {
				t.Fatal("Invalid EDS response ", err)
			}

			ep := cla.Endpoints
			if len(ep) == 0 {
				t.Fatal("No endpoints")
			}
			lbe := ep[0].LbEndpoints
			if len(lbe) == 0 {
				t.Fatal("No lb endpoints")
			}

			for j := 0; j < nPushes; j++ {
				_, err = adsReceive(edsstr, 5*time.Second)
				if err != nil {
					t.Fatal("Recv2 failed", err)
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
		v2.PushAll()
		log.Println("Push done ", j)
	}

	ok = waitTimeout(wg, 20*time.Second)
	if !ok {
		t.Fatal("Failed to receive all responses")
	}
}
