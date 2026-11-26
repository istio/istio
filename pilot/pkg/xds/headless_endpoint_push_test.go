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
	"fmt"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
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
		{
			name:      "Sidecar with EndpointMTLSChange should push CDS but not RDS",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate, model.EndpointMTLSChange),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: true,
			expectCDS: true, // clients infer auto-mTLS for the service from its endpoints
			expectRDS: false,
		},
		{
			name:      "Router with EndpointMTLSChange should push CDS only",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.HeadlessEndpointUpdate, model.EndpointMTLSChange),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"},
			),
			expectLDS: false,
			expectCDS: true,
			expectRDS: false,
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

func TestEDSUpdateInferredMTLSChange(t *testing.T) {
	newServer := func(services map[string]model.Resolution, destRules ...*networking.DestinationRule) *DiscoveryServer {
		env := model.NewEnvironment()
		env.SetPushContext(newInferredMTLSPushContext(services, destRules...))
		s := NewDiscoveryServer(env, nil, nil)
		t.Cleanup(s.Shutdown)
		return s
	}
	mtlsChange := func(s *DiscoveryServer) bool {
		changed := false
		for {
			select {
			case req := <-s.pushChannel:
				changed = changed || req.Reason.Has(model.EndpointMTLSChange)
			default:
				return changed
			}
		}
	}
	endpoint := func(ip, tlsMode string) []*model.IstioEndpoint {
		return []*model.IstioEndpoint{{Addresses: []string{ip}, ServicePortName: "tcp", TLSMode: tlsMode, HealthStatus: model.Healthy}}
	}
	type step struct {
		name  string
		svc   string
		shard model.ShardKey
		eps   []*model.IstioEndpoint
		want  bool
	}
	run := func(t *testing.T, s *DiscoveryServer, steps []step) {
		for _, st := range steps {
			s.EDSUpdate(st.shard, st.svc, "default", st.eps)
			if got := mtlsChange(s); got != st.want {
				t.Fatalf("%s: EndpointMTLSChange = %v, want %v", st.name, got, st.want)
			}
		}
	}
	local := model.ShardKey{Cluster: "local"}
	remote := model.ShardKey{Cluster: "remote"}
	mtls, plaintext := model.IstioMutualTLSModeLabel, model.DisabledTLSModeLabel

	t.Run("headless across clusters", func(t *testing.T) {
		s := newServer(map[string]model.Resolution{"headless.default.svc": model.Passthrough, "clusterip.default.svc": model.ClientSideLB})
		run(t, s, []step{
			{"remote mTLS endpoint appears", "headless.default.svc", remote, endpoint("10.0.0.1", mtls), true},
			{"local plaintext endpoint appears", "headless.default.svc", local, endpoint("10.0.1.1", plaintext), true},
			{"local plaintext endpoint goes away", "headless.default.svc", local, nil, true},
			{"local mTLS endpoint appears", "headless.default.svc", local, endpoint("10.0.1.1", mtls), false},
			{"remote endpoint goes away", "headless.default.svc", remote, nil, false},
			{"last endpoint goes away", "headless.default.svc", local, nil, true},
			{"not passthrough", "clusterip.default.svc", local, endpoint("10.0.2.1", mtls), false},
		})
	})

	t.Run("passthrough destination rule", func(t *testing.T) {
		s := newServer(
			map[string]model.Resolution{"top.default.svc": model.ClientSideLB, "subset.default.svc": model.ClientSideLB},
			&networking.DestinationRule{
				Host:          "top.default.svc",
				TrafficPolicy: &networking.TrafficPolicy{LoadBalancer: passthroughLB()},
			},
			&networking.DestinationRule{
				Host:    "subset.default.svc",
				Subsets: []*networking.Subset{{Name: "v1", TrafficPolicy: &networking.TrafficPolicy{LoadBalancer: passthroughLB()}}},
			},
		)
		run(t, s, []step{
			{"top level endpoint appears", "top.default.svc", local, endpoint("10.0.0.1", mtls), true},
			{"top level endpoint goes away", "top.default.svc", local, nil, true},
			{"subset endpoint appears", "subset.default.svc", local, endpoint("10.0.1.1", mtls), true},
			{"subset endpoint goes away", "subset.default.svc", local, nil, true},
		})
	})

	t.Run("service registered after its endpoints", func(t *testing.T) {
		s := newServer(nil)
		run(t, s, []step{{"unknown service endpoint appears", "late.default.svc", local, endpoint("10.0.0.1", mtls), false}})
		s.Env.SetPushContext(newInferredMTLSPushContext(map[string]model.Resolution{"late.default.svc": model.Passthrough}))
		run(t, s, []step{{"last endpoint goes away", "late.default.svc", local, nil, true}})
	})
}

func newInferredMTLSPushContext(services map[string]model.Resolution, destRules ...*networking.DestinationRule) *model.PushContext {
	push := model.NewPushContext()
	for name, resolution := range services {
		push.ServiceIndex.HostnameAndNamespace[host.Name(name)] = map[string]*model.Service{
			"default": {Hostname: host.Name(name), Resolution: resolution, Attributes: model.ServiceAttributes{Namespace: "default"}},
		}
	}
	configs := make([]config.Config, 0, len(destRules))
	for i, dr := range destRules {
		configs = append(configs, config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: fmt.Sprintf("dr%d", i), Namespace: "default"},
			Spec: dr,
		})
	}
	push.SetDestinationRulesForTesting(configs)
	return push
}

func passthroughLB() *networking.LoadBalancerSettings {
	return &networking.LoadBalancerSettings{
		LbPolicy: &networking.LoadBalancerSettings_Simple{Simple: networking.LoadBalancerSettings_PASSTHROUGH},
	}
}
