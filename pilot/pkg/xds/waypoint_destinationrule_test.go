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

package xds_test

import (
	"strings"
	"testing"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

// waypointEDS returns the endpoints for cluster from both the EDS generator and the endpoint
// builder, which look up the DestinationRule separately.
func waypointEDS(t *testing.T, d *xds.FakeDiscoveryServer, proxy *model.Proxy, cluster string) map[string][]string {
	t.Helper()
	w := &model.WatchedResource{TypeUrl: v3.EndpointType, ResourceNames: sets.New(cluster)}
	res, _, err := d.Discovery.Generators[v3.EndpointType].Generate(proxy, w, &model.PushRequest{Forced: true, Push: d.PushContext()})
	if err != nil {
		t.Fatal(err)
	}
	clas := make([]*endpoint.ClusterLoadAssignment, 0, len(res))
	for _, r := range res {
		cla := &endpoint.ClusterLoadAssignment{}
		if err := r.Resource.UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		clas = append(clas, cla)
	}
	return map[string][]string{
		"generator": slices.Sort(xdstest.ExtractLoadAssignments(clas)[cluster]),
		"builder":   slices.Sort(xdstest.ExtractLoadAssignments(d.Endpoints(proxy))[cluster]),
	}
}

// TestWaypointInboundVIPDestinationRule checks that waypoint EDS picks the same DestinationRule
// policy as CDS for inbound-vip clusters.
func TestWaypointInboundVIPDestinationRule(t *testing.T) {
	allEndpoints := []string{
		"1.1.1.3:80,[2001:20::3]:80",
		"connect_originate;1.1.1.1:80",
		"connect_originate;1.1.1.2:80",
	}
	httpCluster := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "app.com", 80)
	tcpCluster := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "tcp", "app.com", 80)
	tcpServiceEntry := strings.ReplaceAll(appServiceEntry, "protocol: HTTP", "protocol: TCP")
	subsetNamed := func(name string) string {
		return `apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: app
  namespace: default
spec:
  host: app.com
  subsets:
  - name: ` + name + `
    labels:
      unused: "true"
`
	}
	for _, tt := range []struct {
		name    string
		se      string
		dr      string
		cluster string
		want    []string
	}{
		{
			// waypoint inbound policy ignores workload selectors, so EDS must use the subset CDS was built from
			name: "workload selector matching waypoint",
			se:   appServiceEntry,
			dr: `apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: app
  namespace: default
spec:
  host: app.com
  subsets:
  - name: v1
    labels:
      app: app
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: app-waypoint
  namespace: default
spec:
  host: app.com
  workloadSelector:
    matchLabels:
      gateway.networking.k8s.io/gateway-name: waypoint
  subsets:
  - name: v1
    labels:
      unused: "true"
`,
			cluster: model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http/v1", "app.com", 80),
			want:    allEndpoints,
		},
		{
			name:    "default http cluster with subset named http",
			se:      appServiceEntry,
			dr:      subsetNamed("http"),
			cluster: httpCluster,
			want:    allEndpoints,
		},
		{
			name:    "subset named http still filters",
			se:      appServiceEntry,
			dr:      subsetNamed("http"),
			cluster: model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http/http", "app.com", 80),
			want:    []string{},
		},
		{
			name:    "default tcp cluster with subset named tcp",
			se:      tcpServiceEntry,
			dr:      subsetNamed("tcp"),
			cluster: tcpCluster,
			want:    allEndpoints,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, proxy := setupWaypointTest(t,
				waypointGateway, waypointSvc, waypointInstance,
				appPod, appPodNoMesh, appWorkloadEntry, tt.se, tt.dr)
			if xdstest.ExtractClusters(d.Clusters(proxy))[tt.cluster] == nil {
				t.Fatalf("CDS missing cluster %s", tt.cluster)
			}
			for path, got := range waypointEDS(t, d, proxy, tt.cluster) {
				t.Run(path, func(t *testing.T) {
					if got == nil {
						got = []string{}
					}
					assert.Equal(t, got, tt.want)
				})
			}
		})
	}
}
