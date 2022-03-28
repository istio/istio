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

package xds_test

import (
	"reflect"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestDeltaAds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.ClusterType)
	ads.RequestResponseAck(nil)
}

func TestDeltaAdsClusterUpdate(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ads := s.ConnectDeltaADS().WithType(v3.EndpointType)
	nonce := ""
	sendEDSReqAndVerify := func(add, remove, expect []string) {
		t.Helper()
		res := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
			ResponseNonce:            nonce,
			ResourceNamesSubscribe:   add,
			ResourceNamesUnsubscribe: remove,
		})
		nonce = res.Nonce
		got := xdstest.MapKeys(xdstest.ExtractLoadAssignments(xdstest.UnmarshalClusterLoadAssignment(t, model.ResourcesToAny(res.Resources))))
		if !reflect.DeepEqual(expect, got) {
			t.Fatalf("expected clusters %v got %v", expect, got)
		}
	}

	sendEDSReqAndVerify([]string{"outbound|80||local.default.svc.cluster.local"}, nil, []string{"outbound|80||local.default.svc.cluster.local"})
	// Only send the one that is requested
	sendEDSReqAndVerify([]string{"outbound|81||local.default.svc.cluster.local"}, nil, []string{"outbound|81||local.default.svc.cluster.local"})
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResponseNonce:            nonce,
		ResourceNamesUnsubscribe: []string{"outbound|81||local.default.svc.cluster.local"},
	})
	ads.ExpectNoResponse()
}

func TestDeltaEDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-locality.yaml"),
		DiscoveryServerModifier: func(s *xds.DiscoveryServer) {
			addTestClientEndpoints(s)
			s.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
			s.MemRegistry.SetEndpoints(edsIncSvc, "",
				newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))
		},
	})

	ads := s.ConnectDeltaADS().WithType(v3.EndpointType)
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"outbound|80||test-1.default"},
	})
	resp := ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|80||test-1.default" {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"outbound|8080||" + edsIncSvc},
	})
	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// update endpoint
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// update svc, only send the eds for this service
	s.Discovery.MemRegistry.AddHTTPService(edsIncSvc, "10.10.1.3", 8080)
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, ConfigsUpdated: map[model.ConfigKey]struct{}{{
		Kind:      gvk.ServiceEntry,
		Name:      edsIncSvc,
		Namespace: "",
	}: {}}})

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// delete svc, only send eds fot this service
	s.Discovery.MemRegistry.RemoveService(edsIncSvc)
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, ConfigsUpdated: map[model.ConfigKey]struct{}{{
		Kind:      gvk.ServiceEntry,
		Name:      edsIncSvc,
		Namespace: "",
	}: {}}})

	resp = ads.ExpectResponse()
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}
	if len(resp.Resources) != 0 {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
}
