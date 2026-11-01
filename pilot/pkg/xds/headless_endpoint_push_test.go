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

package xds

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

// TestHeadlessEndpointPushOptimization verifies that LDS/CDS/RDS pushes are skipped
// for headless endpoint updates where appropriate:
// - Routers: Skip LDS, CDS, and RDS (only push EDS)
// - Sidecars: Skip CDS and RDS (push LDS for TCP services, NDS for HTTP services)
func TestHeadlessEndpointPushOptimization(t *testing.T) {
	tests := []struct {
		name          string
		proxyType     model.NodeType
		reason        model.ReasonStats
		configUpdates sets.Set[model.ConfigKey]
		expectLDS     bool
		expectCDS     bool
		expectRDS     bool
	}{
		{
			name:      "Router with headless endpoint update should skip LDS, CDS and RDS",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: false,
			expectCDS: false,
			expectRDS: false,
		},
		{
			name:      "Sidecar with headless endpoint update should skip CDS and RDS but not LDS",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: true, // Sidecars need LDS for per-pod listeners/filter chains
			expectCDS: false,
			expectRDS: false,
		},
		{
			name:      "Router with non-headless ServiceEntry should push all",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.ConfigUpdate), // Not HeadlessEndpointUpdate
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: true,
			expectCDS: true,
			expectRDS: true,
		},
		{
			name:      "Router with mixed config types should push all",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
				model.ConfigKey{Kind: kind.VirtualService, Name: "my-vs", Namespace: "default"},
			),
			expectLDS: true,
			expectCDS: true,
			expectRDS: true,
		},
		{
			name:      "Sidecar with regular endpoint update (kind.Endpoints) should push none",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.EndpointUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.Endpoints, Name: "my-service", Namespace: "default"},
			),
			expectLDS: false, // kind.Endpoints is in skip list
			expectCDS: false, // kind.Endpoints is in skip list
			expectRDS: false, // kind.Endpoints is in skip list
		},
		{
			name:      "Router with HeadlessEndpointUpdate + ServiceUpdate should push all",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate, model.ServiceUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: true, // ServiceUpdate means service definition changed
			expectCDS: true, // ServiceUpdate means service definition changed
			expectRDS: true, // ServiceUpdate means service definition changed
		},
		{
			name:      "Sidecar with HeadlessEndpointUpdate + ServiceUpdate should push all",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate, model.ServiceUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: true, // ServiceUpdate means service definition changed
			expectCDS: true, // ServiceUpdate means service definition changed
			expectRDS: true, // ServiceUpdate means service definition changed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{
				Type:              tt.proxyType,
				IPAddresses:       []string{"1.1.1.1"},
				ID:                "test-proxy.default",
				Metadata:          &model.NodeMetadata{},
				MergedGateway:     &model.MergedGateway{},
				PrevMergedGateway: &model.PrevMergedGateway{},
			}

			req := &model.PushRequest{
				ConfigsUpdated: tt.configUpdates,
				Reason:         tt.reason,
				Push:           &model.PushContext{},
			}

			// Test LDS
			gotLDS := ldsNeedsPush(proxy, req)
			if gotLDS != tt.expectLDS {
				t.Errorf("ldsNeedsPush() = %v, want %v", gotLDS, tt.expectLDS)
			}

			// Test CDS
			_, gotCDS := cdsNeedsPush(req, proxy)
			if gotCDS != tt.expectCDS {
				t.Errorf("cdsNeedsPush() = %v, want %v", gotCDS, tt.expectCDS)
			}

			// Test RDS
			gotRDS := RdsNeedsPush(req, proxy)
			if gotRDS != tt.expectRDS {
				t.Errorf("RdsNeedsPush() = %v, want %v", gotRDS, tt.expectRDS)
			}
		})
	}
}

func TestFilterNdsPushCoalescedEndpointUpdates(t *testing.T) {
	service := model.ConfigKey{Kind: kind.ServiceEntry, Name: "db.default.svc.cluster.local", Namespace: "default"}
	endpoint := model.ConfigKey{Kind: kind.Endpoints, Name: service.Name, Namespace: service.Namespace}
	request := &model.PushRequest{
		Reason:         model.NewReasonStats(model.HeadlessEndpointUpdate, model.EndpointUpdate),
		ConfigsUpdated: sets.New(service, endpoint),
	}

	filtered, needsPush := filterNdsPush(request, &model.Proxy{Type: model.SidecarProxy})
	if !needsPush {
		t.Fatal("coalesced headless endpoint update did not trigger NDS")
	}
	if len(filtered.ConfigsUpdated) != 1 || !filtered.ConfigsUpdated.Contains(service) {
		t.Fatalf("filtered updates = %v, want only %v", filtered.ConfigsUpdated, service)
	}
	if !filtered.Reason.Has(model.HeadlessEndpointUpdate) || !filtered.Reason.Has(model.EndpointUpdate) {
		t.Fatalf("filtered reasons = %v, want both endpoint reasons", filtered.Reason)
	}
}

func TestCanSendPartialNdsPush(t *testing.T) {
	updates := func(kinds ...kind.Kind) sets.Set[model.ConfigKey] {
		out := sets.New[model.ConfigKey]()
		for _, k := range kinds {
			out.Insert(model.ConfigKey{Kind: k, Name: "a.default.svc.cluster.local", Namespace: "default"})
		}
		return out
	}
	tests := []struct {
		name string
		req  *model.PushRequest
		want bool
	}{
		{name: "service entry", req: &model.PushRequest{ConfigsUpdated: updates(kind.ServiceEntry)}, want: true},
		{name: "dns name", req: &model.PushRequest{ConfigsUpdated: updates(kind.ServiceEntry, kind.DNSName)}, want: true},
		{name: "forced", req: &model.PushRequest{Forced: true, ConfigsUpdated: updates(kind.ServiceEntry)}},
		{name: "no configs", req: &model.PushRequest{}},
		{name: "non delta-aware kind", req: &model.PushRequest{ConfigsUpdated: updates(kind.ServiceEntry, kind.Sidecar)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canSendPartialNdsPush(tt.req); got != tt.want {
				t.Fatalf("canSendPartialNdsPush() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupportsDeltaNDS(t *testing.T) {
	proxy := func(autoAllocate bool) *model.Proxy {
		return &model.Proxy{
			Metadata:     &model.NodeMetadata{DeltaNDS: true, DNSAutoAllocate: model.StringBool(autoAllocate), IstioVersion: "1.32.0"},
			IstioVersion: model.ParseIstioVersion("1.32.0"),
		}
	}
	tests := []struct {
		name         string
		ipAutoAlloc  bool
		autoAllocate bool
		want         bool
	}{
		{name: "new allocator", ipAutoAlloc: true, autoAllocate: true, want: true},
		{name: "legacy allocator", autoAllocate: true},
		{name: "no auto allocation", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableIPAutoallocate, tt.ipAutoAlloc)
			if got := supportsDeltaNDS(proxy(tt.autoAllocate)); got != tt.want {
				t.Fatalf("supportsDeltaNDS() = %v, want %v", got, tt.want)
			}
		})
	}
}
