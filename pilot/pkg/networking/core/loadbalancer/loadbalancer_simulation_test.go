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

// TestFailoverPriorityWithMultiLocalityDNSServiceEntry reproduces
// https://github.com/istio/istio/issues/61857: a DNS-resolution ServiceEntry whose endpoints span
// more than one locality, combined with a DestinationRule that enables locality failoverPriority
// and outlier detection, used to panic (index out of range) while building the STRICT_DNS
// cluster, since only the first locality group's LbEndpoints were paired with the flattened,
// all-locality IstioEndpoints list. It must build cleanly and keep every endpoint.
func TestFailoverPriorityWithMultiLocalityDNSServiceEntry(t *testing.T) {
	g := NewWithT(t)

	const config = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: search-external
  namespace: default
spec:
  hosts:
    - search.example.internal
  location: MESH_EXTERNAL
  resolution: DNS
  ports:
    - number: 8080
      name: http
      protocol: HTTP
  endpoints:
    - address: node-a.example.internal
      locality: region-a
    - address: node-b.example.internal
      locality: region-b
    - address: node-c.example.internal
      locality: region-c
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: search-external
  namespace: default
spec:
  host: search.example.internal
  trafficPolicy:
    outlierDetection:
      consecutive5xxErrors: 4
      interval: 30s
      baseEjectionTime: 30s
    loadBalancer:
      localityLbSetting:
        enabled: true
        failoverPriority:
          - topology.kubernetes.io/region
`
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: config,
	})

	proxy := &model.Proxy{
		Metadata: &model.NodeMetadata{},
		Labels: map[string]string{
			"topology.kubernetes.io/region": "region-a",
		},
	}

	// Building the clusters below must not panic.
	sim := simulation.NewSimulation(t, s, s.SetupProxy(proxy))

	clusterName := "outbound|8080||search.example.internal"
	var dnsCluster *cluster.Cluster
	for _, c := range sim.Clusters {
		if c.Name == clusterName {
			dnsCluster = c
			break
		}
	}
	g.Expect(dnsCluster).NotTo(BeNil(), "cluster %s not found", clusterName)
	g.Expect(dnsCluster.GetType()).To(Equal(cluster.Cluster_STRICT_DNS))

	// All three endpoints, across all three locality groups, must still be present: the fix must
	// not drop endpoints belonging to locality groups other than the first.
	var addrs []string
	for _, epGroup := range dnsCluster.LoadAssignment.Endpoints {
		for _, lbEp := range epGroup.LbEndpoints {
			addrs = append(addrs, lbEp.GetEndpoint().GetAddress().GetSocketAddress().GetAddress())
		}
	}
	g.Expect(addrs).To(ConsistOf("node-a.example.internal", "node-b.example.internal", "node-c.example.internal"))

	// The proxy is in region-a, so region-a's group should be the highest priority (0); the other
	// two groups should be lower (non-zero) priorities.
	for _, epGroup := range dnsCluster.LoadAssignment.Endpoints {
		for _, lbEp := range epGroup.LbEndpoints {
			addr := lbEp.GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
			if addr == "node-a.example.internal" {
				g.Expect(epGroup.Priority).To(Equal(uint32(0)), "region-a group should be highest priority")
			} else {
				g.Expect(epGroup.Priority).NotTo(Equal(uint32(0)), "%s should not be highest priority", addr)
			}
		}
	}
}
