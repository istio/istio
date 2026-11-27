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
)

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
				if req.Reason.Has(model.EndpointMTLSChange) {
					if !req.Full {
						t.Fatalf("EndpointMTLSChange push is not Full, so CDS would be skipped")
					}
					changed = true
				}
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
