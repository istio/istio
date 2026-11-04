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
	"fmt"
	"testing"

	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/assert"
)

func TestUpdateServiceAccount(t *testing.T) {
	var (
		c1Key = ShardKey{Cluster: "c1"}
		c2Key = ShardKey{Cluster: "c2"}
	)

	cluster1Endpoints := []*IstioEndpoint{
		{Addresses: []string{"10.172.0.1"}, ServiceAccount: "sa1"},
		{Addresses: []string{"10.172.0.2"}, ServiceAccount: "sa-vm1"},
	}

	testCases := []struct {
		name      string
		shardKey  ShardKey
		endpoints []*IstioEndpoint
		expect    bool
	}{
		{
			name:      "added new endpoint",
			shardKey:  c1Key,
			endpoints: append(cluster1Endpoints, &IstioEndpoint{Addresses: []string{"10.172.0.3"}, ServiceAccount: "sa1"}),
			expect:    false,
		},

		{
			name:      "added new endpoint with multiple addresses",
			shardKey:  c1Key,
			endpoints: append(cluster1Endpoints, &IstioEndpoint{Addresses: []string{"10.171.0.1", "2001:1::1"}, ServiceAccount: "sa1"}),
			expect:    false,
		},
		{
			name:      "added new sa",
			shardKey:  c1Key,
			endpoints: append(cluster1Endpoints, &IstioEndpoint{Addresses: []string{"10.172.0.3"}, ServiceAccount: "sa2"}),
			expect:    true,
		},
		{
			name:      "added new sa of an endpoint with multiple addresses",
			shardKey:  c1Key,
			endpoints: append(cluster1Endpoints, &IstioEndpoint{Addresses: []string{"10.172.0.3", "2001:1::3"}, ServiceAccount: "sa2"}),
			expect:    true,
		},
		{
			name:     "updated endpoints address",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Addresses: []string{"10.172.0.5"}, ServiceAccount: "sa1"},
				{Addresses: []string{"10.172.0.2"}, ServiceAccount: "sa-vm1"},
			},
			expect: false,
		},
		{
			name:     "updated endpoints multiple addresses",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Addresses: []string{"10.172.0.5", "2001:1::5"}, ServiceAccount: "sa1"},
				{Addresses: []string{"10.172.0.2", "2001:1::2"}, ServiceAccount: "sa-vm1"},
			},
			expect: false,
		},
		{
			name:     "deleted one endpoint with unique sa",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Addresses: []string{"10.172.0.1"}, ServiceAccount: "sa1"},
			},
			expect: true,
		},
		{
			name:     "deleted one endpoint which contains multiple addresses with unique sa",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Addresses: []string{"10.171.0.1", "2001:1::1"}, ServiceAccount: "sa1"},
			},
			expect: true,
		},

		{
			name:     "deleted one endpoint with duplicate sa",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Addresses: []string{"10.172.0.2"}, ServiceAccount: "sa-vm1"},
			},
			expect: false,
		},
		{
			name:      "deleted endpoints",
			shardKey:  c1Key,
			endpoints: nil,
			expect:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			originalEndpointsShard := &EndpointShards{
				Shards: map[ShardKey][]*IstioEndpoint{
					c1Key: cluster1Endpoints,
					c2Key: {{Addresses: []string{"10.244.0.1"}, ServiceAccount: "sa1"}, {Addresses: []string{"10.244.0.2"}, ServiceAccount: "sa-vm2"}},
				},
				ServiceAccounts: map[string]struct{}{
					"sa1":    {},
					"sa-vm1": {},
					"sa-vm2": {},
				},
			}
			originalEndpointsShard.Shards[tc.shardKey] = tc.endpoints
			ret := updateShardServiceAccount(originalEndpointsShard, "test-svc")
			if ret != tc.expect {
				t.Errorf("expect UpdateServiceAccount %v, but got %v", tc.expect, ret)
			}
		})
	}
}

