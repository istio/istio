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
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

func TestRDS(t *testing.T) {
	tests := []struct {
		name   string
		node   string
		routes []string
	}{
		{
			"sidecar_new",
			sidecarID(app3Ip, "app3"),
			[]string{"80", "8080"},
		},
		{
			"gateway_new",
			gatewayID(gatewayIP),
			[]string{"http.80", "https.443.https.my-gateway.testns"},
		},
		{
			// Even if we get a bad route, we should still send Envoy an empty response, rather than
			// ignore it. If we ignore the route, the listeners can get stuck waiting forever.
			"sidecar_badroute",
			sidecarID(app3Ip, "app3"),
			[]string{"ht&p"},
		},
	}

	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ads := s.ConnectADS().WithType(v3.RouteType).WithID(tt.node)
			ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: tt.routes})
		})
	}
}

const (
	app3Ip    = "10.2.0.1"
	gatewayIP = "10.3.0.1"
)

// Common code for the xds testing.
// The tests in this package use an in-process pilot using mock service registry and
// envoy.

func sidecarID(ip, deployment string) string { // nolint: unparam
	return fmt.Sprintf("sidecar~%s~%s-644fc65469-96dza.testns~testns.svc.cluster.local", ip, deployment)
}

func gatewayID(ip string) string { //nolint: unparam
	return fmt.Sprintf("router~%s~istio-gateway-644fc65469-96dzt.istio-system~istio-system.svc.cluster.local", ip)
}

// TestDeltaRDS exercises the full RdsGenerator.GenerateDeltas path (rdsNeedsPush +
// shouldUseDeltaRoutes + BuildDeltaHTTPRoutes) over a real Delta xDS connection, rather than
// unit testing BuildDeltaHTTPRoutes in isolation.
func TestDeltaRDS(t *testing.T) {
	test.SetForTest(t, &features.EnableDeltaRDS, true)

	const numRoutes = 3
	configs := createDeltaRDSBenchmarkConfig(numRoutes)
	// A real DestinationRule targeting one of the routed services, so the sidecar's scope
	// actually depends on it (an update to an unrelated/nonexistent config would be filtered
	// out before ever reaching RDS, which isn't the case we want to exercise here).
	configs = append(configs, config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "dr-0", Namespace: "default"},
		Spec: &networking.DestinationRule{Host: "svc-0.default.svc.cluster.local"},
	})
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{Configs: configs})
	s.EnsureSynced(t)

	// RDS is not wildcard-subscribable: a real Envoy discovers route names via LDS and then
	// subscribes to those specific RDS resources. Compute the same route names here so we can
	// subscribe to them explicitly, the way a real client would.
	proxy := &model.Proxy{
		Type:            model.SidecarProxy,
		IPAddresses:     []string{"10.9.9.9"},
		ID:              "delta-rds-0.default",
		ConfigNamespace: "default",
		Metadata:        &model.NodeMetadata{},
	}
	proxy.DiscoverIPMode()
	initPushContext(s.Env(), proxy)
	watched := getWatchedResources(v3.RouteType, ConfigInput{}, s, proxy)
	routeNames := watched.ResourceNames.UnsortedList()
	if len(routeNames) != numRoutes {
		t.Fatalf("expected %d routes, got %v", numRoutes, routeNames)
	}

	nodeID := "sidecar~10.9.9.9~delta-rds-0.default~default.svc.cluster.local"
	ads := s.ConnectDeltaADS().WithType(v3.RouteType).WithID(nodeID)

	// Initial subscription gets every route the sidecar watches.
	resp := ads.RequestResponseAck(&discovery.DeltaDiscoveryRequest{ResourceNamesSubscribe: routeNames})
	initial := sets.New(slices.Map(resp.Resources, (*discovery.Resource).GetName)...)
	if initial.Len() != numRoutes {
		t.Fatalf("expected %d initial routes, got %v", numRoutes, initial)
	}

	// A VirtualService update naming a single route should push only that route as a true
	// delta: one resource, no removals.
	s.Discovery.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.VirtualService, Name: "vs-0", Namespace: "default"}),
	})
	resp = ads.ExpectResponse()
	got := slices.Map(resp.Resources, (*discovery.Resource).GetName)
	if len(got) != 1 || !initial.Contains(got[0]) {
		t.Fatalf("expected a single delta route update, got %v", got)
	}
	if len(resp.RemovedResources) != 0 {
		t.Fatalf("expected no removed routes, got %v", resp.RemovedResources)
	}

	// A config kind delta RDS doesn't know how to map (DestinationRule) forces a full rebuild
	// of every watched route rather than a partial delta.
	s.Discovery.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "dr-0", Namespace: "default"}),
	})
	resp = ads.ExpectResponse()
	got = slices.Map(resp.Resources, (*discovery.Resource).GetName)
	if len(got) != numRoutes {
		t.Fatalf("expected a full rebuild of %d routes, got %v", numRoutes, got)
	}

	// A headless-endpoint-only ServiceEntry update must not trigger an RDS push at all.
	s.Discovery.ConfigUpdate(&model.PushRequest{
		Reason:         model.NewReasonStats(model.HeadlessEndpointUpdate),
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "svc-0", Namespace: "default"}),
	})
	ads.ExpectNoResponse()
}
