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
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/gogoprotomarshal"
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
	res, err := adsReceive(edsstr, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}

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
	res, _ = adsReceive(edsstr, 15*time.Second)

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

	_, _ = adsReceive(edsstr, 15*time.Second)

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
	_, _ = adsReceive(edsstr2, 15*time.Second)

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
		res, err := adsReceive(edsstr, 15*time.Second)
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

	cluster1 := "outbound|80||local.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster1)

	cluster2 := "outbound|80||hello.default.svc.cluster.local"
	sendEDSReqAndVerify(cluster2)
}

func TestAdsPushScoping(t *testing.T) {
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	const (
		svcSuffix = ".testPushScoping.com"
		ns1       = "ns1"
	)

	removeServiceByNames := func(ns string, names ...string) {
		configsUpdated := map[model.ConfigKey]struct{}{}

		for _, name := range names {
			hostname := host.Name(name)
			server.EnvoyXdsServer.MemRegistry.RemoveService(hostname)
			configsUpdated[model.ConfigKey{
				Kind:      model.ServiceEntryKind,
				Name:      string(hostname),
				Namespace: ns,
			}] = struct{}{}
		}

		server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true, ConfigsUpdated: configsUpdated})

	}
	removeService := func(ns string, indexes ...int) {
		var names []string

		for _, i := range indexes {
			names = append(names, fmt.Sprintf("svc%d%s", i, svcSuffix))
		}

		removeServiceByNames(ns, names...)
	}
	addServiceByNames := func(ns string, names ...string) {
		configsUpdated := map[model.ConfigKey]struct{}{}

		for _, name := range names {
			hostname := host.Name(name)
			configsUpdated[model.ConfigKey{
				Kind:      model.ServiceEntryKind,
				Name:      string(hostname),
				Namespace: ns,
			}] = struct{}{}

			server.EnvoyXdsServer.MemRegistry.AddService(hostname, &model.Service{
				Hostname: hostname,
				Address:  "10.11.0.1",
				Ports: []*model.Port{
					{
						Name:     "http-main",
						Port:     2080,
						Protocol: protocol.HTTP,
					},
				},
				Attributes: model.ServiceAttributes{
					Namespace: ns,
				},
			})
		}

		server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true, ConfigsUpdated: configsUpdated})
	}
	addService := func(ns string, indexes ...int) {
		var hostnames []string
		for _, i := range indexes {
			hostnames = append(hostnames, fmt.Sprintf("svc%d%s", i, svcSuffix))
		}
		addServiceByNames(ns, hostnames...)
	}

	addServiceInstance := func(hostname host.Name, indexes ...int) {
		for _, i := range indexes {
			server.EnvoyXdsServer.MemRegistry.AddEndpoint(hostname, "http-main", 2080, "192.168.1.10", i)
		}

		server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
			{Kind: model.ServiceEntryKind, Name: string(hostname), Namespace: model.IstioDefaultConfigNamespace}: {},
		}})
	}

	sc := &networking.Sidecar{
		Egress: []*networking.IstioEgressListener{
			{
				Hosts: []string{model.IstioDefaultConfigNamespace + "/*" + svcSuffix},
			},
		},
	}
	sidecarKind := collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind()
	if _, err := server.EnvoyXdsServer.MemConfigController.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:    sidecarKind.Kind,
			Version: sidecarKind.Version,
			Group:   sidecarKind.Group,
			Name:    "sc", Namespace: model.IstioDefaultConfigNamespace},
		Spec: sc,
	}); err != nil {
		t.Fatal(err)
	}
	addService(model.IstioDefaultConfigNamespace, 1, 2, 3)

	adscConn := adsConnectAndWait(t, 0x0a0a0a0a)
	defer adscConn.Close()

	type svcCase struct {
		ev          model.Event
		indexes     []int
		names       []string
		instIndexes []struct {
			name    string
			indexes []int
		}
		ns string

		expectUpdates   []string
		unexpectUpdates []string
	}
	svcCases := []svcCase{
		// Add a scoped service.
		{
			ev:            model.EventAdd,
			indexes:       []int{4},
			ns:            model.IstioDefaultConfigNamespace,
			expectUpdates: []string{"lds"},
		}, // then: default 1,2,3,4
		// Add instances to a scoped service.
		{
			ev: model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{fmt.Sprintf("svc%d%s", 4, svcSuffix), []int{1, 2}}},
			ns:            model.IstioDefaultConfigNamespace,
			expectUpdates: []string{"eds"},
		}, // then: default 1,2,3,4
		// Add a unscoped(name not match) service.
		{
			ev:              model.EventAdd,
			names:           []string{"foo.com"},
			ns:              model.IstioDefaultConfigNamespace,
			unexpectUpdates: []string{"lds"},
		}, // then: default 1,2,3,4, foo.com; ns1: 11
		// Add instances to an unscoped service.
		{
			ev: model.EventAdd,
			instIndexes: []struct {
				name    string
				indexes []int
			}{{"foo.com", []int{1, 2}}},
			ns:              model.IstioDefaultConfigNamespace,
			unexpectUpdates: []string{"eds"},
		}, // then: default 1,2,3,4
		// Add a unscoped(ns not match) service.
		{
			ev:              model.EventAdd,
			indexes:         []int{11},
			ns:              ns1,
			unexpectUpdates: []string{"lds"},
		}, // then: default 1,2,3,4, foo.com; ns1: 11
		// Remove a scoped service.
		{
			ev:            model.EventDelete,
			indexes:       []int{4},
			ns:            model.IstioDefaultConfigNamespace,
			expectUpdates: []string{"lds"},
		}, // then: default 1,2,3, foo.com; ns: 11
		// Remove a unscoped(name not match) service.
		{
			ev:              model.EventDelete,
			names:           []string{"foo.com"},
			ns:              model.IstioDefaultConfigNamespace,
			unexpectUpdates: []string{"lds"},
		}, // then: default 1,2,3; ns1: 11
		// Remove a unscoped(ns not match) service.
		{
			ev:              model.EventDelete,
			indexes:         []int{11},
			ns:              ns1,
			unexpectUpdates: []string{"lds"},
		}, // then: default 1,2,3
	}

	for i, c := range svcCases {
		fmt.Printf("begin %d case %v\n", i, c)

		var wantUpdates []string
		wantUpdates = append(wantUpdates, c.expectUpdates...)
		wantUpdates = append(wantUpdates, c.unexpectUpdates...)

		switch c.ev {
		case model.EventAdd:
			if len(c.indexes) > 0 {
				addService(c.ns, c.indexes...)
			}
			if len(c.names) > 0 {
				addServiceByNames(c.ns, c.names...)
			}
			if len(c.instIndexes) > 0 {
				for _, instIndex := range c.instIndexes {
					addServiceInstance(host.Name(instIndex.name), instIndex.indexes...)
				}
			}
		case model.EventDelete:
			if len(c.indexes) > 0 {
				removeService(c.ns, c.indexes...)
			}
			if len(c.names) > 0 {
				removeServiceByNames(c.ns, c.names...)
			}
		default:
			t.Fatalf("wrong event for case %v", c)
		}

		time.Sleep(200 * time.Millisecond)
		upd, _ := adscConn.Wait(5*time.Second, wantUpdates...) // XXX slow for unexpect ...
		for _, expect := range c.expectUpdates {
			if !contains(upd, expect) {
				t.Fatalf("expect %s but not contains (%v) for case %v", expect, upd, c)
			}
		}
		for _, unexpect := range c.unexpectUpdates {
			if contains(upd, unexpect) {
				t.Fatalf("unexpect %s but contains (%v) for case %v", unexpect, upd, c)
			}
		}
	}
}

