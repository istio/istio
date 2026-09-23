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
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	dnsutil "istio.io/istio/pkg/dns"
	dnsProto "istio.io/istio/pkg/dns/proto"
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
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture:   true,
			DeltaNDS:     true,
			IstioVersion: "1.32.0",
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) == 0 {
			t.Fatal("expected named NDS resources")
		}
		fullSnapshot := false
		for _, resource := range response.Resources {
			if resource.Name == "" {
				t.Fatal("delta-capable agent received an unnamed full-table resource")
			}
			var table dnsProto.NameTable
			if err := resource.Resource.UnmarshalTo(&table); err != nil {
				t.Fatal(err)
			}
			if resource.Name == dnsutil.FullSnapshotResourceName {
				fullSnapshot = true
				if table.GetNameInfo() != nil || len(table.GetTable()) != 0 {
					t.Fatalf("full-snapshot marker contains DNS entries: %v", &table)
				}
				continue
			}
			if table.GetNameInfo() == nil || len(table.GetTable()) != 0 {
				t.Fatalf("resource %q does not use the single-name NDS representation: %v", resource.Name, &table)
			}
		}
		if !fullSnapshot {
			t.Fatal("initial named response omitted the full-snapshot marker")
		}
	})

	t.Run("configured legacy agent receives unnamed full table", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture:   true,
			DeltaNDS:     true,
			IstioVersion: "1.31.9",
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) != 1 || response.Resources[0].Name != "" {
			t.Fatalf("older agent selected named resources: %v", response.Resources)
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

	t.Run("configured agent without version receives unnamed full table", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture: true,
			DeltaNDS:   true,
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) != 1 || response.Resources[0].Name != "" {
			t.Fatalf("agent without a version selected named resources: %v", response.Resources)
		}
	})

	t.Run("legacy agent receives unnamed full table", func(t *testing.T) {
		s := newServer(t)
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
			DNSCapture:   true,
			IstioVersion: "1.32.0",
		})
		response := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
		if len(response.Resources) != 1 || response.Resources[0].Name != "" {
			t.Fatalf("expected one unnamed compatibility resource, got %v", response.Resources)
		}
		var table dnsProto.NameTable
		if err := response.Resources[0].Resource.UnmarshalTo(&table); err != nil {
			t.Fatal(err)
		}
		if len(table.GetTable()) == 0 || table.GetNameInfo() != nil {
			t.Fatalf("legacy response does not contain a full table: %v", &table)
		}
	})
}

func TestNDSDeltaStatePersistsAcrossPushes(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addService := func(name, address string) {
		s.MemRegistry.AddService(&model.Service{
			Hostname:       host.Name(name),
			DefaultAddress: address,
			Attributes:     model.ServiceAttributes{Namespace: "default"},
		})
	}
	addService("a.example.com", "10.0.0.1")
	s.EnsureSynced(t)
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
		DNSCapture:   true,
		DeltaNDS:     true,
		IstioVersion: "1.32.0",
	})
	assertGeneratorManaged := func() {
		t.Helper()
		clients := s.Discovery.Clients()
		if len(clients) != 1 {
			t.Fatalf("expected one client, got %d", len(clients))
		}
		proxy := clients[0].Proxy()
		proxy.RLock()
		watched, found := proxy.DeepCloneWatchedResourcesLocked()[v3.NameTableType]
		proxy.RUnlock()
		if !found || watched.GeneratorState == nil || len(watched.ResourceNames) != 0 {
			t.Fatalf("named NDS did not manage its own membership: %+v", watched)
		}
	}
	ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})
	assertGeneratorManaged()

	addService("b.example.com", "10.0.0.2")
	response := ads.ExpectResponse()
	if len(response.Resources) != 1 || response.Resources[0].Name != "b.example.com" {
		t.Fatalf("incremental push did not retain the initial stream state: %v", response.Resources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})
	assertGeneratorManaged()

	addService("b.example.com", "10.0.0.3")
	response = ads.ExpectResponse()
	if len(response.Resources) != 1 || response.Resources[0].Name != "b.example.com" {
		t.Fatalf("service update was not incremental: %v", response.Resources)
	}
	var updated dnsProto.NameTable
	if err := response.Resources[0].Resource.UnmarshalTo(&updated); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"10.0.0.3"}, updated.GetNameInfo().GetIps()); diff != "" {
		t.Fatalf("updated resource has unexpected addresses (-want +got):\n%s", diff)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})

	s.MemRegistry.RemoveService("b.example.com")
	response = ads.ExpectResponse()
	if len(response.Resources) != 0 || !cmp.Equal(response.RemovedResources, []string{"b.example.com"}) {
		t.Fatalf("service removal was not incremental: resources=%v removed=%v", response.Resources, response.RemovedResources)
	}
}

