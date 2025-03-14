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
	"fmt"
	"reflect"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	xdsserver "istio.io/istio/pkg/xds"
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
		got := xdstest.MapKeys(xdstest.ExtractLoadAssignments(xdstest.UnmarshalClusterLoadAssignment(t, xdsserver.ResourcesToAny(res.Resources))))
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

func TestDeltaCDS(t *testing.T) {
	base := sets.New("BlackHoleCluster", "PassthroughCluster", "InboundPassthroughCluster")
	assertResources := func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
		t.Helper()
		got := slices.Map(resp.Resources, (*discovery.Resource).GetName)

		assert.Equal(t, sets.New(got...), sets.New(names...).Merge(base))
	}
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	spamDebugEndpointsToDetectRace(t, s)
	addTestClientEndpoints(s.MemRegistry)
	s.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))
	// Wait until the above debounce, to ensure we can precisely check XDS responses without spurious pushes
	s.EnsureSynced(t)

	ads := s.ConnectDeltaADS().WithID("sidecar~127.0.0.1~test.default~default.svc.cluster.local")

	// Initially we get everything
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{},
	})
	resp := ads.ExpectResponse()
	assertResources(resp, "outbound|80||test-1.default", "outbound|8080||eds.test.svc.cluster.local", "inbound|80||")
	assert.Equal(t, resp.RemovedResources, nil)

	// On remove, just get the removal
	s.MemRegistry.RemoveService("test-1.default")
	resp = ads.ExpectResponse()
	assertResources(resp, "inbound|80||") // currently we always send the inbound stuff. Not ideal, but acceptable
	assert.Equal(t, resp.RemovedResources, []string{"outbound|80||test-1.default"})

	// Another removal should behave the same
	s.MemRegistry.RemoveService("eds.test.svc.cluster.local")
	resp = ads.ExpectResponse()
	assertResources(resp)
	assert.Equal(t, resp.RemovedResources, []string{"inbound|80||", "outbound|8080||eds.test.svc.cluster.local"})
}

func TestDeltaCDSReconnect(t *testing.T) {
	base := sets.New("BlackHoleCluster", "PassthroughCluster", "InboundPassthroughCluster")
	assertResources := func(resp *discovery.DeltaDiscoveryResponse, names ...string) {
		t.Helper()
		got := slices.Map(resp.Resources, (*discovery.Resource).GetName)

		assert.Equal(t, sets.New(got...), sets.New(names...).Merge(base))
	}
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addTestClientEndpoints(s.MemRegistry)
	s.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))
	// Wait until the above debounce, to ensure we can precisely check XDS responses without spurious pushes
	s.EnsureSynced(t)

	ads := s.ConnectDeltaADS()

	// Initially we get everything
	resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
	assertResources(resp, "outbound|80||test-1.default", "outbound|8080||eds.test.svc.cluster.local")
	assert.Equal(t, resp.RemovedResources, nil)

	// Disconnect and remove a service
	ads.Cleanup()
	s.MemRegistry.RemoveService("test-1.default")
	s.MemRegistry.AddHTTPService("eds2.test.svc.cluster.local", "10.10.1.3", 8080)
	s.EnsureSynced(t)
	ads = s.ConnectDeltaADS()
	resp = ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		InitialResourceVersions: map[string]string{
			"outbound|80||test-1.default":               "",
			"outbound|8080||eds.test.svc.cluster.local": "",
		},
	})
	assertResources(resp, "outbound|8080||eds.test.svc.cluster.local", "outbound|8080||eds2.test.svc.cluster.local")
	assert.Equal(t, resp.RemovedResources, []string{"outbound|80||test-1.default"})

	// Another removal should behave the same
	s.MemRegistry.RemoveService("eds.test.svc.cluster.local")
	resp = ads.ExpectResponse()
	// ACK
	ads.Request(&discovery.DeltaDiscoveryRequest{
		TypeUrl:       resp.TypeUrl,
		ResponseNonce: resp.Nonce,
	})
	assertResources(resp)
	assert.Equal(t, resp.RemovedResources, []string{"outbound|8080||eds.test.svc.cluster.local"})

	// Another removal should behave the same
	s.MemRegistry.RemoveService("eds2.test.svc.cluster.local")
	resp = ads.ExpectResponse()
	assertResources(resp)
	assert.Equal(t, resp.RemovedResources, []string{"outbound|8080||eds2.test.svc.cluster.local"})
}

func TestDeltaEDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "tests/testdata/config/destination-rule-locality.yaml"),
	})
	addTestClientEndpoints(s.MemRegistry)
	s.MemRegistry.AddHTTPService(edsIncSvc, edsIncVip, 8080)
	s.MemRegistry.SetEndpoints(edsIncSvc, "",
		newEndpointWithAccount("127.0.0.1", "hello-sa", "v1"))

	// Wait until the above debounce, to ensure we can precisely check XDS responses without spurious pushes
	s.EnsureSynced(t)

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

	t.Logf("update svc")
	// update svc, only send the eds for this service
	s.MemRegistry.AddHTTPService(edsIncSvc, "10.10.1.3", 8080)

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// delete svc, only send eds for this service
	s.MemRegistry.RemoveService(edsIncSvc)

	resp = ads.ExpectResponse()
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != "outbound|8080||"+edsIncSvc {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}
	if len(resp.Resources) != 0 {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
}

func TestDeltaReconnectRequests(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		Services: []*model.Service{
			{
				Hostname:       "adsupdate.example.com",
				DefaultAddress: "10.11.0.1",
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
			},
			{
				Hostname:       "adsstatic.example.com",
				DefaultAddress: "10.11.0.2",
				Ports: []*model.Port{
					{
						Name:     "http-main",
						Port:     2080,
						Protocol: protocol.HTTP,
					},
				},
				Attributes: model.ServiceAttributes{
					Name:      "adsstatic",
					Namespace: "default",
				},
			},
		},
	})

	const updateCluster = "outbound|2080||adsupdate.example.com"
	const staticCluster = "outbound|2080||adsstatic.example.com"
	ads := s.ConnectDeltaADS()
	// Send initial request
	res := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{TypeUrl: v3.ClusterType})
	// we must get the cluster back
	if resn := xdstest.ExtractResource(res.Resources); !resn.Contains(updateCluster) || !resn.Contains(staticCluster) {
		t.Fatalf("unexpected resources: %v", resn)
	}

	// A push should get a response
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true, Forced: true})
	ads.ExpectResponse()

	// Close the connection
	ads.Cleanup()

	// Service is removed while connection is closed
	s.MemRegistry.RemoveService("adsupdate.example.com")
	s.Discovery.ConfigUpdate(&model.PushRequest{
		Full:           true,
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "adsupdate.example.com", Namespace: "default"}),
	})
	s.EnsureSynced(t)

	ads = s.ConnectDeltaADS()
	// Sometimes we get an EDS request first before CDS
	ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		TypeUrl:                v3.EndpointType,
		ResponseNonce:          "",
		ResourceNamesSubscribe: []string{"outbound|80||local.default.svc.cluster.local"},
	})

	// Now send initial CDS request
	res = ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		TypeUrl: v3.ClusterType,
		InitialResourceVersions: map[string]string{
			// This time we include the version map, since it is a reconnect
			staticCluster: "",
			updateCluster: "",
		},
	})

	// Expect that we send an EDS response even though there was no request
	resp := ads.ExpectResponse()
	if resp.TypeUrl != v3.EndpointType {
		t.Fatalf("unexpected response type %v. Expected dependent EDS response", resp.TypeUrl)
	}
	// we must NOT get the cluster back
	if resn := xdstest.ExtractResource(res.Resources); resn.Contains(updateCluster) || !resn.Contains(staticCluster) {
		t.Fatalf("unexpected resources: %v", resn)
	}
	// It should be removed
	if resn := sets.New(res.RemovedResources...); !resn.Contains(updateCluster) {
		t.Fatalf("unexpected remove resources: %v", resn)
	}
}

