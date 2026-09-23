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

package core

import (
	"strings"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	dnsutil "istio.io/istio/pkg/dns"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

func deltaTestService(hostname, address string, registry provider.ID) *model.Service {
	return &model.Service{
		Hostname:       host.Name(hostname),
		DefaultAddress: address,
		Ports:          model.PortList{&model.Port{Name: "http", Port: 80, Protocol: protocol.HTTP}},
		Resolution:     model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Name:            stringsBeforeDot(hostname),
			Namespace:       "default",
			ServiceRegistry: registry,
		},
	}
}

func deltaTestPush(services ...*model.Service) *model.PushContext {
	push := model.NewPushContext()
	push.Mesh = &meshconfig.MeshConfig{RootNamespace: "istio-system"}
	push.AddPublicServices(services)
	return push
}

func deltaTestProxy(previous, current *model.PushContext) *model.Proxy {
	proxy := &model.Proxy{
		Type:      model.SidecarProxy,
		DNSDomain: "default.svc.cluster.local",
		Metadata: &model.NodeMetadata{
			Namespace: "default",
		},
	}
	proxy.SetSidecarScope(previous)
	proxy.SetSidecarScope(current)
	return proxy
}

func deltaTestRequest(push *model.PushContext, hostname string) *model.PushRequest {
	return &model.PushRequest{
		Push: push,
		ConfigsUpdated: sets.New(model.ConfigKey{
			Kind: kind.ServiceEntry, Name: hostname, Namespace: "default",
		}),
	}
}

func resourceTable(t *testing.T, resources []*discovery.Resource) map[string]*dnsProto.NameTable_NameInfo {
	t.Helper()
	out := make(map[string]*dnsProto.NameTable_NameInfo, len(resources))
	for _, resource := range resources {
		if resource.Name == dnsutil.FullSnapshotResourceName {
			continue
		}
		var table dnsProto.NameTable
		if err := resource.Resource.UnmarshalTo(&table); err != nil {
			t.Fatal(err)
		}
		out[resource.Name] = table.GetNameInfo()
	}
	return out
}

func initializedDeltaWatch(t *testing.T, push *model.PushContext) *model.WatchedResource {
	t.Helper()
	watched := &model.WatchedResource{ResourceNames: sets.New[string]()}
	_, _, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(push, push),
		&model.PushRequest{Push: push, Forced: true},
		watched,
	)
	if usedDelta {
		t.Fatal("initial NDS response unexpectedly used a delta")
	}
	return watched
}

