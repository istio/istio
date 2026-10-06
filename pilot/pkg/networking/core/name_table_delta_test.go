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
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/maps"
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

func deltaTestHeadless() *model.Service {
	svc := deltaTestService("db.default.svc.cluster.local", constants.UnspecifiedIP, provider.Kubernetes)
	svc.Resolution = model.Passthrough
	return svc
}

func deltaTestPod(name, address string) *model.IstioEndpoint {
	return &model.IstioEndpoint{Addresses: []string{address}, HostName: name, SubDomain: "db", HealthStatus: model.Healthy}
}

func deltaTestPush(services ...*model.Service) *model.PushContext {
	push := model.NewPushContext()
	push.Mesh = &meshconfig.MeshConfig{RootNamespace: "istio-system"}
	push.AddPublicServices(services)
	return push
}

func deltaTestProxy(push *model.PushContext) *model.Proxy {
	proxy := &model.Proxy{
		Type:      model.SidecarProxy,
		DNSDomain: "default.svc.cluster.local",
		Metadata: &model.NodeMetadata{
			Namespace: "default",
		},
	}
	proxy.SetSidecarScope(push)
	return proxy
}

func deltaTestRequest(push *model.PushContext, k kind.Kind, hostnames ...string) *model.PushRequest {
	req := &model.PushRequest{Push: push, ConfigsUpdated: sets.New[model.ConfigKey]()}
	for _, hostname := range hostnames {
		req.ConfigsUpdated.Insert(model.ConfigKey{Kind: k, Name: hostname, Namespace: "default"})
	}
	return req
}

func deltaTestWatch(names ...string) *model.WatchedResource {
	return &model.WatchedResource{ResourceNames: sets.New(names...)}
}

func resourceTables(t *testing.T, resources []*discovery.Resource) map[string]map[string]*dnsProto.NameTable_NameInfo {
	t.Helper()
	out := make(map[string]map[string]*dnsProto.NameTable_NameInfo, len(resources))
	for _, resource := range resources {
		var table dnsProto.NameTable
		if err := resource.Resource.UnmarshalTo(&table); err != nil {
			t.Fatal(err)
		}
		out[resource.Name] = table.Table
	}
	return out
}

func buildDelta(proxy *model.Proxy, req *model.PushRequest, watched *model.WatchedResource) ([]*discovery.Resource, []string, bool) {
	resources, removed, _, usedDelta := (&ConfigGeneratorImpl{}).BuildDeltaNameTable(proxy, req, watched)
	return resources, removed, usedDelta
}

func TestBuildDeltaNameTableHandlesNilScope(t *testing.T) {
	push := deltaTestPush()
	proxy := deltaTestProxy(push)
	proxy.SidecarScope = nil
	resources, removed, usedDelta := buildDelta(proxy, &model.PushRequest{Push: push, Forced: true}, deltaTestWatch())
	if usedDelta || len(removed) != 0 || len(resources) != 0 {
		t.Fatalf("unexpected nil-scope response: usedDelta=%v removed=%v resources=%v", usedDelta, removed, resources)
	}
}

