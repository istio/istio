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

package pilot

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/gogo/protobuf/proto"

	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"
	"istio.io/istio/tests/integration/pilot/sidecarscope"

	"istio.io/istio/pkg/test/framework"
)

func TestServiceEntryDNS(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		configFn := func(c sidecarscope.Config) sidecarscope.Config {
			c.Resolution = "DNS"
			return c
		}
		p, nodeID := sidecarscope.SetupTest(t, ctx, configFn)

		req := &discovery.DiscoveryRequest{
			Node: &core.Node{
				Id: nodeID.ServiceNode(),
			},
			TypeUrl: v3.ClusterType,
		}

		if err := p.StartDiscovery(req); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(time.Second*5, checkEndpoint("app.com")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestServiceEntryDNSNoSelfImport(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		configFn := func(c sidecarscope.Config) sidecarscope.Config {
			c.Resolution = "DNS"
			c.ImportedNamespaces = []string{c.IncludedNamespace + "/*"}
			return c
		}
		p, nodeID := sidecarscope.SetupTest(t, ctx, configFn)

		req := &discovery.DiscoveryRequest{
			Node: &core.Node{
				Id: nodeID.ServiceNode(),
			},
			TypeUrl: v3.ClusterType,
		}

		if err := p.StartDiscovery(req); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(time.Second*5, checkEndpoint("included.com")); err != nil {
			t.Fatal(err)
		}
	})
}

func checkEndpoint(name string) func(resp *discovery.DiscoveryResponse) (success bool, e error) {
	return func(resp *discovery.DiscoveryResponse) (success bool, e error) {
		return checkResultDNS(name, resp)
	}
}

func checkResultDNS(expect string, resp *discovery.DiscoveryResponse) (success bool, e error) {
	expected := map[string]int{
		expect: 1,
	}

	for _, res := range resp.Resources {
		c := &cluster.Cluster{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}
		if c.Name != "outbound|80||app.com" {
			continue
		}

		got := map[string]int{}
		for _, ep := range c.LoadAssignment.Endpoints {
			for _, lb := range ep.LbEndpoints {
				got[lb.GetEndpoint().Address.GetSocketAddress().Address]++
			}
		}
		if !reflect.DeepEqual(expected, got) {
			return false, fmt.Errorf("excepted load assignments %+v, got %+v", expected, got)
		}
	}
	return true, nil
}
