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
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/structpath"
)

func TestSidecarListeners(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {

			// Simulate proxy identity of a sidecar ...
			nodeID := &model.Proxy{
				Metadata:     &model.NodeMetadata{ClusterID: "integration-test"},
				Type:         model.SidecarProxy,
				IPAddresses:  []string{"10.2.0.1"},
				ID:           "app3.testns",
				DNSDomain:    "testns.cluster.local",
				IstioVersion: model.MaxIstioVersion,
			}

			// Start the xDS stream containing the listeners for this node
			p.StartDiscoveryOrFail(t, pilot.NewDiscoveryRequest(nodeID.ServiceNode(), pilot.Listener))

			// Test the empty case where no config is loaded
			p.WatchDiscoveryOrFail(t, time.Second*10,
				func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
					validator := structpath.ForProto(response)
					if validator.Select("{.resources[?(@.address.socketAddress.portValue==%v)]}", 15001).Check() != nil {
						return false, nil
					}
					validateListenersNoConfig(t, validator)
					return true, nil
				})

			namespace.ClaimOrFail(ctx, ctx, "seexamples")
			// Apply some config
			config := mustReadFile(t, "../../config/se-example.yaml")
			ctx.ApplyConfigOrFail(t, "", config)
			defer func() {
				if err := ctx.DeleteConfig("", config); err != nil {
					scopes.Framework.Errorf("failed to delete directory: %v", err)
				}
			}()

			// Now continue to watch on the same stream
			err := p.WatchDiscovery(time.Second*10,
				func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
					validator := structpath.ForProto(response)
					if validator.Select("{.resources[?(@.address.socketAddress.portValue==27018)]}").Check() != nil {
						return false, nil
					}
					validateMongoListener(t, validator)
					return true, nil
				})
			if err != nil {
				t.Fatalf("Failed to test as no resource accepted: %v", err)
			}
		})
}

func validateListenersNoConfig(t *testing.T, response *structpath.Instance) {
	t.Run("iptables-forwarding-listener", func(t *testing.T) {
		response.
			Select("{.resources[?(@.address.socketAddress.portValue==15001)]}").
			Equals("virtualOutbound", "{.name}").
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			Equals("envoy.tcp_proxy", "{.filterChains[0].filters[0].name}").
			Equals("PassthroughCluster", "{.filterChains[0].filters[0].typedConfig.cluster}").
			Equals("PassthroughCluster", "{.filterChains[0].filters[0].typedConfig.statPrefix}").
			Equals(true, "{.useOriginalDst}").
			CheckOrFail(t)
	})
}

func validateMongoListener(t *testing.T, response *structpath.Instance) {
	t.Run("validate-mongo-listener", func(t *testing.T) {
		mixerListener := response.
			Select("{.resources[?(@.address.socketAddress.portValue==%v)]}", 27018)

		mixerListener.
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			// Example doing a struct comparison, note the pain with oneofs....
			Equals(&xdscore.SocketAddress{
				Address: "0.0.0.0",
				PortSpecifier: &xdscore.SocketAddress_PortValue{
					PortValue: uint32(27018),
				},
			}, "{.address.socketAddress}").
			Select("{.filterChains[0].filters[0]}").
			Equals("envoy.mongo_proxy", "{.name}").
			Select("{.typedConfig}").
			Exists("{.statPrefix}").
			CheckOrFail(t)
	})
}
