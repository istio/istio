//go:build integ

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

package zoneawarelb

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

const (
	// localLocality must match the locality of the calling proxy (apps.A defaults to
	// "region.zone.subzone" in the standard echo deployment).
	localLocality  = "region/zone/subzone"
	remoteLocality = "notregion/notzone/notsubzone"

	// localClusterName matches xds.LocalClusterName in pilot. We hard-code it here so the
	// integration test stays decoupled from pilot internals, but a mismatch should fail
	// the static-cluster assertion below — which is the whole point.
	localClusterName = "local_cluster"

	sendCount = 50
)

// Zone-aware-routing configuration:
//   - LocalSvc: a ServiceEntry whose endpoint matches the calling proxy's address.
//     This makes the proxy's local_cluster (the static cluster injected when
//     ISTIO_META_ENABLE_SELF_DISCOVERY=true) resolve to a real service, exercising
//     that side of the feature.
//   - ZoneAwareSvc: the target ServiceEntry that selects WorkloadEntries by label.
//     The DR enables ZoneAwareLbSetting with MinClusterSize=1 so Envoy zone-aware
//     LB engages even with a single endpoint per zone.
const zoneAwareConfig = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: local-svc
spec:
  hosts: ["{{ .LocalHost }}"]
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: STATIC
  endpoints:
  - address: {{ .ProxyAddress }}
    locality: {{ .LocalLocality }}
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: zone-aware-svc
spec:
  hosts: ["{{ .RemoteHost }}"]
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: zone-aware-backend
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: zone-aware-dr
spec:
  host: "{{ .RemoteHost }}"
  trafficPolicy:
    connectionPool:
      tcp:
        connectTimeout: 250ms
    outlierDetection:
      interval: 1s
      baseEjectionTime: 10m
      maxEjectionPercent: 100
    loadBalancer:
      zoneAwareLbSetting:
        enabled: true
        minClusterSize: 1
{{ range $i, $we := .Workloads }}
---
apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: zone-aware-we-{{ $i }}
spec:
  address: {{ $we.Address }}
  locality: {{ $we.Locality }}
  labels:
    app: zone-aware-backend
{{ end }}
`

type weEntry struct {
	Address  string
	Locality string
}

type zoneAwareInput struct {
	LocalHost     string
	RemoteHost    string
	ProxyAddress  string
	LocalLocality string
	Workloads     []weEntry
}

func TestZoneAwareLoadBalancer(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			caller := apps.A[0]
			destB := apps.B[0]
			destC := apps.C[0]
			proxyAddress := caller.WorkloadsOrFail(t)[0].Address()

			cases := []struct {
				name      string
				workloads []weEntry
				expected  map[string]int
			}{
				{
					name: "LocalEndpointAttractsTraffic",
					workloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: remoteLocality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					name: "MultipleLocalEndpoints",
					workloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: remoteLocality},
						{Address: destC.Address(), Locality: remoteLocality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					name: "NoLocalFailsOverToRemote",
					workloads: []weEntry{
						{Address: destC.Address(), Locality: remoteLocality},
					},
					expected: expectAllTrafficTo(destC.Config().Service),
				},
				{
					name: "UnreachableLocalEjectsToRemote",
					workloads: []weEntry{
						{Address: "10.99.99.99", Locality: localLocality},
						{Address: destC.Address(), Locality: remoteLocality},
					},
					expected: expectAllTrafficTo(destC.Config().Service),
				},
			}

			for _, tc := range cases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					hostSuffix := strings.ToLower(strings.ReplaceAll(tc.name, "/", "-"))
					input := zoneAwareInput{
						LocalHost:     fmt.Sprintf("local-%s.example.com", hostSuffix),
						RemoteHost:    fmt.Sprintf("zone-aware-%s.example.com", hostSuffix),
						ProxyAddress:  proxyAddress,
						LocalLocality: localLocality,
						Workloads:     tc.workloads,
					}
					t.ConfigIstio().
						Eval(apps.Namespace.Name(), input, zoneAwareConfig).
						ApplyOrFail(t)

					// The traffic-distribution assertions below are not sufficient on their own:
					// locality-LB defaults can produce the same pattern. We first verify that
					// the proxy's Envoy config is actually zone-aware by asserting:
					//   1. local_cluster is present as a static cluster
					//   2. local_cluster has at least one endpoint (CLA populated via EDS)
					//   3. the remote-host cluster uses zone_aware_lb_config, not locality_weighted
					// All three must hold for zone-aware to be actually exercised at runtime.
					assertZoneAwareConfig(t, caller, input.RemoteHost)

					sendTrafficOrFail(t, caller, input.RemoteHost, tc.expected)
				})
			}
		})
}

// assertZoneAwareConfig waits for the caller's Envoy config to reflect zone-aware routing.
// Envoy's /config_dump endpoint omits endpoint data by default, so we use two separate
// admin queries: /config_dump for cluster definitions (LocalityConfigSpecifier) and
// /clusters?format=json for runtime host status (proves local_cluster is populated).
func assertZoneAwareConfig(t framework.TestContext, caller echo.Instance, remoteHost string) {
	t.Helper()
	sidecar := caller.WorkloadsOrFail(t)[0].Sidecar()
	remoteClusterName := fmt.Sprintf("outbound|80||%s", remoteHost)

	// First: assert cluster definitions are correct.
	sidecar.WaitForConfigOrFail(t, func(cd *admin.ConfigDump) (bool, error) {
		clusters, err := extractClusters(cd)
		if err != nil {
			return false, err
		}

		var local, remote *cluster.Cluster
		for _, c := range clusters {
			switch c.GetName() {
			case localClusterName:
				local = c
			case remoteClusterName:
				remote = c
			}
		}
		if local == nil {
			return false, fmt.Errorf("static cluster %q not found in proxy config — "+
				"ISTIO_META_ENABLE_SELF_DISCOVERY did not propagate to sidecar bootstrap", localClusterName)
		}
		if remote == nil {
			return false, fmt.Errorf("dynamic cluster %q not yet present", remoteClusterName)
		}

		switch remote.GetCommonLbConfig().GetLocalityConfigSpecifier().(type) {
		case *cluster.Cluster_CommonLbConfig_ZoneAwareLbConfig_:
			// ok
		case *cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig_:
			return false, fmt.Errorf("cluster %q has LocalityWeightedLbConfig — istiod did not "+
				"emit ZoneAwareLbConfig (is ISTIO_META_ENABLE_SELF_DISCOVERY set in proxyMetadata?)", remoteClusterName)
		default:
			return false, fmt.Errorf("cluster %q has unexpected LocalityConfigSpecifier %T — expected ZoneAwareLbConfig",
				remoteClusterName, remote.GetCommonLbConfig().GetLocalityConfigSpecifier())
		}
		return true, nil
	}, retry.Delay(time.Second), retry.Timeout(30*time.Second))

	// Second: assert local_cluster has live hosts via /clusters (config_dump excludes EDS data).
	retry.UntilSuccessOrFail(t, func() error {
		clusters, err := sidecar.Clusters()
		if err != nil {
			return err
		}
		for _, cs := range clusters.GetClusterStatuses() {
			if cs.GetName() != localClusterName {
				continue
			}
			if len(cs.GetHostStatuses()) == 0 {
				return fmt.Errorf("cluster %q has 0 hosts — local_cluster EDS never populated", localClusterName)
			}
			return nil
		}
		return fmt.Errorf("cluster %q not present in /clusters output", localClusterName)
	}, retry.Delay(time.Second), retry.Timeout(30*time.Second))
}

func extractClusters(cd *admin.ConfigDump) ([]*cluster.Cluster, error) {
	var out []*cluster.Cluster
	for _, c := range cd.GetConfigs() {
		if c.GetTypeUrl() != "type.googleapis.com/envoy.admin.v3.ClustersConfigDump" {
			continue
		}
		dump := &admin.ClustersConfigDump{}
		if err := c.UnmarshalTo(dump); err != nil {
			return nil, err
		}
		for _, sc := range dump.StaticClusters {
			ct := &cluster.Cluster{}
			if sc.Cluster != nil && sc.Cluster.UnmarshalTo(ct) == nil {
				out = append(out, ct)
			}
		}
		for _, dc := range dump.DynamicActiveClusters {
			ct := &cluster.Cluster{}
			if dc.Cluster != nil && dc.Cluster.UnmarshalTo(ct) == nil {
				out = append(out, ct)
			}
		}
	}
	return out, nil
}

func expectAllTrafficTo(dest string) map[string]int {
	return map[string]int{dest: sendCount}
}

func sendTrafficOrFail(t framework.TestContext, from echo.Instance, host string, expected map[string]int) {
	t.Helper()
	headers := http.Header{}
	headers.Add("Host", host)
	checker := func(result echo.CallResult, inErr error) error {
		if inErr != nil {
			return inErr
		}
		got := map[string]int{}
		for _, r := range result.Responses {
			parts := strings.SplitN(r.Hostname, "-", 2)
			if len(parts) < 2 {
				return fmt.Errorf("unexpected hostname: %v", r)
			}
			got[parts[0]]++
		}
		scopes.Framework.Infof("Got responses: %+v", got)
		for svc, reqs := range got {
			if !common.AlmostEquals(reqs, expected[svc], 3) {
				return fmt.Errorf("unexpected request distribution. Expected: %+v, got: %+v", expected, got)
			}
		}
		return nil
	}
	_ = from.CallOrFail(t, echo.CallOptions{
		To: from,
		Port: echo.Port{
			Name: "http",
		},
		HTTP: echo.HTTP{
			Headers: headers,
		},
		Count: sendCount,
		Check: checker,
	})
}
