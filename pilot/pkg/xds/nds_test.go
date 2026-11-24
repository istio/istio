// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package xds_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdspkg "istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

func TestNDS(t *testing.T) {
	// The "auto allocate" test only needs a special case for the legacy auto allocation mode, so we disable the new one here
	// and only test the old one. The new one appears identically to manually-allocated SE from NDS perspective.
	test.SetForTest(t, &features.EnableIPAutoallocate, false)
	cases := []struct {
		name     string
		meta     model.NodeMetadata
		expected *dnsProto.NameTable
	}{
		{
			name: "auto allocate",
			meta: model.NodeMetadata{
				DNSCapture:      true,
				DNSAutoAllocate: true,
			},
			expected: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"random-1.host.example": {
						Ips:      []string{"240.240.116.21"},
						Registry: "External",
					},
					"random-2.host.example": {
						Ips:      []string{"9.9.9.9"},
						Registry: "External",
					},
					"random-3.host.example": {
						Ips:      []string{"240.240.81.100"},
						Registry: "External",
					},
				},
			},
		},
		{
			name: "just capture",
			meta: model.NodeMetadata{
				DNSCapture: true,
			},
			expected: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"random-2.host.example": {
						Ips:      []string{"9.9.9.9"},
						Registry: "External",
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				ConfigString: mustReadFile(t, "./testdata/nds-se.yaml"),
			})

			ads := s.ConnectADS().WithType(v3.NameTableType)
			res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
				Node: &core.Node{
					Id:       ads.ID,
					Metadata: tt.meta.ToStruct(),
				},
			})

			nt := &dnsProto.NameTable{}
			err := res.Resources[0].UnmarshalTo(nt)
			if err != nil {
				t.Fatal("Failed to unmarshal name table", err)
				return
			}
			if len(nt.Table) == 0 {
				t.Fatal("expected more than 0 entries in name table")
			}
			if diff := cmp.Diff(nt, tt.expected, protocmp.Transform()); diff != "" {
				t.Fatalf("name table does not match expected value:\n %v", diff)
			}
		})
	}
}

var deltaNDSMetadata = model.NodeMetadata{DNSCapture: true, DeltaNDS: true, IstioVersion: "1.33.0"}

func addNDSService(s *xds.FakeDiscoveryServer, hostname, address string) {
	s.MemRegistry.AddService(&model.Service{
		Hostname:       host.Name(hostname),
		DefaultAddress: address,
		Attributes:     model.ServiceAttributes{Namespace: "default"},
	})
}

func ndsTable(t *testing.T, resource *anypb.Any) *dnsProto.NameTable {
	t.Helper()
	table := &dnsProto.NameTable{}
	if err := resource.UnmarshalTo(table); err != nil {
		t.Fatal(err)
	}
	return table
}

// deltaNDSIPs returns the addresses of each resource's own hostname.
func deltaNDSIPs(t *testing.T, response *discovery.DeltaDiscoveryResponse) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, resource := range response.Resources {
		out[resource.Name] = slices.Sort(ndsTable(t, resource.Resource).GetTable()[resource.Name].GetIps())
	}
	return out
}

func ndsServiceEntry(name, namespace, address string, exportTo []string, hosts ...string) config.Config {
	return config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Name: name, Namespace: namespace},
		Spec: &networking.ServiceEntry{
			Hosts:      hosts,
			Addresses:  []string{address},
			Ports:      []*networking.ServicePort{{Number: 80, Name: "http", Protocol: "HTTP"}},
			Resolution: networking.ServiceEntry_NONE,
			ExportTo:   exportTo,
		},
	}
}

func ndsSidecar(namespace string, egress ...*networking.IstioEgressListener) config.Config {
	return config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.Sidecar, Name: "sidecar", Namespace: namespace},
		Spec: &networking.Sidecar{Egress: egress},
	}
}

