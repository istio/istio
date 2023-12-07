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
)

func TestUpdateServiceAccount(t *testing.T) {
	var (
		c1Key = ShardKey{Cluster: "c1"}
		c2Key = ShardKey{Cluster: "c2"}
	)

	cluster1Endppoints := []*IstioEndpoint{
		{Address: "10.172.0.1", ServiceAccount: "sa1"},
		{Address: "10.172.0.2", ServiceAccount: "sa-vm1"},
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
			endpoints: append(cluster1Endppoints, &IstioEndpoint{Address: "10.172.0.3", ServiceAccount: "sa1"}),
			expect:    false,
		},
		{
			name:      "added new sa",
			shardKey:  c1Key,
			endpoints: append(cluster1Endppoints, &IstioEndpoint{Address: "10.172.0.3", ServiceAccount: "sa2"}),
			expect:    true,
		},
		{
			name:     "updated endpoints address",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Address: "10.172.0.5", ServiceAccount: "sa1"},
				{Address: "10.172.0.2", ServiceAccount: "sa-vm1"},
			},
			expect: false,
		},
		{
			name:     "deleted one endpoint with unique sa",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Address: "10.172.0.1", ServiceAccount: "sa1"},
			},
			expect: true,
		},
		{
			name:     "deleted one endpoint with duplicate sa",
			shardKey: c1Key,
			endpoints: []*IstioEndpoint{
				{Address: "10.172.0.2", ServiceAccount: "sa-vm1"},
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
					c1Key: cluster1Endppoints,
					c2Key: {{Address: "10.244.0.1", ServiceAccount: "sa1"}, {Address: "10.244.0.2", ServiceAccount: "sa-vm2"}},
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
