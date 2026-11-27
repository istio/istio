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

package model

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

func TestDestinationRulePortLevelSettingsMerge(t *testing.T) {
	port80 := &networking.TrafficPolicy_PortTrafficPolicy{
		Port: &networking.PortSelector{Number: 80},
		Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
	}
	port443 := &networking.TrafficPolicy_PortTrafficPolicy{
		Port: &networking.PortSelector{Number: 443},
		Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
	}
	duplicate80 := &networking.TrafficPolicy_PortTrafficPolicy{
		Port: &networking.PortSelector{Number: 80},
		ConnectionPool: &networking.ConnectionPoolSettings{
			Tcp: &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 10},
		},
	}
	firstTLS := &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE}
	incomingTLS := &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}
	policy := func(tls *networking.ClientTLSSettings, ports ...*networking.TrafficPolicy_PortTrafficPolicy) *networking.TrafficPolicy {
		return &networking.TrafficPolicy{Tls: tls, PortLevelSettings: ports}
	}
	cases := []struct {
		name     string
		policies []*networking.TrafficPolicy
		want     *networking.TrafficPolicy
	}{
		{
			name:     "distinct ports",
			policies: []*networking.TrafficPolicy{policy(firstTLS, port80), policy(incomingTLS, port443)},
			want:     policy(firstTLS, port80, port443),
		},
		{
			name:     "duplicate port keeps entire first setting",
			policies: []*networking.TrafficPolicy{policy(firstTLS, port80), policy(incomingTLS, duplicate80, port443)},
			want:     policy(firstTLS, port80, port443),
		},
		{
			name:     "three rules retain accumulated ports",
			policies: []*networking.TrafficPolicy{policy(firstTLS, port80), policy(nil, port443), policy(incomingTLS, duplicate80, port443)},
			want:     policy(firstTLS, port80, port443),
		},
		{
			name:     "first policy has no ports",
			policies: []*networking.TrafficPolicy{policy(firstTLS), policy(incomingTLS, port443)},
			want:     policy(firstTLS, port443),
		},
		{
			name:     "first policy absent",
			policies: []*networking.TrafficPolicy{nil, policy(incomingTLS, port443)},
			want:     policy(incomingTLS, port443),
		},
		{
			name:     "incoming policy absent",
			policies: []*networking.TrafficPolicy{policy(firstTLS, port80), nil},
			want:     policy(firstTLS, port80),
		},
		{
			name:     "incoming policy has no ports",
			policies: []*networking.TrafficPolicy{policy(nil, port80), policy(incomingTLS)},
			want:     policy(nil, port80),
		},
	}
	for _, hostname := range []string{"specific.example.com", "*.wildcard.example.com"} {
		t.Run(hostname, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					ps := NewPushContext()
					ps.exportToDefaults.destinationRule = sets.New(visibility.Public)
					configs := make([]config.Config, len(tc.policies))
					originals := make([]*networking.DestinationRule, len(tc.policies))
					for i, tp := range tc.policies {
						rule := &networking.DestinationRule{Host: hostname, TrafficPolicy: protomarshal.Clone(tp)}
						originals[i] = protomarshal.Clone(rule)
						// Reverse input order to exercise creation-time precedence, not slice order.
						configs[len(configs)-1-i] = config.Config{
							Meta: config.Meta{Name: tc.name, Namespace: "test", CreationTimestamp: time.Unix(int64(i), 0)},
							Spec: rule,
						}
					}
					ps.setDestinationRules(configs)
					for _, index := range []*consolidatedDestRules{
						ps.destinationRuleIndex.namespaceLocal["test"],
						ps.destinationRuleIndex.exportedByNamespace["test"],
					} {
						rules := index.specificDestRules[host.Name(hostname)]
						if host.Name(hostname).IsWildCarded() {
							rules = index.wildcardDestRules[host.Name(hostname)]
						}
						assert.Equal(t, len(rules), 1)
						got := rules[0].rule.Spec.(*networking.DestinationRule).TrafficPolicy
						if len(got.GetPortLevelSettings()) != len(tc.want.PortLevelSettings) {
							t.Fatalf("expected %d port-level settings, got %d", len(tc.want.PortLevelSettings), len(got.GetPortLevelSettings()))
						}
						assert.Equal(t, got, tc.want)
					}
					for i, cfg := range configs {
						assert.Equal(t, cfg.Spec.(*networking.DestinationRule), originals[i])
					}
				})
			}
		})
	}
}

