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
	"maps"
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	pkgxds "istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
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

// ndsTableFromDeltaResponse merges a set of per-host delta resources into a single NameTable,
// applying removals. This mirrors what the agent accumulates client-side.
func ndsTableFromDeltaResponse(t *testing.T, resp *discovery.DeltaDiscoveryResponse) *dnsProto.NameTable {
	t.Helper()
	nt := &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{}}
	for _, r := range resp.Resources {
		var entry dnsProto.NameTable
		if err := r.Resource.UnmarshalTo(&entry); err != nil {
			t.Fatalf("failed to unmarshal NDS resource %q: %v", r.Name, err)
		}
		for k, v := range entry.Table {
			nt.Table[k] = v
		}
	}
	for _, name := range resp.RemovedResources {
		delete(nt.Table, name)
	}
	return nt
}

// connectDeltaNDS sets up a delta ADS connection subscribed to NDS with DeltaNDS capability set.
func connectDeltaNDS(t *testing.T, s *xds.FakeDiscoveryServer) *pkgxds.DeltaAdsTest {
	t.Helper()
	ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{
		DNSCapture: true,
		DeltaNDS:   true,
	})
	return ads
}

// addClusterIPService registers a ClusterIP service in the mem registry and fires a push.
func addClusterIPService(s *xds.FakeDiscoveryServer, name, ns, ip string) {
	h := host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", name, ns))
	s.MemRegistry.AddService(&model.Service{
		Hostname:       h,
		DefaultAddress: ip,
		Ports:          []*model.Port{{Name: "http", Port: 80, Protocol: protocol.HTTP}},
		Attributes: model.ServiceAttributes{
			Namespace: ns,
			Name:      name,
		},
	})
}

// removeService signals a service deletion push for the given hostname.
func removeService(s *xds.FakeDiscoveryServer, name, ns string) {
	h := host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", name, ns))
	s.MemRegistry.RemoveService(h)
}

// addHeadlessService registers a headless (Passthrough) service and fires a push.
func addHeadlessService(s *xds.FakeDiscoveryServer, name, ns string) {
	h := host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", name, ns))
	s.MemRegistry.AddService(&model.Service{
		Hostname:       h,
		DefaultAddress: constants.UnspecifiedIP,
		Resolution:     model.Passthrough,
		Ports:          []*model.Port{{Name: "http", Port: 80, Protocol: protocol.HTTP}},
		Attributes:     model.ServiceAttributes{Namespace: ns, Name: name},
	})
}

// addHeadlessPod adds a pod endpoint to a headless service and fires a push. The pod gets a
// per-pod DNS entry <podName>.<svcName>.<ns>.svc.cluster.local.
func addHeadlessPod(s *xds.FakeDiscoveryServer, svcName, ns, podName, ip string) {
	h := host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", svcName, ns))
	s.MemRegistry.AddInstance(&model.ServiceInstance{
		Service: &model.Service{Hostname: h},
		Endpoint: &model.IstioEndpoint{
			Addresses:       []string{ip},
			ServicePortName: "http",
			EndpointPort:    80,
			HostName:        podName,
			SubDomain:       svcName,
			HealthStatus:    model.Healthy,
		},
		ServicePort: &model.Port{Name: "http", Port: 80, Protocol: protocol.HTTP},
	})
	s.Discovery.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DNSName, Name: string(h), Namespace: ns}),
	})
}

// removeAllHeadlessPods clears all endpoints from a headless service. With no endpoints the
// service record itself also disappears from the name table, so RemovedResources will contain
// the service hostname plus all per-pod hostnames that were previously tracked.
func removeAllHeadlessPods(s *xds.FakeDiscoveryServer, svcName, ns string) {
	h := host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", svcName, ns))
	s.MemRegistry.SetEndpoints(string(h), ns, nil)
	s.Discovery.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DNSName, Name: string(h), Namespace: ns}),
	})
}

// perPodHostname returns the expected per-pod DNS name for a headless service pod.
func perPodHostname(podName, svcName, ns string) string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", podName, svcName, ns)
}