func init() {
	features.EnableAmbient = true
}

func TestDeltaWDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	wlA := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:       fmt.Sprintf("Kubernetes//Pod/%s/%s", "test", "a"),
			Namespace: "test",
			Name:      "a",
		},
	}
	wlB := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:       fmt.Sprintf("Kubernetes//Pod/%s/%s", "test", "b"),
			Namespace: "test",
			Name:      "n",
		},
	}
	wlC := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Uid:       fmt.Sprintf("Kubernetes//Pod/%s/%s", "test", "c"),
			Namespace: "test",
			Name:      "c",
		},
	}
	svcA := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "a",
			Namespace: "default",
			Hostname:  "a.default.svc.cluster.local",
		},
	}
	svcB := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "b",
			Namespace: "default",
			Hostname:  "b.default.svc.cluster.local",
		},
	}
	svcC := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      "c",
			Namespace: "default",
			Hostname:  "c.default.svc.cluster.local",
		},
	}
	s.MemRegistry.AddWorkloadInfo(wlA, wlB, wlC)
	s.MemRegistry.AddServiceInfo(svcA, svcB, svcC)

	// Wait until the above debounce, to ensure we can precisely check XDS responses without spurious pushes
	s.EnsureSynced(t)

	ads := s.ConnectDeltaADS().WithType(v3.AddressType).WithID("ztunnel~1.1.1.1~test.default~default.svc.cluster.local")
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{"*"},
	})
	resp := ads.ExpectResponse()
	if len(resp.Resources) != 6 {
		t.Fatalf("received unexpected eds resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// simulate a svc update
	s.XdsUpdater.ConfigUpdate(&model.PushRequest{
		AddressesUpdated: sets.New(svcA.ResourceName()),
	})

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 1 || resp.Resources[0].Name != svcA.ResourceName() {
		t.Fatalf("received unexpected address resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// simulate a svc delete
	s.MemRegistry.RemoveServiceInfo(svcA)
	s.XdsUpdater.ConfigUpdate(&model.PushRequest{
		AddressesUpdated: sets.New(svcA.ResourceName()),
	})

	resp = ads.ExpectResponse()
	if len(resp.Resources) != 0 {
		t.Fatalf("received unexpected address resource %v", resp.Resources)
	}
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != svcA.ResourceName() {
		t.Fatalf("received unexpected removed eds resource %v", resp.RemovedResources)
	}

	// delete workload
	s.MemRegistry.RemoveWorkloadInfo(wlA)
	// a pod delete event
	s.XdsUpdater.ConfigUpdate(&model.PushRequest{
		AddressesUpdated: sets.New(wlA.ResourceName()),
	})

	resp = ads.ExpectResponse()
	if len(resp.RemovedResources) != 1 || resp.RemovedResources[0] != wlA.ResourceName() {
		t.Fatalf("received unexpected removed wds resource %v", resp.RemovedResources)
	}
	if len(resp.Resources) != 0 {
		t.Fatalf("received unexpected wds resource %v", resp.Resources)
	}
}

func TestDeltaUnsub(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

	ads := s.ConnectDeltaADS().WithID("sidecar~127.0.0.1~test.default~default.svc.cluster.local")

	runAssert := func(nonce string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			sync := getSyncStatus(t, s.Discovery)
			if len(sync) != 1 {
				return fmt.Errorf("got %v sync status", len(sync))
			}
			if sync[0].ClusterSent != nonce {
				return fmt.Errorf("want %q, got %q for send", nonce, sync[0].ClusterSent)
			}
			if sync[0].ClusterAcked != nonce {
				return fmt.Errorf("want %q, got %q for ack", nonce, sync[0].ClusterAcked)
			}
			return nil
		})
	}
	// Initially we get everything
	resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		ResourceNamesSubscribe: []string{},
	})
	runAssert(resp.Nonce)
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResourceNamesUnsubscribe: []string{"something"},
	})
	runAssert(resp.Nonce)
}
