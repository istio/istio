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
	"testing"
	"time"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
)

const (
	routeA = "http.80"
	routeB = "https.443.https.my-gateway.testns"
)

// Regression for envoy restart and overlapping connections
func TestAdsReconnectWithNonce(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()
	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	err = sendEDSReq([]string{"outbound|1080||service3.default.svc.cluster.local"}, sidecarID(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := adsReceive(edsstr, 5*time.Second)

	// closes old process
	cancel()

	edsstr, cancel, err = connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	err = sendEDSReqReconnect([]string{"outbound|1080||service3.default.svc.cluster.local"}, edsstr, res)
	if err != nil {
		t.Fatal(err)
	}
	err = sendEDSReq([]string{"outbound|1080||service3.default.svc.cluster.local"}, sidecarID(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, _ = adsReceive(edsstr, 5*time.Second)

	t.Log("Received ", res)
}

// Regression for envoy restart and overlapping connections
func TestAdsReconnect(t *testing.T) {
	s, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	err = sendCDSReq(sidecarID(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = adsReceive(edsstr, 5*time.Second)

	// envoy restarts and reconnects
	edsstr2, cancel2, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel2()
	err = sendCDSReq(sidecarID(app3Ip, "app3"), edsstr2)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = adsReceive(edsstr2, 5*time.Second)

	// closes old process
	cancel()

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
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADSS(util.MockPilotSecureAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	err = sendCDSReq(sidecarID(app3Ip, "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adsReceive(edsstr, 3*time.Second)
	if err != nil {
		t.Error("Failed to receive with TLS connection ", err)
	}
}

func TestAdsClusterUpdate(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	var sendEDSReqAndVerify = func(clusterName string) {
		err = sendEDSReq([]string{clusterName}, sidecarID("1.1.1.1", "app3"), edsstr)
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

	cluster1 := "outbound|80||adsclusterupdate.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster1)

	cluster2 := "outbound|80||adsclusterupdate2.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster2)
}

func TestEnvoyRDSProtocolError(t *testing.T) {
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	// wait for debounce
	time.Sleep(3 * v2.DebounceAfter)

	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA, routeB}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}

	v2.AdsPushAll(server.EnvoyXdsServer)

	res, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) != 2 {
		t.Fatal("No routes returned")
	}

	// send a protocol error
	err = sendRDSReq(gatewayID(gatewayIP), nil, res.Nonce, edsstr)
	if err != nil {
		t.Fatal(err)
	}
	// Refresh routes
	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA, routeB}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}

	res, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes after protocol error")
	}
}

func TestEnvoyRDSUpdatedRouteRequest(t *testing.T) {
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	// wait for debounce
	time.Sleep(3 * v2.DebounceAfter)

	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err := unmarshallRoute(res.Resources[0].Value)
	if err != nil || len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	v2.AdsPushAll(server.EnvoyXdsServer)

	res, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	if len(res.Resources) != 1 {
		t.Fatal("Expected only 1 route to be returned")
	}
	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil || len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	// Test update from A -> B
	err = sendRDSReq(gatewayID(gatewayIP), []string{routeB}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil || len(res.Resources) != 1 || route1.Name != routeB {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	// Test update from B -> A, B
	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA, routeB}, res.Nonce, edsstr)
	if err != nil {
		t.Fatal(err)
	}

	res, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes after protocol error")
	}
	if len(res.Resources) != 2 {
		t.Fatal("Expected 2 routes to be returned")
	}

	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	route2, err := unmarshallRoute(res.Resources[1].Value)
	if err != nil {
		t.Fatal(err)
	}

	if (route1.Name == routeA && route2.Name != routeB) || (route2.Name == routeA && route1.Name != routeB) {
		t.Fatal("Expected http.80 and https.443.http routes to be returned")
	}

	// Test update from B, B -> A

	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, err = adsReceive(edsstr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err = unmarshallRoute(res.Resources[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}
}

func unmarshallRoute(value []byte) (*xdsapi.RouteConfiguration, error) {
	route := &xdsapi.RouteConfiguration{}
	err := route.Unmarshal(value)
	if err != nil {
		return nil, err
	}
	return route, nil
}
