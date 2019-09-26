// Copyright 2019 Istio Authors
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

package circuitbreakers

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"math"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2Cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/gomega"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	serviceEntryConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-svc-mongocluster
  namespace: test
spec:
  hosts:
    - mymongodb.somedomain
  location: MESH_EXTERNAL
  ports:
    - number: 80
      name: http
      protocol: HTTP
  resolution: DNS
`
	defaultCircuitBreakerThresholds = v2Cluster.CircuitBreakers_Thresholds{
		MaxRetries:         &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxRequests:        &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxConnections:     &wrappers.UInt32Value{Value: math.MaxUint32},
		MaxPendingRequests: &wrappers.UInt32Value{Value: math.MaxUint32},
	}

	g galley.Instance
	p pilot.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("circuitbreakers", m).
		RequireEnvironment(environment.Native).
		Setup(setup).
		Run()
}

func setup(ctx resource.Context) error {
	// Create the components.
	var err error
	g, err = galley.New(ctx, galley.Config{})
	if err != nil {
		return err
	}
	p, err = pilot.New(ctx, pilot.Config{
		Galley: g,
	})
	return err
}

func TestCircuitBreakers(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Apply configuration via Galley.
			g.ApplyConfigOrFail(ctx, nil, serviceEntryConfig)
			defer g.DeleteConfigOrFail(ctx, nil, serviceEntryConfig)

			proxy := &model.Proxy{
				ClusterID:       "integration-test",
				ID:              "httpbin-test",
				DNSDomain:       "test.cluster.local",
				Type:            model.SidecarProxy,
				IPAddresses:     []string{"1.1.1.1"},
				ConfigNamespace: "test",
			}

			discoveryRequest := &xdsapi.DiscoveryRequest{
				TypeUrl: v2.ClusterType,
				Node: &xdscore.Node{
					Id: proxy.ServiceNode(),
				},
			}
			p.StartDiscoveryOrFail(t, discoveryRequest)
			var clusters []xdsapi.Cluster
			p.WatchDiscoveryOrFail(t, time.Second*5,
				func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
					if len(response.Resources) < 4 {
						// if we only get the 3 default clusters, our config has not arrived yet
						return false, nil
					}
					for _, res := range response.Resources {
						cluster := xdsapi.Cluster{}
						if err := proto.Unmarshal(res.Value, &cluster); err != nil {
							t.Fatal(err)
						}
						clusters = append(clusters, cluster)
					}
					return true, nil
				},
			)
			gomega := NewWithT(t)
			gomega.Expect(len(clusters)).To(Equal(4))

			for _, c := range clusters {
				t.Logf("Processing cluster %v", c.Name)
				for _, thresholds := range c.GetCircuitBreakers().GetThresholds() {
					if c.Name != "BlackHoleCluster" && c.Name[:7] != "inbound" {
						gomega.Expect(*thresholds).To(Equal(defaultCircuitBreakerThresholds))
					}
				}
			}
		})
}
