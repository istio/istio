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
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	xdsfake "istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

// TestEdsCachePoisonedByStaleSidecarScope reproduces EDS cache poisoning by a gateway with a stale SidecarScope.
//
// The EDS cache key (EndpointBuilder.WriteHash) contains the DestinationRule name/namespace but not its subsets,
// and is shared by all gateways with the same cluster/network/locality. Endpoint-only pushes reuse the proxy's
// existing SidecarScope, and the cache keeps the first write per push token. So if one gateway still holds an
// old DR version (e.g. the subset label hash from before a canary promotion), its EDS result is cached and
// served to every gateway on the istiod instance, including newly connected ones:
//  1. after the DR subset change, correct gateways still get the old subset's pods;
//  2. once the old pods are removed, they get empty EDS (no healthy upstream);
//  3. new connections get the same empty EDS;
//  4. full pushes for unrelated configs do not repair it, even after the stale scope itself is healed;
//  5. only ClearAll (or the next SE/DR/endpoint change for the service) clears it; there is no TTL.
//
// With PILOT_ENABLE_EDS_DEBOUNCE=false, a gateway can keep the old DR when an un-debounced EDS push that read the
// old push context is enqueued after its DR full push: PushRequest.CopyMerge keeps the later-enqueued (older) push
// context with the full push's Start and ConfigsUpdated, and computeProxyState rebuilds the SidecarScope from it.
func TestEdsCachePoisonedByStaleSidecarScope(t *testing.T) {
	const (
		hostname = "foo.com"
		ns       = "default"
		clusterN = "outbound|80|stable|foo.com"
	)
	makeDR := func(name, host, hash string) config.Config {
		return config.Config{
			Meta: config.Meta{Name: name, Namespace: ns, GroupVersionKind: gvk.DestinationRule},
			Spec: &networking.DestinationRule{
				Host:    host,
				Subsets: []*networking.Subset{{Name: "stable", Labels: map[string]string{"hash": hash}}},
			},
		}
	}
	se := config.Config{
		Meta: config.Meta{Name: "foo", Namespace: ns, GroupVersionKind: gvk.ServiceEntry},
		Spec: &networking.ServiceEntry{
			Hosts:      []string{hostname},
			Ports:      []*networking.ServicePort{{Number: 80, Protocol: "HTTP", Name: "http"}},
			Resolution: networking.ServiceEntry_STATIC,
		},
	}
	ep := func(ip, hash string) *model.IstioEndpoint {
		return &model.IstioEndpoint{
			Addresses:       []string{ip},
			EndpointPort:    8080,
			ServicePortName: "http",
			Labels:          map[string]string{"hash": hash},
			HealthStatus:    model.Healthy,
			Namespace:       ns,
		}
	}
	shard := model.ShardKey{Cluster: cluster.ID("Kubernetes"), Provider: provider.Kubernetes}

	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{Configs: []config.Config{se, makeDR("foo", hostname, "old")}})
	setEndpoints := func(eps ...*model.IstioEndpoint) { s.Discovery.EDSCacheUpdate(shard, hostname, ns, eps) }
	setEndpoints(ep("10.0.0.1", "old"))
	eds := s.Discovery.Generators[v3.EndpointType].(*xds.EdsGenerator)
	w := &model.WatchedResource{TypeUrl: v3.EndpointType, ResourceNames: sets.New(clusterN)}

	gateway := func(id, ip string) *model.Proxy {
		return s.SetupProxy(&model.Proxy{
			Type: model.Router, ID: id + "." + ns, IPAddresses: []string{ip},
			Locality: &core.Locality{Region: "eu-south-1"},
		})
	}
	subsetHash := func(p *model.Proxy) string {
		dr := p.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, p, hostname)
		if dr == nil {
			return ""
		}
		return dr.GetRule().Spec.(*networking.DestinationRule).Subsets[0].Labels["hash"]
	}
	gen := func(p *model.Proxy, req *model.PushRequest) []string {
		t.Helper()
		res, _, err := eds.Generate(p, w, req)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res {
			cla := &endpoint.ClusterLoadAssignment{}
			if err := r.Resource.UnmarshalTo(cla); err != nil {
				t.Fatal(err)
			}
			return xdstest.ExtractEndpoints(cla)
		}
		return nil
	}
	// push mirrors DiscoveryServer.Push + StartPush: drop the cache for the updated configs, then stamp Start.
	push := func(configs ...model.ConfigKey) *model.PushRequest {
		req := &model.PushRequest{Push: s.PushContext(), ConfigsUpdated: sets.New(configs...), Reason: model.NewReasonStats(model.EndpointUpdate)}
		s.Discovery.Cache.Clear(req.ConfigsUpdated)
		req.Start = time.Now()
		return req
	}
	endpointPush := func() *model.PushRequest {
		return push(model.ConfigKey{Kind: kind.Endpoints, Name: hostname, Namespace: ns})
	}
	// connect mirrors a gateway's initial EDS request after connecting: full generation for all watched clusters.
	connect := func(id, ip string) []string {
		return gen(gateway(id, ip), &model.PushRequest{Push: s.PushContext(), Start: time.Now(), Forced: true})
	}
	waitForPushContext := func(cond func() error) {
		retry.UntilSuccessOrFail(t, cond, retry.Timeout(5*time.Second))
	}

	// gw-stale keeps the SidecarScope from the old push context across the DR full push. This test injects that
	// state; with PILOT_ENABLE_EDS_DEBOUNCE=false it happens via the push request merge described above.
	stale := gateway("gw-stale", "1.1.1.1")

	// Canary promotion: the DR's stable subset is rewritten to the new pod hash and the new pods are ready.
	if _, err := s.Store().Update(makeDR("foo", hostname, "new")); err != nil {
		t.Fatal(err)
	}
	waitForPushContext(func() error {
		if got := subsetHash(gateway("probe", "9.9.9.9")); got != "new" {
			return fmt.Errorf("push context not updated yet: %q", got)
		}
		return nil
	})
	if subsetHash(stale) != "old" {
		t.Fatal("setup: gw-stale should still hold the old DR")
	}
	setEndpoints(ep("10.0.0.1", "old"), ep("10.0.0.2", "new"))
	fresh := gateway("gw-fresh", "1.1.1.2")

	// [1] Before the old pods are removed, gw-fresh is served gw-stale's result (old pod) from the cache.
	req := endpointPush()
	staleGot, freshGot := gen(stale, req), gen(fresh, req)
	t.Logf("[1] after DR change + new pods ready: gw-stale=%v gw-fresh=%v (correct: [10.0.0.2:8080])", staleGot, freshGot)
	if len(freshGot) != 1 || freshGot[0] != "10.0.0.1:8080" {
		t.Errorf("[1] expected gw-fresh to be served the old pod from the poisoned cache, got %v", freshGot)
	}

	// [2] Old pods removed: both gateways share the cache key and drop to 0 endpoints.
	setEndpoints(ep("10.0.0.2", "new"))
	req = endpointPush()
	bs, bf := endpoints.NewEndpointBuilder(clusterN, stale, req.Push), endpoints.NewEndpointBuilder(clusterN, fresh, req.Push)
	if bs.Key() != bf.Key() {
		t.Fatal("expected gw-stale and gw-fresh to share the EDS cache key")
	}
	staleGot, freshGot = gen(stale, req), gen(fresh, req)
	t.Logf("[2] after old pods removed: gw-stale=%v gw-fresh=%v", staleGot, freshGot)
	if len(freshGot) != 0 {
		t.Errorf("[2] expected gw-fresh to be served empty EDS, got %v", freshGot)
	}

	// [3] A gateway (re)connecting to this istiod immediately gets 0 endpoints.
	if got := connect("gw-reconnect", "1.1.1.3"); len(got) != 0 {
		t.Errorf("[3] expected newly connected gateway to get empty EDS, got %v", got)
	} else {
		t.Logf("[3] newly connected gateway: %v", got)
	}

	// [4] Unrelated full pushes do not repair it, even though they heal gw-stale's SidecarScope.
	for i := range 30 {
		before := s.PushContext().PushVersion
		if _, err := s.Store().Create(makeDR(fmt.Sprintf("other-%d", i), fmt.Sprintf("other-%d.com", i), "x")); err != nil {
			t.Fatal(err)
		}
		// initPushContext drops the cache before publishing the push context, so once the version changes the
		// background Clear(DR other-i) has already happened.
		waitForPushContext(func() error {
			if s.PushContext().PushVersion == before {
				return fmt.Errorf("push context not updated yet")
			}
			return nil
		})
		push(
			model.ConfigKey{Kind: kind.DestinationRule, Name: fmt.Sprintf("other-%d", i), Namespace: ns},
			model.ConfigKey{Kind: kind.VirtualService, Name: "unrelated", Namespace: ns},
		)
	}
	stale = gateway("gw-stale", "1.1.1.1")
	if subsetHash(stale) != "new" {
		t.Fatal("gw-stale's scope should be healed by the full pushes")
	}
	if got := connect("gw-after-30-pushes", "1.1.1.4"); len(got) != 0 {
		t.Errorf("[4] expected empty EDS to survive 30 unrelated full pushes, got %v", got)
	} else {
		t.Logf("[4] after 30 unrelated full pushes, newly connected gateway: %v", got)
	}

	// [5] The poisoned entry has no TTL; it only clears on ClearAll (global push, istiod restart), or on the next
	// endpoint/SE/DR event for this service once no gateway holds a stale scope.
	s.Discovery.Cache.ClearAll()
	if got := connect("gw-after-global-push", "1.1.1.5"); len(got) != 1 || got[0] != "10.0.0.2:8080" {
		t.Fatalf("[5] after ClearAll expected [10.0.0.2:8080], got %v", got)
	} else {
		t.Logf("[5] after global push (ClearAll): %v", got)
	}
}