func TestBuildDeltaNameTableFullPushGroupsByHostname(t *testing.T) {
	kube := deltaTestService("reviews.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	external := deltaTestService("example.com", "192.0.2.1", provider.External)
	headless := deltaTestHeadless()
	push := deltaTestPush(kube, external, headless)
	push.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{
		80: {deltaTestPod("db-0", "10.0.0.2"), deltaTestPod("db-1", "10.0.0.3")},
	})

	resources, removed, usedDelta := buildDelta(deltaTestProxy(push), &model.PushRequest{Push: push, Forced: true}, deltaTestWatch())
	if usedDelta || len(removed) != 0 {
		t.Fatalf("full push must leave removals to the xDS server: usedDelta=%v removed=%v", usedDelta, removed)
	}
	want := []string{"db.default.svc.cluster.local", "example.com", "reviews.default.svc.cluster.local"}
	if diff := cmp.Diff(want, slices.Map(resources, func(r *discovery.Resource) string { return r.Name })); diff != "" {
		t.Fatalf("unexpected resource names (-want +got):\n%s", diff)
	}
	got := resourceTables(t, resources)
	wantHeadless := []string{
		"db-0.db.default.svc.cluster.local",
		"db-1.db.default.svc.cluster.local",
		"db.default.svc.cluster.local",
	}
	if diff := cmp.Diff(wantHeadless, slices.Sort(maps.Keys(got[headless.Hostname.String()]))); diff != "" {
		t.Fatalf("headless resource must carry its per-pod names (-want +got):\n%s", diff)
	}
	aliases := func(resource, name string) []string {
		return got[resource][name].GetAliases()
	}
	if diff := cmp.Diff([]string{"reviews", "reviews.default", "reviews.default.svc"},
		aliases(kube.Hostname.String(), kube.Hostname.String())); diff != "" {
		t.Fatalf("unexpected Kubernetes aliases (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"db-0.db", "db-0.db.default", "db-0.db.default.svc"},
		aliases(headless.Hostname.String(), "db-0.db.default.svc.cluster.local")); diff != "" {
		t.Fatalf("unexpected headless pod aliases (-want +got):\n%s", diff)
	}
	if got := aliases(external.Hostname.String(), external.Hostname.String()); len(got) != 0 {
		t.Fatalf("non-Kubernetes entry must not have aliases: %v", got)
	}
}

func TestBuildDeltaNameTableAliasesDependOnProxy(t *testing.T) {
	kube := deltaTestService("reviews.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	push := deltaTestPush(kube)
	cases := []struct {
		name      string
		namespace string
		domain    string
		want      []string
	}{
		{name: "same namespace", namespace: "default", domain: "default.svc.cluster.local", want: []string{"reviews", "reviews.default", "reviews.default.svc"}},
		{name: "other namespace", namespace: "other", domain: "other.svc.cluster.local", want: []string{"reviews.default", "reviews.default.svc"}},
		{name: "other cluster domain", namespace: "default", domain: "default.svc.remote.local", want: nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := deltaTestProxy(push)
			proxy.Metadata.Namespace = tt.namespace
			proxy.DNSDomain = tt.domain
			resources, _, _ := buildDelta(proxy, &model.PushRequest{Push: push, Forced: true}, deltaTestWatch())
			got := resourceTables(t, resources)[kube.Hostname.String()][kube.Hostname.String()].GetAliases()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("unexpected aliases (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildDeltaNameTablePrefersKubernetesDecorator(t *testing.T) {
	hostname := "shared.default.svc.cluster.local"
	external := deltaTestService(hostname, "192.0.2.1", provider.External)
	kube := deltaTestService(hostname, "10.0.0.1", provider.Kubernetes)
	for _, services := range [][]*model.Service{{external, kube}, {kube, external}} {
		push := deltaTestPush(services...)
		resources, _, _ := buildDelta(deltaTestProxy(push), &model.PushRequest{Push: push, Forced: true}, deltaTestWatch())
		got := resourceTables(t, resources)[hostname][hostname]
		if got == nil || got.Registry != string(provider.Kubernetes) || !cmp.Equal(got.Ips, []string{"10.0.0.1"}) {
			t.Fatalf("Kubernetes service did not override its decorator: %v", got)
		}
	}
}

func TestBuildDeltaNameTableAddsService(t *testing.T) {
	service := deltaTestService("reviews.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	push := deltaTestPush(service)
	resources, removed, usedDelta := buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.ServiceEntry, service.Hostname.String()), deltaTestWatch())
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("unexpected service-add delta: used=%v removed=%v", usedDelta, removed)
	}
	got := resourceTables(t, resources)
	if len(got) != 1 || got[service.Hostname.String()][service.Hostname.String()] == nil {
		t.Fatalf("added service was not published: %v", got)
	}
}

func TestBuildDeltaNameTableMergesSameHostnameServices(t *testing.T) {
	first := deltaTestService("shared.example.com", "192.0.2.1", provider.External)
	second := deltaTestService("shared.example.com", "192.0.2.2", provider.External)
	push := deltaTestPush(first, second)
	resources, removed, usedDelta := buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.ServiceEntry, first.Hostname.String()), deltaTestWatch(first.Hostname.String()))
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("unexpected same-host delta: used=%v removed=%v", usedDelta, removed)
	}
	got := resourceTables(t, resources)[first.Hostname.String()][first.Hostname.String()]
	if got == nil || !cmp.Equal(got.Ips, []string{"192.0.2.1", "192.0.2.2"}) {
		t.Fatalf("same-host ServiceEntry addresses were not merged: %v", got)
	}
}

