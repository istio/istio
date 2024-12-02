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