func TestBackendPolicyPortSettingsUpdates(t *testing.T) {
	for _, backendFirst := range []bool{true, false} {
		name := "backend created last"
		if backendFirst {
			name = "backend created first"
		}
		t.Run(name, func(t *testing.T) {
			const testHost = "backend.test.svc.cluster.local"
			user := config.Config{
				Meta: config.Meta{Name: "user", Namespace: "test", CreationTimestamp: time.Unix(2, 0)},
				Spec: &networking.DestinationRule{Host: testHost, TrafficPolicy: &networking.TrafficPolicy{
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
						Port: &networking.PortSelector{Number: 8080},
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{Simple: networking.LoadBalancerSettings_ROUND_ROBIN},
						},
					}},
				}},
			}
			backend := config.Config{
				Meta: config.Meta{
					Name: "backend-policy", Namespace: "test", CreationTimestamp: time.Unix(1, 0),
					Annotations: map[string]string{constants.InternalParentNames: "BackendTLSPolicy/tls.test"},
				},
				Spec: &networking.DestinationRule{Host: testHost, TrafficPolicy: &networking.TrafficPolicy{
					PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
						Port:        &networking.PortSelector{Number: 8080},
						Tls:         &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE, Sni: "old.example.com"},
						RetryBudget: &networking.TrafficPolicy_RetryBudget{MinRetryConcurrency: 5},
					}},
				}},
			}
			if !backendFirst {
				backend.CreationTimestamp = time.Unix(3, 0)
			}
			originalUser := protomarshal.Clone(user.Spec.(*networking.DestinationRule))
			originalBackend := protomarshal.Clone(backend.Spec.(*networking.DestinationRule))
			merge := func(backend *config.Config) *networking.TrafficPolicy_PortTrafficPolicy {
				t.Helper()
				ps := NewPushContext()
				ps.exportToDefaults.destinationRule = sets.New(visibility.Public)
				configs := []config.Config{user}
				if backend != nil {
					configs = append(configs, *backend)
				}
				ps.setDestinationRules(configs)
				merged := ps.destinationRuleIndex.namespaceLocal["test"].specificDestRules[host.Name(testHost)]
				assert.Equal(t, len(merged), 1)
				ports := merged[0].rule.Spec.(*networking.DestinationRule).TrafficPolicy.PortLevelSettings
				assert.Equal(t, len(ports), 1)
				assert.Equal(t, ports[0].LoadBalancer, originalUser.TrafficPolicy.PortLevelSettings[0].LoadBalancer)
				return ports[0]
			}
			merged := merge(&backend)
			assert.Equal(t, merged.Tls, originalBackend.TrafficPolicy.PortLevelSettings[0].Tls)
			assert.Equal(t, merged.RetryBudget, originalBackend.TrafficPolicy.PortLevelSettings[0].RetryBudget)
			assert.Equal(t, user.Spec.(*networking.DestinationRule), originalUser)
			assert.Equal(t, backend.Spec.(*networking.DestinationRule), originalBackend)

			updated := backend.DeepCopy()
			updated.Spec.(*networking.DestinationRule).TrafficPolicy.PortLevelSettings[0].Tls.Sni = "new.example.com"
			assert.Equal(t, merge(&updated).Tls, updated.Spec.(*networking.DestinationRule).TrafficPolicy.PortLevelSettings[0].Tls)
			assert.Equal(t, user.Spec.(*networking.DestinationRule), originalUser)
			assert.Equal(t, merge(nil).Tls, (*networking.ClientTLSSettings)(nil))
		})
	}
}

func TestConsolidatedDestRuleEquals(t *testing.T) {
	testcases := []struct {
		name     string
		l        *ConsolidatedDestRule
		r        *ConsolidatedDestRule
		expected bool
	}{
		{
			name:     "two nil",
			expected: true,
		},
		{
			name: "l is nil",
			l:    nil,
			r: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
				},
			},
			expected: false,
		},
		{
			name: "r is nil",
			l: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
				},
			},
			r:        nil,
			expected: false,
		},
		{
			name: "from length not equal",
			l: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
				},
			},
			r: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			expected: false,
		},
		{
			name: "from length equals but element is different",
			l: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			r: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr3",
					},
				},
			},
			expected: false,
		},
		{
			name: "all from elements equal",
			l: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			r: &ConsolidatedDestRule{
				from: []types.NamespacedName{
					{
						Namespace: "default",
						Name:      "dr1",
					},
					{
						Namespace: "default",
						Name:      "dr2",
					},
				},
			},
			expected: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.l.Equals(tc.r), tc.expected)
		})
	}
}
