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

package grpcgen

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func TestApplyCircuitBreakers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		policy      *networking.TrafficPolicy
		wantNil     bool
		wantMaxReqs uint32
	}{
		{
			name: "nil connection pool",
			policy: &networking.TrafficPolicy{
				ConnectionPool: nil,
			},
			wantNil: true,
		},
		{
			name: "no http2MaxRequests",
			policy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{},
				},
			},
			wantNil: true,
		},
		{
			name: "http2MaxRequests set",
			policy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						Http2MaxRequests: 100,
					},
				},
			},
			wantMaxReqs: 100,
		},
		{
			name: "only tcp maxConnections set - should be ignored",
			policy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						MaxConnections: 50,
					},
				},
			},
			wantNil: true, // No circuit breakers since only max_requests is supported.
		},
		{
			name: "other http fields ignored - only max_requests used",
			policy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						Http2MaxRequests:        100,
						Http1MaxPendingRequests: 200, // Ignored.
						MaxRetries:              3,   // Ignored.
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						MaxConnections: 50, // Ignored.
					},
				},
			},
			wantMaxReqs: 100, // Only max_requests is set.
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := &cluster.Cluster{}
			b := &clusterBuilder{
				node: &model.Proxy{ID: "test-node"},
			}
			b.applyCircuitBreakers(c, test.policy)

			if test.wantNil {
				if c.CircuitBreakers != nil {
					t.Errorf("expected nil CircuitBreakers, got %v", c.CircuitBreakers)
				}
				return
			}

			if c.CircuitBreakers == nil {
				t.Fatal("CircuitBreakers should not be nil")
			}
			if len(c.CircuitBreakers.Thresholds) != 1 {
				t.Fatalf("expected 1 threshold, got %d", len(c.CircuitBreakers.Thresholds))
			}

			th := c.CircuitBreakers.Thresholds[0]
			if th.MaxRequests.Value != test.wantMaxReqs {
				t.Errorf("MaxRequests: got %d, want %d", th.MaxRequests.Value, test.wantMaxReqs)
			}

			// Verify other fields are NOT set (per gRFC A32).
			if th.MaxConnections != nil {
				t.Errorf("MaxConnections should not be set, got %d", th.MaxConnections.Value)
			}
			if th.MaxRetries != nil {
				t.Errorf("MaxRetries should not be set, got %d", th.MaxRetries.Value)
			}
			if th.MaxPendingRequests != nil {
				t.Errorf("MaxPendingRequests should not be set, got %d", th.MaxPendingRequests.Value)
			}
			if th.TrackRemaining {
				t.Error("TrackRemaining should not be set")
			}
		})
	}
}

func TestApplyLoadBalancing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		policy       *networking.TrafficPolicy
		wantLbPolicy cluster.Cluster_LbPolicy
	}{
		{
			name:         "nil policy",
			policy:       nil,
			wantLbPolicy: cluster.Cluster_ROUND_ROBIN, // Default is ROUND_ROBIN.
		},
		{
			name: "ROUND_ROBIN",
			policy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
			},
			wantLbPolicy: cluster.Cluster_ROUND_ROBIN, // Default gRPC behavior.
		},
		{
			name: "LEAST_REQUEST",
			policy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_LEAST_REQUEST,
					},
				},
			},
			wantLbPolicy: cluster.Cluster_LEAST_REQUEST,
		},
		{
			name: "RING_HASH with consistent hash",
			policy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
						ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
							HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpHeaderName{
								HttpHeaderName: "x-session-id",
							},
						},
					},
				},
			},
			wantLbPolicy: cluster.Cluster_RING_HASH,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := &cluster.Cluster{}
			b := &clusterBuilder{}
			b.applyLoadBalancing(c, test.policy)

			if c.LbPolicy != test.wantLbPolicy {
				t.Errorf("LbPolicy: got %v, want %v", c.LbPolicy, test.wantLbPolicy)
			}
		})
	}
}

func TestApplyTrafficPolicy(t *testing.T) {
	t.Parallel()

	t.Run("calls all sub-functions", func(t *testing.T) {
		t.Parallel()

		c := &cluster.Cluster{}
		b := &clusterBuilder{}
		policy := &networking.TrafficPolicy{
			ConnectionPool: &networking.ConnectionPoolSettings{
				Http: &networking.ConnectionPoolSettings_HTTPSettings{
					Http2MaxRequests: 100,
				},
			},
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{
					Simple: networking.LoadBalancerSettings_LEAST_REQUEST,
				},
			},
		}

		b.applyTrafficPolicy(c, policy)

		// Verify circuit breakers were applied.
		if c.CircuitBreakers == nil {
			t.Error("CircuitBreakers should be set")
		}

		// Verify load balancing was applied.
		if c.LbPolicy != cluster.Cluster_LEAST_REQUEST {
			t.Errorf("LbPolicy should be LEAST_REQUEST, got %v", c.LbPolicy)
		}
	})
}
