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

// TestEdsCacheNotPoisonedByMergedPush covers EDS cache poisoning by a gateway whose SidecarScope regressed to an old
// DestinationRule when PILOT_ENABLE_EDS_DEBOUNCE=false.
//
// Un-debounced EDS pushes run concurrently with full pushes, so an EDS push can read the old push context and be
// enqueued after a DR full push. PushRequest merging used to keep the later-enqueued (older) push context, and
// computeProxyState rebuilt the gateway's SidecarScope from it with the old DR subsets but a fresh Start. The EDS
// cache key (EndpointBuilder.WriteHash) has the DR name/namespace but not its subsets, and is shared by all gateways
// with the same cluster/network/locality, so the stale result was cached and served to every gateway on the istiod
// instance:
//  1. after the DR subset change, correct gateways still got the old subset's pods;
//  2. once the old pods were removed, they got empty EDS (no healthy upstream);
//  3. new connections got the same empty EDS, until ClearAll or the next SE/DR/endpoint change for the service.
//
// Merging now keeps the most recent push context (see TestPushQueueMergeKeepsLatestPushContext).
func TestEdsCacheNotPoisonedByMergedPush(t *testing.T) {
	const (
		hostname = "foo.com"
		ns       = "default"
		clusterN = "outbound|80|stable|foo.com"
	)
	makeDR := func(hash string) config.Config {
		return config.Config{
			Meta: config.Meta{Name: "foo", Namespace: ns, GroupVersionKind: gvk.DestinationRule},
			Spec: &networking.DestinationRule{
				Host:    hostname,
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
	drKey := model.ConfigKey{Kind: kind.DestinationRule, Name: "foo", Namespace: ns}
	epKey := model.ConfigKey{Kind: kind.Endpoints, Name: hostname, Namespace: ns}

	s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{Configs: []config.Config{se, makeDR("old")}})
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
	assertEndpoints := func(step string, got []string) {
		t.Helper()
		if len(got) != 1 || got[0] != "10.0.0.2:8080" {
			t.Errorf("%s: expected [10.0.0.2:8080], got %v", step, got)
		}
	}

	stale := gateway("gw-stale", "1.1.1.1")
	oldPush := s.PushContext()

	// Canary promotion: the DR's stable subset is rewritten to the new pod hash.
	if _, err := s.Store().Update(makeDR("new")); err != nil {
		t.Fatal(err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		if got := subsetHash(gateway("probe", "9.9.9.9")); got != "new" {
			return fmt.Errorf("push context not updated yet: %q", got)
		}
		return nil
	}, retry.Timeout(5*time.Second))

	// gw-stale's DR full push is merged with an un-debounced EDS push that read the old push context, and
	// computeProxyState rebuilds its SidecarScope from the merged push context.
	full := push(drKey)
	full.Forced = true
	merged := full.CopyMerge(&model.PushRequest{Push: oldPush, ConfigsUpdated: sets.New(epKey), Start: time.Now()})
	stale.SetSidecarScope(merged.Push)
	if got := subsetHash(stale); got != "new" {
		t.Fatalf("gw-stale's SidecarScope regressed to the %q DR", got)
	}

	setEndpoints(ep("10.0.0.1", "old"), ep("10.0.0.2", "new"))
	fresh := gateway("gw-fresh", "1.1.1.2")

	// [1] New pods ready: gw-fresh must not be served gw-stale's old pod from the shared cache key.
	req := push(epKey)
	gen(stale, req)
	assertEndpoints("[1] gw-fresh after DR change", gen(fresh, req))

	// [2] Old pods removed: gateways must keep the new pod rather than drop to empty EDS.
	setEndpoints(ep("10.0.0.2", "new"))
	req = push(epKey)
	assertEndpoints("[2] gw-stale after old pods removed", gen(stale, req))
	assertEndpoints("[2] gw-fresh after old pods removed", gen(fresh, req))

	// [3] A gateway (re)connecting to this istiod gets the new pod.
	assertEndpoints("[3] newly connected gateway",
		gen(gateway("gw-reconnect", "1.1.1.3"), &model.PushRequest{Push: s.PushContext(), Start: time.Now(), Forced: true}))
}