func pushServiceEntryKey(s *xds.FakeDiscoveryServer, hostname, namespace string) {
	s.Discovery.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: hostname, Namespace: namespace}),
		Reason:         model.NewReasonStats(model.ServiceUpdate),
	})
}

func TestNDSDeltaWireFormat(t *testing.T) {
	newServer := func(t *testing.T) *xds.FakeDiscoveryServer {
		t.Helper()
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
			ConfigString: mustReadFile(t, "./testdata/nds-se.yaml"),
		})
		s.EnsureSynced(t)
		return s
	}

	t.Run("capable agent receives named resources", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) == 0 {
			t.Fatal("expected named NDS resources")
		}
		for _, resource := range response.Resources {
			if resource.Name == "" {
				t.Fatal("delta-capable agent received an unnamed full-table resource")
			}
			if table := ndsTable(t, resource.Resource); table.GetTable()[resource.Name] == nil {
				t.Fatalf("resource %q does not contain its own hostname: %v", resource.Name, table)
			}
		}
	})

	for _, version := range []string{"1.32.0", "1.32.9"} {
		t.Run("configured "+version+" agent receives unnamed full table", func(t *testing.T) {
			s := newServer(t)
			metadata := deltaNDSMetadata
			metadata.IstioVersion = version
			ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(metadata)
			response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
			if len(response.Resources) != 1 || response.Resources[0].Name != "" {
				t.Fatalf("older agent selected named resources: %v", response.Resources)
			}
		})
	}

	t.Run("SotW stream of a configured agent receives unnamed full table", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectADS().WithType(v3.NameTableType)
		response := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
			Node: &core.Node{Id: ads.ID, Metadata: deltaNDSMetadata.ToStruct()},
		})
		if len(response.Resources) != 1 {
			t.Fatalf("expected one full-table resource, got %v", response.Resources)
		}
		table := ndsTable(t, response.Resources[0])
		if diff := cmp.Diff([]string{"9.9.9.9"}, table.GetTable()["random-2.host.example"].GetIps()); diff != "" {
			t.Fatalf("SotW response is not the full table (-want +got):\n%s", diff)
		}
	})

	t.Run("configured custom-version agent receives named resources", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture:   true,
			DeltaNDS:     true,
			IstioVersion: "custom-build",
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) == 0 || response.Resources[0].Name == "" {
			t.Fatalf("custom-version agent did not receive named resources: %v", response.Resources)
		}
	})

	t.Run("configured agent without version receives named resources", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture: true,
			DeltaNDS:   true,
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) == 0 || response.Resources[0].Name == "" {
			t.Fatalf("agent without a version did not receive named resources: %v", response.Resources)
		}
	})

	t.Run("legacy allocator agent receives unnamed full table", func(t *testing.T) {
		test.SetForTest(t, &features.EnableIPAutoallocate, false)
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture:      true,
			DNSAutoAllocate: true,
			DeltaNDS:        true,
			IstioVersion:    "1.33.0",
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) != 1 || response.Resources[0].Name != "" {
			t.Fatalf("legacy allocator agent selected named resources: %v", response.Resources)
		}
	})

	t.Run("legacy agent receives unnamed full table", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture:   true,
			IstioVersion: "1.33.0",
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) != 1 || response.Resources[0].Name != "" {
			t.Fatalf("expected one unnamed compatibility resource, got %v", response.Resources)
		}
		if table := ndsTable(t, response.Resources[0].Resource); len(table.GetTable()) == 0 {
			t.Fatalf("legacy response does not contain a full table: %v", table)
		}
	})
}

func TestNDSDeltaIncrementalPushes(t *testing.T) {
	for _, nodeType := range []model.NodeType{model.SidecarProxy, model.Router} {
		t.Run(string(nodeType), func(t *testing.T) {
			testNDSDeltaIncrementalPushes(t, nodeType)
		})
	}
}