func TestBuildDeltaNameTablePreservesCollidingName(t *testing.T) {
	kube := deltaTestService("reviews.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	external := deltaTestService("reviews", "192.0.2.1", provider.External)
	previous := deltaTestPush(kube, external)
	current := deltaTestPush(kube)
	proxy := deltaTestProxy(previous, current)

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		deltaTestRequest(current, "reviews"),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta {
		t.Fatal("expected incremental NDS generation")
	}
	if len(removed) != 0 {
		t.Fatalf("deleting the exact ServiceEntry must reveal the colliding Kubernetes alias, not remove it: %v", removed)
	}
	got := resourceTable(t, resources)["reviews"]
	if got == nil || !cmp.Equal(got.Ips, []string{"10.0.0.1"}) {
		t.Fatalf("expected the Kubernetes alias to replace the deleted exact name, got %v", got)
	}
}

func TestBuildDeltaNameTableAddsService(t *testing.T) {
	service := deltaTestService("reviews.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	previous := deltaTestPush()
	current := deltaTestPush(service)
	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(previous, current),
		deltaTestRequest(current, service.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("unexpected service-add delta: used=%v removed=%v", usedDelta, removed)
	}
	if got := resourceTable(t, resources)[service.Hostname.String()]; got == nil || !cmp.Equal(got.Ips, []string{"10.0.0.1"}) {
		t.Fatalf("added service was not published: %v", got)
	}
}

func TestBuildDeltaNameTableMergesSameHostnameServices(t *testing.T) {
	first := deltaTestService("shared.example.com", "192.0.2.1", provider.External)
	second := deltaTestService("shared.example.com", "192.0.2.2", provider.External)
	previous := deltaTestPush(first, second)
	updatedFirst := deltaTestService("shared.example.com", "192.0.2.3", provider.External)
	current := deltaTestPush(updatedFirst, second)

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(previous, current),
		deltaTestRequest(current, first.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("unexpected same-host delta: used=%v removed=%v", usedDelta, removed)
	}
	got := resourceTable(t, resources)[first.Hostname.String()]
	if got == nil || !cmp.Equal(got.Ips, []string{"192.0.2.2", "192.0.2.3"}) {
		t.Fatalf("same-host ServiceEntry addresses were not merged: %v", got)
	}
}

func TestBuildDeltaNameTablePrefersKubernetesDecorator(t *testing.T) {
	hostname := "shared.default.svc.cluster.local"
	external := deltaTestService(hostname, "192.0.2.1", provider.External)
	kube := deltaTestService(hostname, "10.0.0.1", provider.Kubernetes)
	for _, services := range [][]*model.Service{{external, kube}, {kube, external}} {
		push := deltaTestPush(services...)
		resources, _, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
			deltaTestProxy(push, push),
			&model.PushRequest{Push: push, Forced: true},
			&model.WatchedResource{},
		)
		if usedDelta {
			t.Fatal("initial response unexpectedly used a delta")
		}
		got := resourceTable(t, resources)[hostname]
		if got == nil || got.Registry != string(provider.Kubernetes) || !cmp.Equal(got.Ips, []string{"10.0.0.1"}) {
			t.Fatalf("Kubernetes service did not override its decorator: %v", got)
		}
	}
}

func TestBuildDeltaNameTableUsesLexicalOwnerForEqualCandidates(t *testing.T) {
	first := deltaTestService("alpha.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	first.Resolution = model.Passthrough
	second := deltaTestService("zulu.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	second.Resolution = model.Passthrough

	push := deltaTestPush(second, first)
	push.AddServiceInstances(first, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.1"}, HostName: "shared-0", SubDomain: "shared", HealthStatus: model.Healthy}},
	})
	push.AddServiceInstances(second, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.2"}, HostName: "shared-0", SubDomain: "shared", HealthStatus: model.Healthy}},
	})

	resources, _, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(push, push),
		&model.PushRequest{Push: push, Forced: true},
		&model.WatchedResource{},
	)
	if usedDelta {
		t.Fatal("initial response unexpectedly used a delta")
	}
	got := resourceTable(t, resources)["shared-0.shared.default.svc.cluster.local"]
	if got == nil || !cmp.Equal(got.Ips, []string{"10.0.0.1"}) {
		t.Fatalf("lexically first owner did not win equal candidate collision: %v", got)
	}
}

func TestBuildDeltaNameTableDoesNotClaimServiceEntrySuffix(t *testing.T) {
	parent := deltaTestService("example.com", "192.0.2.1", provider.External)
	child := deltaTestService("pod.example.com", "192.0.2.2", provider.External)
	previous := deltaTestPush(parent, child)
	current := deltaTestPush(parent, child)
	proxy := deltaTestProxy(previous, current)

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		deltaTestRequest(current, "example.com"),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta {
		t.Fatal("expected incremental NDS generation")
	}
	if len(removed) != 0 {
		t.Fatalf("an unrelated ServiceEntry sharing a DNS suffix was removed: %v", removed)
	}
	if got := resourceTable(t, resources); len(got) != 0 {
		t.Fatalf("unchanged ServiceEntry was resent: %v", got)
	}
}

