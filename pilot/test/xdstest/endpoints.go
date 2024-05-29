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

package xdstest

import (
	"fmt"
	"sort"
	"testing"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"istio.io/istio/pilot/pkg/networking/util"
)

type LbEpInfo struct {
	Address string
	// nolint: structcheck
	Weight uint32
}

type LocLbEpInfo struct {
	LbEps  []LbEpInfo
	Weight uint32
}

func (i LocLbEpInfo) GetAddrs() []string {
	addrs := make([]string, 0)
	for _, ep := range i.LbEps {
		addrs = append(addrs, ep.Address)
	}
	return addrs
}

func CompareEndpointsOrFail(t *testing.T, cluster string, got []*endpointv3.LocalityLbEndpoints, want []LocLbEpInfo) {
	if err := CompareEndpoints(cluster, got, want); err != nil {
		t.Error(err)
	}
}

func CompareEndpoints(cluster string, got []*endpointv3.LocalityLbEndpoints, want []LocLbEpInfo) error {
	if len(got) != len(want) {
		return fmt.Errorf("unexpected number of filtered endpoints for %s: got %v, want %v", cluster, len(got), len(want))
	}

	sort.Slice(got, func(i, j int) bool {
		addrI := util.GetEndpointHost(got[i].LbEndpoints[0])
		addrJ := util.GetEndpointHost(got[j].LbEndpoints[0])
		return addrI < addrJ
	})

	for i, ep := range got {
		if len(ep.LbEndpoints) != len(want[i].LbEps) {
			return fmt.Errorf("unexpected number of LB endpoints within endpoint %d: %v, want %v",
				i, getLbEndpointAddrs(ep), want[i].GetAddrs())
		}

		if ep.LoadBalancingWeight.GetValue() != want[i].Weight {
			return fmt.Errorf("unexpected weight for endpoint %d: got %v, want %v", i, ep.LoadBalancingWeight.GetValue(), want[i].Weight)
		}

		for _, lbEp := range ep.LbEndpoints {
			addr := util.GetEndpointHost(lbEp)
			found := false
			for _, wantLbEp := range want[i].LbEps {
				if addr == wantLbEp.Address {
					found = true

					// Now compare the weight.
					if lbEp.GetLoadBalancingWeight().Value != wantLbEp.Weight {
						return fmt.Errorf("unexpected weight for endpoint %s: got %v, want %v",
							addr, lbEp.GetLoadBalancingWeight().Value, wantLbEp.Weight)
					}
					break
				}
			}
			if !found {
				return fmt.Errorf("unexpected address for endpoint %d: %v", i, addr)
			}
		}
	}
	return nil
}

func getLbEndpointAddrs(ep *endpointv3.LocalityLbEndpoints) []string {
	addrs := make([]string, 0)
	for _, lbEp := range ep.LbEndpoints {
		addrs = append(addrs, util.GetEndpointHost(lbEp))
	}
	return addrs
}