func TestNDSDelta(t *testing.T) {
	const ns = "default"

	t.Run("initial full push returns per-host resources including headless per-pod entries", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addClusterIPService(s, "svc-a", ns, "10.0.0.1")
		addHeadlessService(s, "svc-h", ns)
		addHeadlessPod(s, "svc-h", ns, "pod-0", "10.1.0.1")
		addHeadlessPod(s, "svc-h", ns, "pod-1", "10.1.0.2")
		s.EnsureSynced(t)

		ads := connectDeltaNDS(t, s)
		resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

		if len(resp.Resources) == 0 {
			t.Fatal("expected per-host resources, got none")
		}
		if resp.Resources[0].Name == "" {
			t.Fatal("expected named per-host resources, got unnamed full-table resource")
		}
		table := ndsTableFromDeltaResponse(t, resp)
		expectedHosts := []string{
			fmt.Sprintf("svc-a.%s.svc.cluster.local", ns),
			fmt.Sprintf("svc-h.%s.svc.cluster.local", ns),
			perPodHostname("pod-0", "svc-h", ns),
			perPodHostname("pod-1", "svc-h", ns),
		}
		if len(expectedHosts) != len(table.Table) {
			t.Errorf("expected %d entries in push table, got: %d", len(expectedHosts), len(table.Table))
		}
		for _, expected := range expectedHosts {
			if _, ok := table.Table[expected]; !ok {
				t.Errorf("expected %q in initial push table, got: %v", expected, maps.Keys(table.Table))
			}
		}
	})

	t.Run("incapable agent receives unnamed full-table resource", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addClusterIPService(s, "svc-a", ns, "10.0.0.1")
		s.EnsureSynced(t)

		// No DeltaNDS metadata — old agent.
		ads := s.ConnectDeltaADS().WithType(v3.NameTableType).WithMetadata(model.NodeMetadata{DNSCapture: true})
		resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

		if len(resp.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
		}
		if resp.Resources[0].Name != "" {
			t.Errorf("expected unnamed full-table resource, got Name=%q", resp.Resources[0].Name)
		}
	})

	t.Run("service add sends only the new hostname", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addClusterIPService(s, "svc-a", ns, "10.0.0.1")
		addHeadlessService(s, "svc-h", ns)
		addHeadlessPod(s, "svc-h", ns, "pod-0", "10.1.0.1")
		s.EnsureSynced(t)

		ads := connectDeltaNDS(t, s)
		ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{}) // consume initial push

		// Adding a new ClusterIP service should only send that service's hostname.
		addClusterIPService(s, "svc-b", ns, "10.0.0.2")
		resp := ads.ExpectResponse()
		hB := fmt.Sprintf("svc-b.%s.svc.cluster.local", ns)
		if len(resp.Resources) != 1 || resp.Resources[0].Name != hB {
			t.Errorf("expected only %q in delta, got resources: %v", hB, slices.Map(resp.Resources, func(r *discovery.Resource) string { return r.Name }))
		}

		// Adding a pod to the headless service should send the service record and the new per-pod entry.
		addHeadlessPod(s, "svc-h", ns, "pod-1", "10.1.0.2")
		resp = ads.ExpectResponse()
		addedNames := sets.New(slices.Map(resp.Resources, func(r *discovery.Resource) string { return r.Name })...)
		if !addedNames.Contains(perPodHostname("pod-1", "svc-h", ns)) {
			t.Errorf("expected per-pod hostname %q in delta, got: %v", perPodHostname("pod-1", "svc-h", ns), addedNames)
		}
		// svc-a and svc-b must not appear (they were not changed).
		for _, unchanged := range []string{
			fmt.Sprintf("svc-a.%s.svc.cluster.local", ns),
			fmt.Sprintf("svc-b.%s.svc.cluster.local", ns),
		} {
			if addedNames.Contains(unchanged) {
				t.Errorf("%q should not be in an incremental push for svc-h pod", unchanged)
			}
		}
	})

	t.Run("service IP change sends updated entry", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addClusterIPService(s, "svc-a", ns, "10.0.0.1")
		s.EnsureSynced(t)

		ads := connectDeltaNDS(t, s)
		ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

		addClusterIPService(s, "svc-a", ns, "10.0.0.99")
		resp := ads.ExpectResponse()

		table := ndsTableFromDeltaResponse(t, resp)
		if len(resp.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
		}
		h := fmt.Sprintf("svc-a.%s.svc.cluster.local", ns)
		if found, ok := table.Table[h]; ok {
			if len(found.Ips) == 0 || found.Ips[0] != "10.0.0.99" {
				t.Errorf("expected IP 10.0.0.99, got %v", found.Ips)
			}
		} else {
			t.Errorf("expected %q in delta resources, got %v", h, table.Table)
		}
	})

	t.Run("service delete sends RemovedResources including headless per-pod entries", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addClusterIPService(s, "svc-a", ns, "10.0.0.1")
		addClusterIPService(s, "svc-b", ns, "10.0.0.2")
		addHeadlessService(s, "svc-h", ns)
		addHeadlessPod(s, "svc-h", ns, "pod-0", "10.1.0.1")
		addHeadlessPod(s, "svc-h", ns, "pod-1", "10.1.0.2")
		s.EnsureSynced(t)

		ads := connectDeltaNDS(t, s)
		ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

		// ClusterIP service delete: only its hostname should be removed.
		removeService(s, "svc-a", ns)
		resp := ads.ExpectResponse()
		hA := fmt.Sprintf("svc-a.%s.svc.cluster.local", ns)
		if !slices.Contains(resp.RemovedResources, hA) {
			t.Errorf("expected %q in RemovedResources, got %v", hA, resp.RemovedResources)
		}

		// Headless scale-to-zero: removing all pods removes all per-pod entries AND the
		// service record itself (no endpoints → no addresses → no table entry).
		removeAllHeadlessPods(s, "svc-h", ns)
		resp = ads.ExpectResponse()

		removed := sets.New(resp.RemovedResources...)
		for _, expected := range []string{
			fmt.Sprintf("svc-h.%s.svc.cluster.local", ns),
			perPodHostname("pod-0", "svc-h", ns),
			perPodHostname("pod-1", "svc-h", ns),
		} {
			if !removed.Contains(expected) {
				t.Errorf("expected %q in RemovedResources after scale-to-zero, got: %v", expected, resp.RemovedResources)
			}
		}
	})

	t.Run("non-delta-aware push sends full per-host set with server-computed removals", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		addClusterIPService(s, "svc-a", ns, "10.0.0.1")
		addClusterIPService(s, "svc-b", ns, "10.0.0.2")
		s.EnsureSynced(t)

		ads := connectDeltaNDS(t, s)
		ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{})

		s.Discovery.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.MeshConfig}),
			Forced:         true,
		})
		resp := ads.ExpectResponse()

		// All resources should be named per-host (not the unnamed full-table format).
		for _, r := range resp.Resources {
			if r.Name == "" {
				t.Error("non-delta push to capable agent should still produce named per-host resources")
			}
		}

		table := ndsTableFromDeltaResponse(t, resp)
		if len(table.Table) != 2 {
			t.Errorf("expected 2 entries in table, got %d", len(table.Table))
		}
	})
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