func TestAdsUpdate(t *testing.T) {
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	server.EnvoyXdsServer.MemRegistry.AddService("adsupdate.default.svc.cluster.local", &model.Service{
		Hostname: "adsupdate.default.svc.cluster.local",
		Address:  "10.11.0.1",
		Ports: []*model.Port{
			{
				Name:     "http-main",
				Port:     2080,
				Protocol: protocol.HTTP,
			},
		},
		Attributes: model.ServiceAttributes{
			Name:      "adsupdate",
			Namespace: "default",
		},
	})
	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	time.Sleep(time.Millisecond * 200)
	server.EnvoyXdsServer.MemRegistry.SetEndpoints("adsupdate.default.svc.cluster.local", "default",
		newEndpointWithAccount("10.2.0.1", "hello-sa", "v1"))

	err = sendEDSReq([]string{"outbound|2080||adsupdate.default.svc.cluster.local"}, sidecarID("1.1.1.1", "app3"), edsstr)
	if err != nil {
		t.Fatal(err)
	}

	res1, err := adsReceive(edsstr, 15*time.Second)
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
	if lbe[0].GetEndpoint().Address.GetSocketAddress().Address != "10.2.0.1" {
		t.Error("Expecting 10.2.0.1 got ", lbe[0].GetEndpoint().Address.GetSocketAddress().Address)
	}
	strResponse, _ := gogoprotomarshal.ToJSONWithIndent(res1, " ")
	_ = ioutil.WriteFile(env.IstioOut+"/edsv2_sidecar.json", []byte(strResponse), 0644)

	_ = server.EnvoyXdsServer.MemRegistry.AddEndpoint("adsupdate.default.svc.cluster.local",
		"http-main", 2080, "10.1.7.1", 1080)

	// will trigger recompute and push for all clients - including some that may be closing
	// This reproduced the 'push on closed connection' bug.
	v2.AdsPushAll(server.EnvoyXdsServer)

	res1, err = adsReceive(edsstr, 15*time.Second)
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}
	strResponse, _ = gogoprotomarshal.ToJSONWithIndent(res1, " ")
	_ = ioutil.WriteFile(env.IstioOut+"/edsv2_update.json", []byte(strResponse), 0644)
}

func TestEnvoyRDSProtocolError(t *testing.T) {
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	edsstr, cancel, err := connectADS(util.MockPilotGrpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA, routeB}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(edsstr, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}

	v2.AdsPushAll(server.EnvoyXdsServer)

	res, err = adsReceive(edsstr, 15*time.Second)
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

	res, err = adsReceive(edsstr, 15*time.Second)
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

	err = sendRDSReq(gatewayID(gatewayIP), []string{routeA}, "", edsstr)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adsReceive(edsstr, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Resources) == 0 {
		t.Fatal("No routes returned")
	}
	route1, err := unmarshallRoute(res.Resources[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) != 1 || route1.Name != routeA {
		t.Fatal("Expected only the http.80 route to be returned")
	}

	v2.AdsPushAll(server.EnvoyXdsServer)

	res, err = adsReceive(edsstr, 15*time.Second)
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
	res, err = adsReceive(edsstr, 15*time.Second)
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

	res, err = adsReceive(edsstr, 15*time.Second)
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
	res, err = adsReceive(edsstr, 15*time.Second)
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

	err := proto.Unmarshal(value, route)
	if err != nil {
		return nil, err
	}
	return route, nil
}