func TestBuildDeltaNameTableRemovesHeadlessPodNames(t *testing.T) {
	headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	headless.Resolution = model.Passthrough
	headless.Attributes.Name = "db"
	previous := deltaTestPush(headless)
	previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
		80: {
			{Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy},
			{Addresses: []string{"10.0.0.2"}, HostName: "db-1", SubDomain: "db", HealthStatus: model.Healthy},
		},
	})
	current := deltaTestPush()
	proxy := deltaTestProxy(previous, current)

	_, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		deltaTestRequest(current, headless.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta {
		t.Fatal("expected incremental NDS generation")
	}
	want := []string{
		"db",
		"db-0.db",
		"db-0.db.default",
		"db-0.db.default.svc",
		"db-0.db.default.svc.cluster.local",
		"db-1.db",
		"db-1.db.default",
		"db-1.db.default.svc",
		"db-1.db.default.svc.cluster.local",
		"db.default",
		"db.default.svc",
		"db.default.svc.cluster.local",
	}
	if diff := cmp.Diff(want, removed); diff != "" {
		t.Fatalf("unexpected headless removals (-want +got):\n%s", diff)
	}
}

func TestBuildDeltaNameTableRemovesHeadlessPodAliasesAfterOwnerChange(t *testing.T) {
	headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	headless.Resolution = model.Passthrough
	headless.Attributes.Name = "db"
	previous := deltaTestPush(headless)
	previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
	})

	replacement := deltaTestService(headless.Hostname.String(), "192.0.2.10", provider.External)
	replacement.Attributes.Name = "external-db"
	replacement.Attributes.Namespace = "other"
	current := deltaTestPush(replacement)
	proxy := deltaTestProxy(previous, current)

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		deltaTestRequest(current, headless.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta {
		t.Fatal("expected incremental NDS generation")
	}
	wantRemoved := []string{
		"db",
		"db-0.db",
		"db-0.db.default",
		"db-0.db.default.svc",
		"db-0.db.default.svc.cluster.local",
		"db.default",
		"db.default.svc",
	}
	if diff := cmp.Diff(wantRemoved, removed); diff != "" {
		t.Fatalf("unexpected headless alias removals (-want +got):\n%s", diff)
	}
	got := resourceTable(t, resources)[headless.Hostname.String()]
	if got == nil || !cmp.Equal(got.Ips, []string{"192.0.2.10"}) {
		t.Fatalf("replacement owner was not published: %v", got)
	}
}

func TestBuildDeltaNameTablePreservesServiceEntryCollidingWithHeadlessPod(t *testing.T) {
	headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	headless.Resolution = model.Passthrough
	headless.Attributes.Name = "db"
	collidingName := "db-0.db.default.svc.cluster.local"
	external := deltaTestService(collidingName, "192.0.2.10", provider.External)

	previous := deltaTestPush(headless, external)
	previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
	})
	current := deltaTestPush(headless, external)
	proxy := deltaTestProxy(previous, current)

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		deltaTestRequest(current, headless.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta {
		t.Fatal("expected incremental NDS generation")
	}
	if slices.Contains(removed, collidingName) {
		t.Fatalf("headless update removed an independently owned exact name: %v", removed)
	}
	got := resourceTable(t, resources)[collidingName]
	if got == nil || !cmp.Equal(got.Ips, []string{"192.0.2.10"}) {
		t.Fatalf("expected the colliding ServiceEntry to be revealed, got %v", got)
	}
}

func TestBuildDeltaNameTableHandlesHeadlessEndpointUpdateWithoutPreviousScope(t *testing.T) {
	for _, update := range []struct {
		name   string
		kind   kind.Kind
		reason model.ReasonStats
	}{
		{name: "DNSName", kind: kind.DNSName},
		{name: "ServiceEntry", kind: kind.ServiceEntry, reason: model.NewReasonStats(model.HeadlessEndpointUpdate)},
	} {
		t.Run(update.name, func(t *testing.T) {
			headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
			headless.Resolution = model.Passthrough
			headless.Attributes.Name = "db"
			previous := deltaTestPush(headless)
			previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
				80: {{Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
			})
			current := deltaTestPush(headless)
			current.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
				80: {{Addresses: []string{"10.0.0.2"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
			})
			proxy := &model.Proxy{
				Type:      model.SidecarProxy,
				DNSDomain: "default.svc.cluster.local",
				Metadata:  &model.NodeMetadata{Namespace: "default"},
			}
			proxy.SetSidecarScope(current)

			resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
				proxy,
				&model.PushRequest{
					Push:   current,
					Reason: update.reason,
					ConfigsUpdated: sets.New(model.ConfigKey{
						Kind: update.kind, Name: headless.Hostname.String(), Namespace: "default",
					}),
				},
				initializedDeltaWatch(t, previous),
			)
			if !usedDelta {
				t.Fatal("expected a headless endpoint update to use incremental NDS")
			}
			if len(removed) != 0 {
				t.Fatalf("unexpected removals: %v", removed)
			}
			got := resourceTable(t, resources)["db-0.db.default.svc.cluster.local"]
			if got == nil || !cmp.Equal(got.Ips, []string{"10.0.0.2"}) {
				t.Fatalf("expected the changed headless pod record, got %v", got)
			}
		})
	}
}