func TestUpdateServiceEndpoints(t *testing.T) {
	var (
		c1Key = ShardKey{Cluster: "c1"}
		c2Key = ShardKey{Cluster: "c2"}
	)
	endpoints := NewEndpointIndex(DisabledCache{})

	cluster1Endpoints := []*IstioEndpoint{
		{Addresses: []string{"10.172.0.1"}, Namespace: "foo", HostName: "foo.com"},
		{Addresses: []string{"10.172.0.2"}, Namespace: "foo", HostName: "foo.com"},
	}

	cluster2Endpoints := []*IstioEndpoint{
		{Addresses: []string{"10.172.0.3", "2001:1::3"}, Namespace: "bar", HostName: "bar.com"},
		{Addresses: []string{"10.172.0.4", "2001:1::4"}, Namespace: "bar", HostName: "bar.com"},
	}

	testCases := []struct {
		name      string
		shardKey  ShardKey
		endpoints []*IstioEndpoint
		namespace string
		hostname  string
		expect    int
	}{
		{
			name:      "istioEndpoint with IPv4 only address",
			shardKey:  c1Key,
			endpoints: cluster1Endpoints,
			namespace: "foo",
			hostname:  "foo.com",
			expect:    2,
		},
		{
			name:      "istioEndpoint with both IPv4/6 addresses",
			shardKey:  c2Key,
			endpoints: cluster2Endpoints,
			namespace: "bar",
			hostname:  "bar.com",
			expect:    2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			endpoints.UpdateServiceEndpoints(tc.shardKey, tc.hostname, tc.namespace, tc.endpoints, true)
			eps, _ := endpoints.ShardsForService(tc.hostname, tc.namespace)
			assert.Equal(t, len(eps.Shards[tc.shardKey]), tc.expect)
		})
	}
}

// Receiving endpoints for a service we have not seen before creates its shard, but creating the
// shard alone must not require a full push: the endpoints are recorded either way, and a service
// only becomes visible to config generation via its own update, which re-initializes the service
// registry and picks them up.
//
// Note that the service account check applies independently and does force a full push whenever the
// endpoints introduce a service account, which is the common case for real workloads.
func TestUpdateServiceEndpointsNewShardPushType(t *testing.T) {
	shardKey := ShardKey{Cluster: "c1"}

	t.Run("without service accounts", func(t *testing.T) {
		endpoints := NewEndpointIndex(DisabledCache{})
		pushType := endpoints.UpdateServiceEndpoints(shardKey, "foo.com", "foo", []*IstioEndpoint{
			{Addresses: []string{"10.172.0.1"}, Namespace: "foo", HostName: "foo.com"},
		}, true)
		assert.Equal(t, pushType, IncrementalPush)

		// The endpoints are recorded regardless, so a later service update can pick them up.
		eps, ok := endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, true)
		assert.Equal(t, len(eps.Shards[shardKey]), 1)
	})

	t.Run("new service account still requires a full push", func(t *testing.T) {
		endpoints := NewEndpointIndex(DisabledCache{})
		pushType := endpoints.UpdateServiceEndpoints(shardKey, "foo.com", "foo", []*IstioEndpoint{
			{Addresses: []string{"10.172.0.1"}, Namespace: "foo", HostName: "foo.com", ServiceAccount: "sa"},
		}, true)
		assert.Equal(t, pushType, FullPush)
	})
}

func TestDeleteServiceShard(t *testing.T) {
	c1Key := ShardKey{Cluster: "c1"}
	c2Key := ShardKey{Cluster: "c2"}
	endpoint := &IstioEndpoint{Addresses: []string{"10.0.0.1"}, Namespace: "foo", HostName: "foo.com"}
	setup := func(shards ...ShardKey) *EndpointIndex {
		endpoints := NewEndpointIndex(DisabledCache{})
		for _, shard := range shards {
			endpoints.UpdateServiceEndpoints(shard, "foo.com", "foo", []*IstioEndpoint{endpoint}, false)
		}
		return endpoints
	}

	t.Run("unknown service", func(t *testing.T) {
		endpoints := setup()
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", false)
		_, ok := endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, false)
	})

	t.Run("deletes the shard and the keys", func(t *testing.T) {
		endpoints := setup(c1Key)
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", false)
		_, ok := endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, false)
		// Deleting it again is a no-op.
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", false)
		_, ok = endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, false)
	})

	t.Run("keeps the keys while other shards have endpoints", func(t *testing.T) {
		endpoints := setup(c1Key, c2Key)
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", false)
		eps, ok := endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, true)
		assert.Equal(t, eps.Shards, map[ShardKey][]*IstioEndpoint{c2Key: {endpoint}})
		// The shard is already gone and the other shard keeps the keys, so there is nothing to delete.
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", false)
		eps, ok = endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, true)
		assert.Equal(t, eps.Shards, map[ShardKey][]*IstioEndpoint{c2Key: {endpoint}})
	})

	t.Run("deletes keys preserved by an earlier deletion", func(t *testing.T) {
		endpoints := setup(c1Key)
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", true)
		eps, ok := endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, true)
		assert.Equal(t, len(eps.Shards), 0)
		// A later service deletion removes the preserved keys, even without endpoints.
		endpoints.DeleteServiceShard(c1Key, "foo.com", "foo", false)
		_, ok = endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, ok, false)
	})
}