func testNDSDeltaIncrementalPushes(t *testing.T, nodeType model.NodeType) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addNDSService(s, "a.example.com", "10.0.0.1")
	s.EnsureSynced(t)
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithNodeType(nodeType).WithMetadata(deltaNDSMetadata)
	assertResourceNames := func(want ...string) {
		t.Helper()
		clients := s.Discovery.Clients()
		if len(clients) != 1 {
			t.Fatalf("expected one client, got %d", len(clients))
		}
		proxy := clients[0].Proxy()
		proxy.RLock()
		watched, found := proxy.DeepCloneWatchedResourcesLocked()[v3.NameTableType]
		proxy.RUnlock()
		if !found || !watched.ResourceNames.Equals(sets.New(want...)) {
			t.Fatalf("unexpected NDS resource names %v, want %v", watched.ResourceNames, want)
		}
	}
	ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
	assertResourceNames("a.example.com")

	addNDSService(s, "b.example.com", "10.0.0.2")
	response := ads.ExpectResponse()
	if len(response.Resources) != 1 || response.Resources[0].Name != "b.example.com" {
		t.Fatalf("service add was not incremental: %v", response.Resources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})
	assertResourceNames("a.example.com", "b.example.com")

	addNDSService(s, "b.example.com", "10.0.0.3")
	response = ads.ExpectResponse()
	if len(response.Resources) != 1 || response.Resources[0].Name != "b.example.com" {
		t.Fatalf("service update was not incremental: %v", response.Resources)
	}
	updated := ndsTable(t, response.Resources[0].Resource)
	if diff := cmp.Diff([]string{"10.0.0.3"}, updated.GetTable()["b.example.com"].GetIps()); diff != "" {
		t.Fatalf("updated resource has unexpected addresses (-want +got):\n%s", diff)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})

	s.MemRegistry.RemoveService("b.example.com")
	response = ads.ExpectResponse()
	if len(response.Resources) != 0 || !cmp.Equal(response.RemovedResources, []string{"b.example.com"}) {
		t.Fatalf("service removal was not incremental: resources=%v removed=%v", response.Resources, response.RemovedResources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})
	assertResourceNames("a.example.com")
}

func TestNDSDeltaReconnectRemovesStaleResources(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addNDSService(s, "a.example.com", "10.0.0.1")
	s.EnsureSynced(t)
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
	response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{
		InitialResourceVersions: map[string]string{"a.example.com": "", "stale.example.com": ""},
	})
	if len(response.Resources) != 1 || response.Resources[0].Name != "a.example.com" {
		t.Fatalf("unexpected reconnect resources: %v", response.Resources)
	}
	if diff := cmp.Diff([]string{"stale.example.com"}, response.RemovedResources); diff != "" {
		t.Fatalf("reconnect did not remove stale resources (-want +got):\n%s", diff)
	}
}

func TestNDSDeltaNackLifecycle(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addNDSService(s, "a.example.com", "10.0.0.1")
	s.EnsureSynced(t)
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
	ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

	addNDSService(s, "b.example.com", "10.0.0.1")
	rejected := ads.ExpectResponse()
	if len(rejected.Resources) != 1 || rejected.Resources[0].Name != "b.example.com" {
		t.Fatalf("unexpected rejected response: %v", rejected.Resources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResponseNonce: rejected.Nonce,
		ErrorDetail:   &status.Status{Message: "rejected NDS response"},
	})
	ads.ExpectNoResponse()

	addNDSService(s, "c.example.com", "10.0.0.1")
	response := ads.ExpectResponse()
	if len(response.Resources) != 1 || response.Resources[0].Name != "c.example.com" {
		t.Fatalf("NACK changed generic delta progression: %v", response.Resources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})

	s.Discovery.ConfigUpdate(&model.PushRequest{Forced: true})
	response = ads.ExpectResponse()
	got := sets.New(slices.Map(response.Resources, func(r *discovery.Resource) string { return r.Name })...)
	if len(response.RemovedResources) != 0 {
		t.Fatalf("forced reconciliation removed resources: %v", response.RemovedResources)
	}
	want := sets.New("a.example.com", "b.example.com", "c.example.com")
	if !got.Equals(want) {
		t.Fatalf("forced reconciliation after NACK returned %v, want %v", got, want)
	}
}