func TestBuildDeltaNameTableLegacyAllocatorHeadlessEndpointUpdates(t *testing.T) {
	test.SetForTest(t, &features.EnableIPAutoallocate, false)

	tests := []struct {
		name      string
		protocol  protocol.Instance
		kind      kind.Kind
		reason    model.ReasonStats
		wantDelta bool
	}{
		{
			name:      "HTTP",
			protocol:  protocol.HTTP,
			kind:      kind.DNSName,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate),
			wantDelta: true,
		},
		{
			name:      "TCP",
			protocol:  protocol.TCP,
			kind:      kind.ServiceEntry,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate),
			wantDelta: true,
		},
		{
			name:      "coalesced TCP endpoint updates",
			protocol:  protocol.TCP,
			kind:      kind.ServiceEntry,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate, model.EndpointUpdate),
			wantDelta: true,
		},
		{
			name:      "mixed TCP update",
			protocol:  protocol.TCP,
			kind:      kind.ServiceEntry,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate, model.ConfigUpdate),
			wantDelta: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
			headless.Resolution = model.Passthrough
			headless.Ports[0].Protocol = tt.protocol
			previous := deltaTestPush(headless)
			previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
				80: {{Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
			})
			current := deltaTestPush(headless)
			current.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
				80: {{Addresses: []string{"10.0.0.2"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
			})

			resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
				deltaTestProxy(previous, current),
				&model.PushRequest{
					Push:   current,
					Reason: tt.reason,
					ConfigsUpdated: sets.New(model.ConfigKey{
						Kind: tt.kind, Name: headless.Hostname.String(), Namespace: "default",
					}),
				},
				initializedDeltaWatch(t, previous),
			)
			if usedDelta != tt.wantDelta {
				t.Fatalf("used delta = %v, want %v", usedDelta, tt.wantDelta)
			}
			if len(removed) != 0 {
				t.Fatalf("unexpected removals: %v", removed)
			}
			got := resourceTable(t, resources)["db-0.db.default.svc.cluster.local"]
			if got == nil || !cmp.Equal(got.Ips, []string{"10.0.0.2"}) {
				t.Fatalf("expected the changed headless pod record, got %v", got)
			}
		})
	}
}