func TestReplaceServiceEndpoints(t *testing.T) {
	shardKey := ShardKey{Cluster: "c1"}
	endpoint := func(addr, sa string, health HealthStatus) *IstioEndpoint {
		return &IstioEndpoint{Addresses: []string{addr}, Namespace: "foo", HostName: "foo.com", ServiceAccount: sa, HealthStatus: health}
	}

	t.Run("stores the endpoints as given and pushes", func(t *testing.T) {
		endpoints := NewEndpointIndex(DisabledCache{})
		first := []*IstioEndpoint{endpoint("10.0.0.1", "", Healthy)}
		assert.Equal(t, endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo", first, true), IncrementalPush)

		second := []*IstioEndpoint{endpoint("10.0.0.1", "", Healthy), endpoint("10.0.0.2", "", Healthy)}
		assert.Equal(t, endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo", second, true), IncrementalPush)
		eps, _ := endpoints.ShardsForService("foo.com", "foo")
		assert.Equal(t, eps.Shards[shardKey], second)
	})

	t.Run("pushes for new endpoints that are not ready", func(t *testing.T) {
		// UpdateServiceEndpoints holds this push back; callers of ReplaceServiceEndpoints already decided
		// the endpoints changed.
		endpoints := NewEndpointIndex(DisabledCache{})
		endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo", []*IstioEndpoint{endpoint("10.0.0.1", "", Healthy)}, true)
		pushType := endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo",
			[]*IstioEndpoint{endpoint("10.0.0.1", "", Healthy), endpoint("10.0.0.2", "", UnHealthy)}, true)
		assert.Equal(t, pushType, IncrementalPush)
	})

	t.Run("service account changes require a full push", func(t *testing.T) {
		endpoints := NewEndpointIndex(DisabledCache{})
		assert.Equal(t, endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo",
			[]*IstioEndpoint{endpoint("10.0.0.1", "sa", Healthy)}, true), FullPush)
		assert.Equal(t, endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo",
			[]*IstioEndpoint{endpoint("10.0.0.2", "sa", Healthy)}, true), IncrementalPush)
		assert.Equal(t, endpoints.ReplaceServiceEndpoints(shardKey, "foo.com", "foo",
			[]*IstioEndpoint{endpoint("10.0.0.2", "sa2", Healthy)}, true), FullPush)
	})
}

// BenchmarkUpdateServiceEndpoints compares publishing a host whose single endpoint changed, out of n,
// with and without the per-endpoint diff. Unchanged endpoints keep their pointers, as krt hands them over.
func BenchmarkUpdateServiceEndpoints(b *testing.B) {
	// Info logs go to stdout, where they displace benchmark result lines.
	log.SetOutputLevel(istiolog.WarnLevel)
	shardKey := ShardKey{Cluster: "c1"}
	for _, n := range []int{100, 2000} {
		states := [2][]*IstioEndpoint{}
		for i := range n {
			ep := &IstioEndpoint{
				Addresses:       []string{fmt.Sprintf("10.0.%d.%d", i/256, i%256)},
				Namespace:       "foo",
				HostName:        "foo.com",
				WorkloadName:    fmt.Sprintf("wl-%d", i),
				ServicePortName: "http",
				ServiceAccount:  "sa",
			}
			states[0] = append(states[0], ep)
			states[1] = append(states[1], ep)
		}
		changed := states[1][n/2].ShallowCopy()
		changed.Labels = map[string]string{"version": "v2"}
		states[1][n/2] = changed

		for _, tc := range []struct {
			name   string
			update func(e *EndpointIndex, eps []*IstioEndpoint) PushType
		}{
			{"diff", func(e *EndpointIndex, eps []*IstioEndpoint) PushType {
				return e.UpdateServiceEndpoints(shardKey, "foo.com", "foo", eps, false)
			}},
			{"replace", func(e *EndpointIndex, eps []*IstioEndpoint) PushType {
				return e.ReplaceServiceEndpoints(shardKey, "foo.com", "foo", eps, false)
			}},
		} {
			b.Run(fmt.Sprintf("%s/endpoints=%d", tc.name, n), func(b *testing.B) {
				e := NewEndpointIndex(DisabledCache{})
				tc.update(e, states[0])
				state := 0
				b.ReportAllocs()
				for b.Loop() {
					state = 1 - state
					tc.update(e, states[state])
				}
			})
		}
	}
}