func TestNDSDeltaNackLifecycle(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addService := func(name string) {
		s.MemRegistry.AddService(&model.Service{
			Hostname:       host.Name(name),
			DefaultAddress: "10.0.0.1",
			Attributes:     model.ServiceAttributes{Namespace: "default"},
		})
	}
	addService("a.example.com")
	s.EnsureSynced(t)
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
		DNSCapture:   true,
		DeltaNDS:     true,
		IstioVersion: "1.32.0",
	})
	ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

	addService("b.example.com")
	rejected := ads.ExpectResponse()
	if len(rejected.Resources) != 1 || rejected.Resources[0].Name != "b.example.com" {
		t.Fatalf("unexpected rejected response: %v", rejected.Resources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{
		ResponseNonce: rejected.Nonce,
		ErrorDetail:   &status.Status{Message: "rejected NDS response"},
	})
	ads.ExpectNoResponse()

	addService("c.example.com")
	response := ads.ExpectResponse()
	if len(response.Resources) != 1 || response.Resources[0].Name != "c.example.com" {
		t.Fatalf("NACK changed generic delta progression: %v", response.Resources)
	}
	ads.Request(&discovery.DeltaDiscoveryRequest{ResponseNonce: response.Nonce})

	s.Discovery.ConfigUpdate(&model.PushRequest{Forced: true})
	response = ads.ExpectResponse()
	got := sets.New[string]()
	fullSnapshot := false
	for _, resource := range response.Resources {
		if resource.Name == dnsutil.FullSnapshotResourceName {
			fullSnapshot = true
			continue
		}
		got.Insert(resource.Name)
	}
	if !fullSnapshot {
		t.Fatal("forced reconciliation omitted the full-snapshot marker")
	}
	want := sets.New("a.example.com", "b.example.com", "c.example.com")
	if !got.Equals(want) {
		t.Fatalf("forced reconciliation after NACK returned %v, want %v", got, want)
	}
}

func TestNDSInitialNackContinuesDeltas(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	addService := func(name string) {
		s.MemRegistry.AddService(&model.Service{
			Hostname:       host.Name(name),
			DefaultAddress: "10.0.0.1",
			Attributes:     model.ServiceAttributes{Namespace: "default"},
		})
	}
	addService("a.example.com")
	s.EnsureSynced(t)
	metadata := model.NodeMetadata{
		DNSCapture:   true,
		DeltaNDS:     true,
		IstioVersion: "1.32.0",
	}
	rejectedClient := s.ConnectDeltaADS().WithID("sidecar~1.1.1.1~rejected.default~default.svc.cluster.local").
		WithType(v3.NameTableType).WithMetadata(metadata)
	healthyClient := s.ConnectDeltaADS().WithID("sidecar~1.1.1.2~healthy.default~default.svc.cluster.local").
		WithType(v3.NameTableType).WithMetadata(metadata)

	rejectedClient.Request(&discovery.DeltaDiscoveryRequest{})
	rejected := rejectedClient.ExpectResponse()
	rejectedClient.Request(&discovery.DeltaDiscoveryRequest{
		ResponseNonce: rejected.Nonce,
		ErrorDetail:   &status.Status{Message: "rejected initial NDS response"},
	})
	rejectedClient.ExpectNoResponse()
	healthyClient.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

	addService("b.example.com")
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