func TestNDSInitialNackContinuesDeltas(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addNDSService(s, "a.example.com", "10.0.0.1")
	s.EnsureSynced(t)
	rejectedClient := s.ConnectDeltaADS().WithID("sidecar~1.1.1.1~rejected.default~default.svc.cluster.local").
		WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
	healthyClient := s.ConnectDeltaADS().WithID("sidecar~1.1.1.2~healthy.default~default.svc.cluster.local").
		WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)

	rejectedClient.Request(&discovery.DeltaDiscoveryRequest{})
	rejected := rejectedClient.ExpectResponse()
	rejectedClient.Request(&discovery.DeltaDiscoveryRequest{
		ResponseNonce: rejected.Nonce,
		ErrorDetail:   &status.Status{Message: "rejected initial NDS response"},
	})
	rejectedClient.ExpectNoResponse()
	healthyClient.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

	addNDSService(s, "b.example.com", "10.0.0.1")
	delta := rejectedClient.ExpectResponse()
	if len(delta.Resources) != 1 || delta.Resources[0].Name != "b.example.com" {
		t.Fatalf("initial NACK changed generic delta progression: %v", delta.Resources)
	}

	delta = healthyClient.ExpectResponse()
	if len(delta.Resources) != 1 || delta.Resources[0].Name != "b.example.com" {
		t.Fatalf("initial NACK affected another client: %v", delta.Resources)
	}
}

func TestGenerate(t *testing.T) {
	nt := &dnsProto.NameTable{
		Table: make(map[string]*dnsProto.NameTable_NameInfo),
	}
	emptyNameTable := model.Resources{&discovery.Resource{Resource: protoconv.MessageToAny(nt)}}

	cases := []struct {
		name      string
		proxy     *model.Proxy
		resources []string
		request   *model.PushRequest
		nameTable []*discovery.Resource
	}{
		{
			name:      "partial push with headless endpoint update",
			proxy:     &model.Proxy{Type: model.SidecarProxy},
			request:   &model.PushRequest{Reason: model.NewReasonStats(model.HeadlessEndpointUpdate), Forced: true},
			nameTable: emptyNameTable,
		},
		{
			name:      "forced push",
			proxy:     &model.Proxy{Type: model.SidecarProxy},
			request:   &model.PushRequest{Forced: true},
			nameTable: emptyNameTable,
		},
		{
			name:      "partial push with no headless endpoint update",
			proxy:     &model.Proxy{Type: model.SidecarProxy},
			request:   &model.PushRequest{},
			nameTable: nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.proxy.Metadata == nil {
				tt.proxy.Metadata = &model.NodeMetadata{}
			}
			tt.proxy.Metadata.ClusterID = constants.DefaultClusterName
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

			gen := s.Discovery.Generators[v3.NameTableType]
			tt.request.Start = time.Now()
			nametable, _, _ := gen.Generate(s.SetupProxy(tt.proxy), &model.WatchedResource{ResourceNames: sets.New(tt.resources...)}, tt.request)
			if len(tt.nameTable) == 0 {
				if len(nametable) != 0 {
					t.Errorf("unexpected nametable. want: %v, got: %v", tt.nameTable, nametable)
				}
			} else {
				if !reflect.DeepEqual(tt.nameTable[0].Resource, nametable[0].Resource) {
					t.Errorf("unexpected nametable. want: %v, got: %v", tt.nameTable, nametable)
				}
			}
		})
	}
}

