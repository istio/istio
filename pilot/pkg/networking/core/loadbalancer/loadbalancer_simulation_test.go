// Copyright Istio Authors. All Rights Reserved.
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

package loadbalancer_test

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/loadbalancer"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/test/xds"
)

// TestFailoverPriorityWithDNSServiceEntry tests failover priority using a real ServiceEntry
// converted through the full xDS pipeline. This validates that the cluster structure
// produced from a DNS ServiceEntry works correctly with ApplyLocalityLoadBalancer.
func TestFailoverPriorityWithDNSServiceEntry(t *testing.T) {
	g := NewWithT(t)

	// ServiceEntry with DNS resolution and two endpoints with different priority labels
	const dnsServiceEntry = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: dns-service
  namespace: default
spec:
  hosts:
  - dns-service.example.org
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: www.foo.com
    labels:
      priority: "1"
  - address: www.bar.com
    labels:
      priority: "2"
`
	// Create fake discovery server with the ServiceEntry
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: dnsServiceEntry,
	})

	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{},
	}
	sim := simulation.NewSimulation(t, s, s.SetupProxy(proxy))

	// Find the cluster for the DNS service
	clusterName := "outbound|443||dns-service.example.org"
	var dnsCluster *cluster.Cluster
	for _, c := range sim.Clusters {
		if c.Name == clusterName {
			dnsCluster = c
			break
		}
	}

	if dnsCluster == nil {
		t.Fatalf("cluster %s not found", clusterName)
	}

	// Validate initial structure: STRICT_DNS with 1 LocalityLbEndpoints containing 2 LbEndpoints
	g.Expect(dnsCluster.GetType()).To(Equal(cluster.Cluster_STRICT_DNS))
	g.Expect(dnsCluster.LoadAssignment.Endpoints).To(HaveLen(1))
	g.Expect(dnsCluster.LoadAssignment.Endpoints[0].LbEndpoints).To(HaveLen(2))

	// Get IstioEndpoints from the push context to build WrappedLocalityLbEndpoints
	push := s.PushContext()
	svc := push.ServiceForHostname(proxy, "dns-service.example.org")
	if svc == nil {
		t.Fatal("service not found")
	}

	istioEndpoints := push.ServiceEndpointsByPort(svc, 443, nil)
	g.Expect(istioEndpoints).To(HaveLen(2))

	// Build WrappedLocalityLbEndpoints matching how it's done in cluster.go for DNS clusters
	wrappedEndpoints := []*loadbalancer.WrappedLocalityLbEndpoints{
		{
			IstioEndpoints:      istioEndpoints,
			LocalityLbEndpoints: dnsCluster.LoadAssignment.Endpoints[0],
		},
	}

	// Setup locality and mesh config with failoverPriority
	locality := &core.Locality{
		Region:  "region1",
		Zone:    "zone1",
		SubZone: "subzone1",
	}
	proxyLabels := map[string]string{
		"priority": "1", // Matches www.foo.com's label
	}
	localityLbSetting := &networking.LocalityLoadBalancerSetting{
		FailoverPriority: []string{"priority"},
	}

	// Apply locality load balancer with failover priority
	loadbalancer.ApplyLocalityLoadBalancer(
		dnsCluster.LoadAssignment,
		wrappedEndpoints,
		locality,
		proxyLabels,
		localityLbSetting,
		true, // enable failover
	)

	// Validate that endpoints are now split by priority
	// - www.foo.com (priority=1, matches proxy) should be Priority 0
	// - www.bar.com (priority=2, doesn't match) should be Priority 1
	g.Expect(dnsCluster.LoadAssignment.Endpoints).To(HaveLen(2),
		"endpoints should be split into 2 groups by failover priority")

	// Find endpoint groups by hostname
	var fooGroup, barGroup *endpoint.LocalityLbEndpoints
	for _, epGroup := range dnsCluster.LoadAssignment.Endpoints {
		if len(epGroup.LbEndpoints) == 0 {
			continue
		}
		addr := epGroup.LbEndpoints[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
		switch addr {
		case "www.foo.com":
			fooGroup = epGroup
		case "www.bar.com":
			barGroup = epGroup
		}
	}

	g.Expect(fooGroup).NotTo(BeNil(), "www.foo.com endpoint group not found")
	g.Expect(barGroup).NotTo(BeNil(), "www.bar.com endpoint group not found")

	// www.foo.com should have priority 0 (matches failoverPriority label)
	g.Expect(fooGroup.Priority).To(Equal(uint32(0)),
		"www.foo.com should have priority 0 (matches proxy label priority=1)")

	// www.bar.com should have priority 1 (doesn't match failoverPriority label)
	g.Expect(barGroup.Priority).To(Equal(uint32(1)),
		"www.bar.com should have priority 1 (doesn't match proxy label)")

	t.Logf("Failover priority test passed: foo.com (priority=1) -> Priority 0, bar.com (priority=2) -> Priority 1")
}