func TestBuildDeltaNameTableRemovesDeletedService(t *testing.T) {
	kept := deltaTestService("kept.example.com", "192.0.2.1", provider.External)
	push := deltaTestPush(kept)

	resources, removed, usedDelta := buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.ServiceEntry, "deleted.example.com"), deltaTestWatch("deleted.example.com", kept.Hostname.String()))
	if !usedDelta || len(resources) != 0 {
		t.Fatalf("unexpected delete delta: used=%v resources=%v", usedDelta, resources)
	}
	if diff := cmp.Diff([]string{"deleted.example.com"}, removed); diff != "" {
		t.Fatalf("unexpected removals (-want +got):\n%s", diff)
	}

	// A hostname the client never received is not removed, and nothing is sent.
	resources, removed, usedDelta = buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.ServiceEntry, "unknown.example.com"), deltaTestWatch(kept.Hostname.String()))
	if !usedDelta || resources != nil || removed != nil {
		t.Fatalf("unknown hostname produced a response: used=%v resources=%v removed=%v", usedDelta, resources, removed)
	}
}

func TestBuildDeltaNameTableOnlySendsUpdatedHostnames(t *testing.T) {
	db := deltaTestHeadless()
	// A ServiceEntry whose hostname collides with a headless pod name is a separate resource.
	colliding := deltaTestService("db-0.db.default.svc.cluster.local", "192.0.2.10", provider.External)
	external := deltaTestService("example.com", "192.0.2.1", provider.External)
	child := deltaTestService("pod.example.com", "192.0.2.2", provider.External)
	push := deltaTestPush(db, colliding, external, child)
	push.AddServiceInstances(db, map[int][]*model.IstioEndpoint{80: {deltaTestPod("db-0", "10.0.0.1")}})

	resources, removed, usedDelta := buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.DNSName, db.Hostname.String()),
		deltaTestWatch(db.Hostname.String(), colliding.Hostname.String(), external.Hostname.String(), child.Hostname.String()))
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("unexpected delta result: used=%v removed=%v", usedDelta, removed)
	}
	got := resourceTables(t, resources)
	if diff := cmp.Diff([]string{db.Hostname.String()}, maps.Keys(got)); diff != "" {
		t.Fatalf("unrelated resources were sent (-want +got):\n%s", diff)
	}
	if info := got[db.Hostname.String()]["db-0.db.default.svc.cluster.local"]; info == nil || !cmp.Equal(info.Ips, []string{"10.0.0.1"}) {
		t.Fatalf("headless resource missing pod record: %v", got)
	}
}

func TestBuildDeltaNameTableHeadlessScaleDown(t *testing.T) {
	headless := deltaTestHeadless()
	push := deltaTestPush(headless)
	push.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{80: {deltaTestPod("db-0", "10.0.0.1")}})

	resources, removed, usedDelta := buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.ServiceEntry, headless.Hostname.String()), deltaTestWatch(headless.Hostname.String()))
	if !usedDelta || len(removed) != 0 {
		t.Fatalf("scale-down must replace the hostname resource: used=%v removed=%v", usedDelta, removed)
	}
	want := []string{"db-0.db.default.svc.cluster.local", "db.default.svc.cluster.local"}
	if diff := cmp.Diff(want, slices.Sort(maps.Keys(resourceTables(t, resources)[headless.Hostname.String()]))); diff != "" {
		t.Fatalf("unexpected headless names after scale-down (-want +got):\n%s", diff)
	}
}