func TestIsHeadlessEndpointOnly(t *testing.T) {
	tests := []struct {
		name    string
		reasons model.ReasonStats
		want    bool
	}{
		{name: "headless", reasons: model.NewReasonStats(model.HeadlessEndpointUpdate), want: true},
		{name: "coalesced endpoint", reasons: model.NewReasonStats(model.HeadlessEndpointUpdate, model.EndpointUpdate), want: true},
		{name: "config change", reasons: model.NewReasonStats(model.HeadlessEndpointUpdate, model.ConfigUpdate)},
		{name: "service change", reasons: model.NewReasonStats(model.HeadlessEndpointUpdate, model.ServiceUpdate)},
		{name: "ordinary endpoint", reasons: model.NewReasonStats(model.EndpointUpdate)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHeadlessEndpointOnly(tt.reasons); got != tt.want {
				t.Fatalf("isHeadlessEndpointOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildDeltaNameTableDoesNotRebuildUnrelatedHeadlessServices(t *testing.T) {
	db := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	db.Resolution = model.Passthrough
	cache := deltaTestService("cache.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	cache.Resolution = model.Passthrough
	external := deltaTestService("example.com", "192.0.2.1", provider.External)

	previous := deltaTestPush(db, cache, external)
	previous.AddServiceInstances(db, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
	})
	previous.AddServiceInstances(cache, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.2"}, HostName: "cache-0", SubDomain: "cache", HealthStatus: model.Healthy}},
	})
	current := deltaTestPush(db, cache, external)
	current.AddServiceInstances(db, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.3"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy}},
	})
	current.AddServiceInstances(cache, map[int][]*model.IstioEndpoint{
		80: {{Addresses: []string{"10.0.0.2"}, HostName: "cache-0", SubDomain: "cache", HealthStatus: model.Healthy}},
	})

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(previous, current),
		&model.PushRequest{Push: current, ConfigsUpdated: sets.New(model.ConfigKey{
			Kind: kind.DNSName, Name: db.Hostname.String(), Namespace: "default",
		})},
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("unexpected delta result: used=%v removed=%v", usedDelta, removed)
	}
	for name := range resourceTable(t, resources) {
		if strings.HasPrefix(name, "cache") || name == "example.com" {
			t.Fatalf("unrelated DNS resource was resent: %s", name)
		}
	}
}

func TestBuildDeltaNameTableIgnoresEndpointOrdering(t *testing.T) {
	headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	headless.Resolution = model.Passthrough
	first := &model.IstioEndpoint{Addresses: []string{"10.0.0.1"}, HealthStatus: model.Healthy}
	second := &model.IstioEndpoint{Addresses: []string{"10.0.0.2"}, HealthStatus: model.Healthy}
	previous := deltaTestPush(headless)
	previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{80: {first, second}})
	current := deltaTestPush(headless)
	current.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{80: {second, first}})

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(previous, current),
		deltaTestRequest(current, headless.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta || len(resources) != 0 || len(removed) != 0 {
		t.Fatalf("endpoint ordering produced a delta: resources=%v removed=%v used=%v", resources, removed, usedDelta)
	}
}

func TestBuildDeltaNameTableHeadlessScaleDown(t *testing.T) {
	headless := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	headless.Resolution = model.Passthrough
	first := &model.IstioEndpoint{
		Addresses: []string{"10.0.0.1"}, HostName: "db-0", SubDomain: "db", HealthStatus: model.Healthy,
	}
	second := &model.IstioEndpoint{
		Addresses: []string{"10.0.0.2"}, HostName: "db-1", SubDomain: "db", HealthStatus: model.Healthy,
	}
	previous := deltaTestPush(headless)
	previous.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{80: {first, second}})
	current := deltaTestPush(headless)
	current.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{80: {first}})

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(previous, current),
		deltaTestRequest(current, headless.Hostname.String()),
		initializedDeltaWatch(t, previous),
	)
	if !usedDelta {
		t.Fatal("expected headless scale-down to use a delta")
	}
	wantRemoved := []string{
		"db-1.db",
		"db-1.db.default",
		"db-1.db.default.svc",
		"db-1.db.default.svc.cluster.local",
	}
	if diff := cmp.Diff(wantRemoved, removed); diff != "" {
		t.Fatalf("unexpected scale-down removals (-want +got):\n%s", diff)
	}
	for name := range resourceTable(t, resources) {
		if strings.HasPrefix(name, "db-0.db") {
			t.Fatalf("unchanged pod record was resent: %s", name)
		}
	}
}

