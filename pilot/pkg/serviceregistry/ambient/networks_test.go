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

package ambient

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test/util/assert"
)

func TestLookupNetworkGatewayOrder(t *testing.T) {
	gws := make([]NetworkGateway, 0, 8)
	for i := range 8 {
		gws = append(gws, NetworkGateway{
			NetworkGateway: model.NetworkGateway{
				Network:   "nw1",
				Addr:      "10.0.0." + string(rune('0'+i)),
				HBONEPort: 15008,
			},
			Source: types.NamespacedName{Namespace: "istio-system", Name: "gw" + string(rune('0'+i))},
		})
	}
	c := krt.NewStaticCollection(nil, gws, krt.WithDebugging(krt.GlobalDebugHandler))
	byNetwork := krt.NewIndex(c, "network", func(o NetworkGateway) []network.ID {
		return []network.ID{o.Network}
	})
	first := LookupNetworkGateway(krt.TestingDummyContext{}, "nw1", byNetwork)
	assert.Equal(t, len(first), len(gws))
	for i := 1; i < len(first); i++ {
		if first[i-1].ResourceName() >= first[i].ResourceName() {
			t.Fatalf("result not sorted: %v before %v", first[i-1].ResourceName(), first[i].ResourceName())
		}
	}
	// an index lookup iterates a map; the order must not change between calls
	for range 100 {
		assert.Equal(t, LookupNetworkGateway(krt.TestingDummyContext{}, "nw1", byNetwork), first)
	}
}