func TestBuildDeltaNameTableHeadlessScaleToZero(t *testing.T) {
	headless := deltaTestHeadless()
	push := deltaTestPush(headless)

	resources, removed, usedDelta := buildDelta(deltaTestProxy(push),
		deltaTestRequest(push, kind.DNSName, headless.Hostname.String()), deltaTestWatch(headless.Hostname.String()))
	if !usedDelta || len(resources) != 0 {
		t.Fatalf("unexpected delta: used=%v resources=%v", usedDelta, resources)
	}
	if diff := cmp.Diff([]string{headless.Hostname.String()}, removed); diff != "" {
		t.Fatalf("a headless service without endpoints must be removed (-want +got):\n%s", diff)
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
			headless := deltaTestHeadless()
			headless.Ports[0].Protocol = tt.protocol
			push := deltaTestPush(headless)
			push.AddServiceInstances(headless, map[int][]*model.IstioEndpoint{80: {deltaTestPod("db-0", "10.0.0.2")}})

			req := deltaTestRequest(push, tt.kind, headless.Hostname.String())
			req.Reason = tt.reason
			resources, removed, usedDelta := buildDelta(deltaTestProxy(push), req, deltaTestWatch(headless.Hostname.String()))
			if usedDelta != tt.wantDelta {
				t.Fatalf("used delta = %v, want %v", usedDelta, tt.wantDelta)
			}
			if len(removed) != 0 {
				t.Fatalf("unexpected removals: %v", removed)
			}
			got := resourceTables(t, resources)[headless.Hostname.String()]["db-0.db.default.svc.cluster.local"]
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
			if got := IsHeadlessEndpointOnly(tt.reasons); got != tt.want {
				t.Fatalf("IsHeadlessEndpointOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildDeltaNameTableFullPushTriggers(t *testing.T) {
	a := deltaTestService("a.default.svc.cluster.local", "10.0.0.1", provider.Kubernetes)
	b := deltaTestService("b.default.svc.cluster.local", "10.0.0.2", provider.Kubernetes)
	push := deltaTestPush(a, b)
	tests := []struct {
		name    string
		request *model.PushRequest
		watched *model.WatchedResource
	}{
		{name: "forced", request: &model.PushRequest{Push: push, Forced: true}, watched: deltaTestWatch()},
		{name: "no configs", request: &model.PushRequest{Push: push}, watched: deltaTestWatch()},
		{name: "nil watch", request: deltaTestRequest(push, kind.ServiceEntry, a.Hostname.String())},
		{name: "non delta-aware kind", request: deltaTestRequest(push, kind.VirtualService, a.Hostname.String()), watched: deltaTestWatch()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, removed, usedDelta := buildDelta(deltaTestProxy(push), tt.request, tt.watched)
			if usedDelta || len(removed) != 0 {
				t.Fatalf("expected a full push: used=%v removed=%v", usedDelta, removed)
			}
			if got := resourceTables(t, resources); len(got) != 2 {
				t.Fatalf("full push omitted resources: %v", got)
			}
		})
	}
}

func TestBuildDeltaNameTableRebuildsForLegacyAutoAllocation(t *testing.T) {
	test.SetForTest(t, &features.EnableIPAutoallocate, false)

	survivor := deltaTestService("survivor.example.com", constants.UnspecifiedIP, provider.External)
	survivor.AutoAllocatedIPv4Address = "240.240.0.1"
	push := deltaTestPush(survivor)

	proxy := deltaTestProxy(push)
	proxy.Metadata.DNSCapture = true
	proxy.Metadata.DNSAutoAllocate = true
	proxy.SetIPMode(model.IPv4)

	// Deleting one ServiceEntry can move the legacy-allocated address of another.
	resources, removed, usedDelta := buildDelta(proxy, deltaTestRequest(push, kind.ServiceEntry, "deleted.example.com"),
		deltaTestWatch("deleted.example.com", survivor.Hostname.String()))
	if usedDelta || len(removed) != 0 {
		t.Fatalf("legacy auto-allocation must use a full push: used=%v removed=%v", usedDelta, removed)
	}
	got := resourceTables(t, resources)
	info := got[survivor.Hostname.String()][survivor.Hostname.String()]
	if len(got) != 1 || info == nil || !cmp.Equal(info.Ips, []string{"240.240.0.1"}) {
		t.Fatalf("full push did not include the reassigned address: %v", got)
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