// TestNDSDeltaFilteredGateway documents Delta NDS on a gateway with PILOT_FILTER_GATEWAY_CLUSTER_CONFIG. The gateway
// still gets every exported hostname on a full push, but updates to services that its gateways don't reference are not
// pushed, as for SotW. Those hostnames stay stale until a full push (SotW catches up on any push), which doesn't
// affect traffic: the gateway has no clusters for them. Waypoints are not filtered.
func TestNDSDeltaFilteredGateway(t *testing.T) {
	test.SetForTest(t, &features.FilterGatewayClusterConfig, true)
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gw
  namespace: istio-system
spec:
  selector:
    istio: egressgateway
  servers:
  - port: {number: 80, name: http, protocol: HTTP}
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: vs
  namespace: istio-system
spec:
  hosts: ["*"]
  gateways: ["gw"]
  http:
  - route:
    - destination: {host: attached.example.com}
`})
	addNDSService(s, "attached.example.com", "10.0.0.1")
	addNDSService(s, "other.example.com", "10.0.0.2")
	s.EnsureSynced(t)
	gatewayMetadata := deltaNDSMetadata
	gatewayMetadata.Labels = map[string]string{"istio": "egressgateway"}
	gatewayMetadata.Namespace = "istio-system"
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).
		WithID("router~10.0.0.1~egressgateway.istio-system~istio-system.svc.cluster.local").
		WithMetadata(gatewayMetadata)
	// The full push includes the unreferenced service.
	want := map[string][]string{"attached.example.com": {"10.0.0.1"}, "other.example.com": {"10.0.0.2"}}
	if diff := cmp.Diff(want, deltaNDSIPs(t, ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{}))); diff != "" {
		t.Fatalf("unexpected full push (-want +got):\n%s", diff)
	}

	// Updates to unreferenced services are not pushed.
	addNDSService(s, "other.example.com", "10.0.0.20")
	addNDSService(s, "new.example.com", "10.0.0.30")
	ads.ExpectNoResponse()

	// A referenced service update only sends its hostname, so other.example.com keeps its old address.
	addNDSService(s, "attached.example.com", "10.0.0.10")
	response := ads.ExpectResponse()
	if diff := cmp.Diff(map[string][]string{"attached.example.com": {"10.0.0.10"}}, deltaNDSIPs(t, response)); diff != "" {
		t.Fatalf("unexpected partial push (-want +got):\n%s", diff)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})
}

// TestNDSDeltaTrailingDeleteKey covers a delete key pushed after the scope already dropped the service, as when the
// keys of a host rename are split across pushes.
func TestNDSDeltaTrailingDeleteKey(t *testing.T) {
	t.Run("renamed host is removed", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addNDSService(s, "new.example.com", "10.0.0.1")
		s.EnsureSynced(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
		ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		// The client still has old.example.com after the push of new.example.com.
		proxy := s.Discovery.Clients()[0].Proxy()
		proxy.Lock()
		proxy.WatchedResources[v3.NameTableType].ResourceNames.Insert("old.example.com")
		proxy.Unlock()

		pushServiceEntryKey(s, "old.example.com", "default")
		response := ads.ExpectResponse()
		if len(response.Resources) != 0 || !cmp.Equal(response.RemovedResources, []string{"old.example.com"}) {
			t.Fatalf("renamed host was not removed: resources=%v removed=%v", response.Resources, response.RemovedResources)
		}
	})

	t.Run("same-hostname service in another namespace is dropped", func(t *testing.T) {
		// Each egress listener imports one of the namespaces, so the name table merges both services.
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{Configs: []config.Config{
			ndsSidecar("default",
				&networking.IstioEgressListener{
					Port:  &networking.SidecarPort{Number: 80, Protocol: "HTTP", Name: "http"},
					Hosts: []string{"a/*"},
				},
				&networking.IstioEgressListener{Hosts: []string{"b/*"}}),
			ndsServiceEntry("se", "a", "240.0.0.1", nil, "h.example.com"),
			ndsServiceEntry("se", "b", "240.0.0.2", nil, "h.example.com"),
		}})
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
		want := map[string][]string{"h.example.com": {"240.0.0.1", "240.0.0.2"}}
		if diff := cmp.Diff(want, deltaNDSIPs(t, ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{}))); diff != "" {
			t.Fatalf("unexpected full push (-want +got):\n%s", diff)
		}

		if err := s.Store().Delete(gvk.ServiceEntry, "se", "b", nil); err != nil {
			t.Fatal(err)
		}
		want = map[string][]string{"h.example.com": {"240.0.0.1"}}
		response := ads.ExpectResponse()
		if diff := cmp.Diff(want, deltaNDSIPs(t, response)); diff != "" {
			t.Fatalf("unexpected delete push (-want +got):\n%s", diff)
		}
		ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})

		// The delete key again, now that neither scope has b's service.
		pushServiceEntryKey(s, "h.example.com", "b")
		if diff := cmp.Diff(want, deltaNDSIPs(t, ads.ExpectResponse())); diff != "" {
			t.Fatalf("trailing delete key did not rebuild the hostname (-want +got):\n%s", diff)
		}
	})
}

// TestNDSDeltaScopeShrink covers hostnames that leave the proxy's scope while their services still exist.
func TestNDSDeltaScopeShrink(t *testing.T) {
	connect := func(t *testing.T) (*xds.FakeDiscoveryServer, *xdspkg.DeltaAdsTest) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{Configs: []config.Config{
			ndsServiceEntry("local", "default", "240.0.0.1", nil, "local.example.com"),
			ndsServiceEntry("remote", "other", "240.0.0.2", nil, "remote.example.com"),
		}})
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(deltaNDSMetadata)
		want := map[string][]string{"local.example.com": {"240.0.0.1"}, "remote.example.com": {"240.0.0.2"}}
		if diff := cmp.Diff(want, deltaNDSIPs(t, ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{}))); diff != "" {
			t.Fatalf("unexpected full push (-want +got):\n%s", diff)
		}
		return s, ads
	}

	t.Run("exportTo narrowed", func(t *testing.T) {
		s, ads := connect(t)
		narrowed := ndsServiceEntry("remote", "other", "240.0.0.2", []string{"."}, "remote.example.com")
		if _, err := s.Store().Update(narrowed); err != nil {
			t.Fatal(err)
		}
		response := ads.ExpectResponse()
		if len(response.Resources) != 0 || !cmp.Equal(response.RemovedResources, []string{"remote.example.com"}) {
			t.Fatalf("unexported host was not removed: resources=%v removed=%v", response.Resources, response.RemovedResources)
		}
	})

	t.Run("Sidecar applied", func(t *testing.T) {
		s, ads := connect(t)
		sidecar := ndsSidecar("default", &networking.IstioEgressListener{Hosts: []string{"./*"}})
		if _, err := s.Store().Create(sidecar); err != nil {
			t.Fatal(err)
		}
		response := ads.ExpectResponse()
		if diff := cmp.Diff(map[string][]string{"local.example.com": {"240.0.0.1"}}, deltaNDSIPs(t, response)); diff != "" {
			t.Fatalf("unexpected full push (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"remote.example.com"}, response.RemovedResources); diff != "" {
			t.Fatalf("host outside the Sidecar was not removed (-want +got):\n%s", diff)
		}
	})
}

// TestNDSDeltaMatchesSotW checks that the delta client's tables converge to the SotW table after each random
// ServiceEntry change.
func TestNDSDeltaMatchesSotW(t *testing.T) {
	for seed := int64(1); seed <= 3; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			// nolint: gosec // deterministic seed keeps failures reproducible
			testNDSDeltaMatchesSotW(t, rand.New(rand.NewSource(seed)))
		})
	}
}

func testNDSDeltaMatchesSotW(t *testing.T, r *rand.Rand) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{Configs: []config.Config{
		ndsSidecar("app",
			&networking.IstioEgressListener{
				Port:  &networking.SidecarPort{Number: 80, Protocol: "HTTP", Name: "http"},
				Hosts: []string{"ns1/*"},
			},
			&networking.IstioEgressListener{
				Port:  &networking.SidecarPort{Number: 8080, Protocol: "HTTP", Name: "http-alt"},
				Hosts: []string{"ns2/*"},
			},
			&networking.IstioEgressListener{Hosts: []string{"./*"}}),
		ndsServiceEntry("static", "app", "240.0.0.1", nil, "static.example.com"),
	}})
	metadata := deltaNDSMetadata
	metadata.Namespace = "app"
	metadata.ClusterID = constants.DefaultClusterName
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithID("sidecar~1.1.1.1~client.app~app.svc.cluster.local").
		WithMetadata(metadata)

	state := map[string]*dnsProto.NameTable{}
	apply := func(response *discovery.DeltaDiscoveryResponse) {
		t.Helper()
		for _, resource := range response.Resources {
			state[resource.Name] = ndsTable(t, resource.Resource)
		}
		for _, name := range response.RemovedResources {
			delete(state, name)
		}
	}
	accumulated := func() map[string]*dnsProto.NameTable_NameInfo {
		out := map[string]*dnsProto.NameTable_NameInfo{}
		for _, table := range state {
			for name, info := range table.GetTable() {
				out[name] = info
			}
		}
		return out
	}
	sotw := func() map[string]*dnsProto.NameTable_NameInfo {
		t.Helper()
		sotwMetadata := metadata
		sotwMetadata.DeltaNDS = false
		proxy := s.SetupProxy(&model.Proxy{ConfigNamespace: "app", Metadata: &sotwMetadata})
		resources, _, err := s.Discovery.Generators[v3.NameTableType].Generate(proxy, nil,
			&model.PushRequest{Push: s.PushContext(), Forced: true, Start: time.Now()})
		if err != nil || len(resources) != 1 {
			t.Fatalf("SotW generation failed: resources=%v err=%v", resources, err)
		}
		return ndsTable(t, resources[0].Resource).GetTable()
	}
	// Deletes and other updates are published separately, so a step can take more than one push.
	converge := func(step int) {
		t.Helper()
		var diff string
		defer func() {
			if t.Failed() {
				t.Logf("step %d: delta tables differ from SotW (-want +got):\n%s", step, diff)
			}
		}()
		for {
			s.EnsureSynced(t)
			diff = cmp.Diff(sotw(), accumulated(), protocmp.Transform(),
				protocmp.SortRepeated(func(a, b string) bool { return a < b }))
			if diff == "" {
				return
			}
			response := ads.ExpectResponse()
			apply(response)
			ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})
		}
	}
	apply(ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{}))
	converge(0)

	type entry struct{ name, namespace string }
	var entries []entry
	namespaces := []string{"app", "ns1", "ns2", "ns3"}
	exportTos := [][]string{nil, {"."}, {"*"}, {"app"}, {"ns1"}}
	randomServiceEntry := func(e entry, step int) config.Config {
		hosts := sets.New(fmt.Sprintf("h%d.example.com", r.Intn(5)), fmt.Sprintf("h%d.example.com", r.Intn(5)))
		return ndsServiceEntry(e.name, e.namespace, fmt.Sprintf("240.1.%d.%d", step/250, step%250+1),
			exportTos[r.Intn(len(exportTos))], sets.SortedList(hosts)...)
	}
	for step := 1; step <= 50; step++ {
		var err error
		switch op := r.Intn(3); {
		case op == 0 || len(entries) == 0:
			e := entry{fmt.Sprintf("se-%d", step), namespaces[r.Intn(len(namespaces))]}
			entries = append(entries, e)
			_, err = s.Store().Create(randomServiceEntry(e, step))
		case op == 1:
			_, err = s.Store().Update(randomServiceEntry(entries[r.Intn(len(entries))], step))
		default:
			i := r.Intn(len(entries))
			err = s.Store().Delete(gvk.ServiceEntry, entries[i].name, entries[i].namespace, nil)
			entries = slices.Delete(entries, i)
		}
		if err != nil {
			t.Fatal(err)
		}
		converge(step)
	}
}