func TestBuildDeltaNameTableRebuildsForForcedPush(t *testing.T) {
	a := deltaTestService("a.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	b := deltaTestService("b.default.svc.cluster.local", "10.0.0.2", provider.Kubernetes)
	push := deltaTestPush(a, b)
	proxy := deltaTestProxy(push, push)

	resources, _, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		&model.PushRequest{Push: push, Forced: true},
		&model.WatchedResource{
			ResourceNames: sets.New(a.Hostname.String(), b.Hostname.String()),
		},
	)
	if usedDelta {
		t.Fatal("forced push must trigger a complete reconciliation")
	}
	if !slices.ContainsFunc(resources, func(resource *discovery.Resource) bool {
		return resource.Name == dnsutil.FullSnapshotResourceName
	}) {
		t.Fatal("forced push omitted the full-snapshot marker")
	}
	got := resourceTable(t, resources)
	if got[a.Hostname.String()] == nil || got[b.Hostname.String()] == nil {
		t.Fatalf("complete reconciliation omitted resources: %v", got)
	}
}

func TestBuildDeltaNameTableForcedPushRemovesStaleNames(t *testing.T) {
	a := deltaTestService("a.example.com", "192.0.2.1", provider.External)
	b := deltaTestService("b.example.com", "192.0.2.2", provider.External)
	previous := deltaTestPush(a, b)
	current := deltaTestPush(a)

	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		deltaTestProxy(previous, current),
		&model.PushRequest{Push: current, Forced: true},
		initializedDeltaWatch(t, previous),
	)
	if usedDelta {
		t.Fatal("forced push must remain a full reconciliation")
	}
	if diff := cmp.Diff([]string{"b.example.com"}, removed); diff != "" {
		t.Fatalf("unexpected forced-push removals (-want +got):\n%s", diff)
	}
	got := resourceTable(t, resources)
	if got["a.example.com"] == nil || got["b.example.com"] != nil {
		t.Fatalf("unexpected forced-push resources: %v", got)
	}
}

func TestBuildDeltaNameTableRebuildsForLegacyAutoAllocation(t *testing.T) {
	test.SetForTest(t, &features.EnableIPAutoallocate, false)

	survivor := deltaTestService("survivor.example.com", constants.UnspecifiedIP, provider.External)
	survivor.AutoAllocatedIPv4Address = "240.240.0.2"
	deleted := deltaTestService("deleted.example.com", constants.UnspecifiedIP, provider.External)
	deleted.AutoAllocatedIPv4Address = "240.240.0.1"
	previous := deltaTestPush(deleted, survivor)

	currentSurvivor := survivor.ShallowCopy()
	currentSurvivor.AutoAllocatedIPv4Address = "240.240.0.1"
	current := deltaTestPush(currentSurvivor)
	proxy := deltaTestProxy(previous, previous)
	proxy.Metadata.DNSCapture = true
	proxy.Metadata.DNSAutoAllocate = true
	proxy.SetIPMode(model.IPv4)

	watched := &model.WatchedResource{ResourceNames: sets.New[string]()}
	previousResources, _, _, initialUsedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		&model.PushRequest{Push: previous, Forced: true},
		watched,
	)
	if initialUsedDelta || watched.GeneratorState == nil {
		t.Fatal("initial generation did not retain a complete previous state")
	}
	previousTable := resourceTable(t, previousResources)
	if info := previousTable[survivor.Hostname.String()]; len(previousTable) != 2 || info == nil ||
		!cmp.Equal(info.Ips, []string{"240.240.0.2"}) {
		t.Fatalf("initial generation did not use the previous service scope: %v", previousTable)
	}
	proxy.SetSidecarScope(current)
	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(
		proxy,
		deltaTestRequest(current, deleted.Hostname.String()),
		watched,
	)
	if usedDelta {
		t.Fatal("legacy auto-allocation must use a complete reconciliation")
	}
	if diff := cmp.Diff([]string{deleted.Hostname.String()}, removed); diff != "" {
		t.Fatalf("unexpected removed resources (-want +got):\n%s", diff)
	}
	got := resourceTable(t, resources)
	info := got[survivor.Hostname.String()]
	if len(got) != 1 || info == nil || !cmp.Equal(info.Ips, []string{"240.240.0.1"}) {
		t.Fatalf("complete reconciliation did not include the reassigned address: %v", got)
	}
}

func stringsBeforeDot(value string) string {
	for i, c := range value {
		if c == '.' {
			return value[:i]
		}
	}
	return value
}
