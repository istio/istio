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

package pilot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	// localLocality must match the locality of the calling proxy (apps.A defaults to
	// "region.zone.subzone" in the standard echo deployment).
	localLocality           = "region/zone/subzone"
	sameRegionZone2Locality = "region/zone2/subzone"
	sameRegionZone3Locality = "region/zone3/subzone"
	sameRegionZone4Locality = "region/zone4/subzone"
	remoteRegionLocality    = "notregion/notzone/notsubzone"

	// localClusterName matches xds.LocalClusterName in pilot. We hard-code it here so the
	// integration test stays decoupled from pilot internals, but a mismatch should fail
	// the static-cluster assertion below — which is the whole point.
	localClusterName = "local_cluster"
)

// Zone-aware-routing configuration:
//   - LocalSvc: a ServiceEntry whose endpoints model the caller service's
//     source-zone distribution.
//     This makes the proxy's local_cluster (the static cluster injected when
//     ISTIO_META_ENABLE_SELF_DISCOVERY=true) resolve to a real service, exercising
//     that side of the feature.
//   - ZoneAwareSvc: the target ServiceEntry that selects WorkloadEntries by label.
//     The DR enables ZoneAwareLbSetting with a configurable MinClusterSize (default 1,
//     so Envoy zone-aware LB engages even with a single endpoint per zone). Raising it
//     above the destination cluster's host count makes Envoy disable zone-aware routing.
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
  workloadSelector:
    labels:
      app: zone-aware-local
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
{{ if .WithOutlierDetection }}
    outlierDetection:
      interval: 1s
      baseEjectionTime: 10m
      maxEjectionPercent: 100
{{ end }}
{{ if not .DisableZoneAware }}
    loadBalancer:
      zoneAwareLbSetting:
        enabled: true
        minClusterSize: {{ .MinClusterSize }}
{{ end }}
{{ range $i, $we := .LocalClusterWorkloads }}
---
apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: zone-aware-local-we-{{ $i }}
  labels:
    service.istio.io/workload-name: {{ $.LocalClusterWorkloadName }}
spec:
  address: {{ $we.Address }}
  locality: {{ $we.Locality }}
  labels:
    app: zone-aware-local
{{ range $k, $v := $.LocalClusterLabels }}
    {{ $k }}: {{ $v | quote }}
{{ end }}
{{ end }}
{{ range $i, $we := .DestinationWorkloads }}
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
	LocalHost                string
	RemoteHost               string
	LocalClusterWorkloadName string
	LocalClusterLabels       map[string]string
	LocalClusterWorkloads    []weEntry
	DestinationWorkloads     []weEntry
	WithOutlierDetection     bool
	MinClusterSize           int
	DisableZoneAware         bool
}

func TestZoneAwareLoadBalancer(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			// Deploy a dedicated caller with ISTIO_META_ENABLE_SELF_DISCOVERY=true so that
			// only this proxy gets local_cluster injected, without a global mesh config change.
			callerInstances := deployment.New(t).
				WithConfig(echo.Config{
					Service:   "zone-aware-caller",
					Namespace: apps.Namespace,
					Locality:  "region.zone.subzone",
					Ports:     ports.All(),
					Subsets: []echo.SubsetConfig{{
						Annotations: map[string]string{
							annotation.ProxyConfig.Name: `{"proxyMetadata":{"ISTIO_META_ENABLE_SELF_DISCOVERY":"true"}}`,
						},
					}},
				}).
				BuildOrFail(t)
			caller := callerInstances[0]
			destB := apps.B[0]
			destC := apps.C[0]
			proxyAddress := caller.WorkloadsOrFail(t)[0].Address()
			sourcePeerAddress := destB.WorkloadsOrFail(t)[0].Address()
			callerWorkloadName := workloadNameForEcho(caller)
			callerLocalClusterLabels := localClusterLabelsForEcho(t, caller)
			oneLocalClusterEndpoint := []weEntry{
				{Address: proxyAddress, Locality: localLocality},
			}
			twoLocalClusterZones := []weEntry{
				{Address: proxyAddress, Locality: localLocality},
				{Address: sourcePeerAddress, Locality: sameRegionZone2Locality},
			}

			cases := []struct {
				name                  string
				localClusterWorkloads []weEntry
				destinationWorkloads  []weEntry
				withOutlierDetection  bool
				minClusterSize        int
				disableZoneAware      bool
				expected              map[string]int
			}{
				{
					name:                  "OneSourceEndpointOneDestinationEndpointSameLocality",
					localClusterWorkloads: oneLocalClusterEndpoint,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					name:                  "OneSourceEndpointTwoDestinationLocalitiesSameRegionOverflows",
					localClusterWorkloads: oneLocalClusterEndpoint,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: sameRegionZone2Locality},
					},
					expected: map[string]int{
						destB.Config().Service: sendCount / 2,
						destC.Config().Service: sendCount / 2,
					},
				},
				{
					name:                  "TwoSourceZonesTwoDestinationEndpointsMatchingSourceZones",
					localClusterWorkloads: twoLocalClusterZones,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: sameRegionZone2Locality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					// Same topology as TwoSourceZonesTwoDestinationEndpointsMatchingSourceZones, but
					// with no loadBalancer DR block (default locality LB, no outlier detection).
					// Without zone-aware routing there is no mechanism to pin traffic to the local
					// zone: no priorities are assigned and both same-region endpoints stay equal,
					// so traffic splits evenly.
					name:                  "ZoneAwareDisabledTwoSourceZonesTwoDestinationEndpointsMatchingSourceZones",
					localClusterWorkloads: twoLocalClusterZones,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: sameRegionZone2Locality},
					},
					disableZoneAware: true,
					expected: map[string]int{
						destB.Config().Service: sendCount / 2,
						destC.Config().Service: sendCount / 2,
					},
				},
				{
					// Same setup as TwoSourceZonesTwoDestinationEndpointsMatchingSourceZones, which
					// normally pins all traffic to the same-zone endpoint (destB). Raising
					// minClusterSize above the destination cluster's host count makes Envoy disable
					// zone-aware routing at runtime (the cluster is smaller than the configured
					// threshold), so traffic falls back to an even split across both same-region zones.
					// istiod still emits ZoneAwareLbConfig, so assertZoneAwareConfig continues to pass;
					// only the runtime distribution changes.
					name:                  "HighMinClusterSizeDisablesZoneAwareRouting",
					localClusterWorkloads: twoLocalClusterZones,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: sameRegionZone2Locality},
					},
					minClusterSize: 10,
					expected: map[string]int{
						destB.Config().Service: sendCount / 2,
						destC.Config().Service: sendCount / 2,
					},
				},
				{
					name:                  "TwoSourceZonesTwoDestinationEndpointsDifferentSourceZones",
					localClusterWorkloads: twoLocalClusterZones,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: sameRegionZone3Locality},
						{Address: destC.Address(), Locality: sameRegionZone4Locality},
					},
					expected: map[string]int{
						destB.Config().Service: sendCount / 2,
						destC.Config().Service: sendCount / 2,
					},
				},
				{
					name:                  "CrossRegionEndpointIgnoredWhileSameRegionHealthy",
					localClusterWorkloads: oneLocalClusterEndpoint,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: remoteRegionLocality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					// Same topology as CrossRegionEndpointIgnoredWhileSameRegionHealthy, but with
					// no loadBalancer DR block (default locality LB, no outlier detection).
					// Without zone-aware region-bucketing, both endpoints are at equal priority
					// and traffic splits evenly across the same-region and cross-region endpoints.
					name:                  "ZoneAwareDisabledCrossRegionEndpointNotIgnored",
					localClusterWorkloads: oneLocalClusterEndpoint,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: remoteRegionLocality},
					},
					disableZoneAware: true,
					expected: map[string]int{
						destB.Config().Service: sendCount / 2,
						destC.Config().Service: sendCount / 2,
					},
				},
				{
					// Verifies that a same-region endpoint in a *different* zone beats a cross-region
					// endpoint. Without the region-bucketing in applyZoneAwareRegionalFailover, both land at
					// priority 0 and Envoy's zone-aware LB distributes traffic to the remote region.
					name:                  "CrossRegionEndpointIgnoredWhenSameRegionDifferentZoneAvailable",
					localClusterWorkloads: oneLocalClusterEndpoint,
					destinationWorkloads: []weEntry{
						{Address: destB.Address(), Locality: sameRegionZone2Locality},
						{Address: destC.Address(), Locality: remoteRegionLocality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					name:                  "CrossRegionEndpointUsedWhenSameRegionAbsent",
					localClusterWorkloads: oneLocalClusterEndpoint,
					destinationWorkloads: []weEntry{
						{Address: destC.Address(), Locality: remoteRegionLocality},
					},
					expected: expectAllTrafficTo(destC.Config().Service),
				},
				{
					// outlierDetection is required here: Envoy must detect and eject
					// the unreachable same-region endpoint before falling over to the
					// cross-region one.
					name:                  "CrossRegionEndpointUsedAfterSameRegionEjection",
					localClusterWorkloads: oneLocalClusterEndpoint,
					withOutlierDetection:  true,
					destinationWorkloads: []weEntry{
						{Address: "10.99.99.99", Locality: localLocality},
						{Address: destC.Address(), Locality: remoteRegionLocality},
					},
					expected: expectAllTrafficTo(destC.Config().Service),
				},
			}

			for i, tc := range cases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					hostSuffix := fmt.Sprintf("case-%d", i)
					minClusterSize := tc.minClusterSize
					if minClusterSize == 0 {
						minClusterSize = 1
					}
					input := zoneAwareInput{
						LocalHost:                fmt.Sprintf("local-%s.example.com", hostSuffix),
						RemoteHost:               fmt.Sprintf("zone-aware-%s.example.com", hostSuffix),
						LocalClusterWorkloadName: callerWorkloadName,
						LocalClusterLabels:       callerLocalClusterLabels,
						LocalClusterWorkloads:    tc.localClusterWorkloads,
						DestinationWorkloads:     tc.destinationWorkloads,
						WithOutlierDetection:     tc.withOutlierDetection,
						MinClusterSize:           minClusterSize,
						DisableZoneAware:         tc.disableZoneAware,
					}
					t.ConfigIstio().
						Eval(apps.Namespace.Name(), input, zoneAwareConfig).
						ApplyOrFail(t)

					// For cases where zone-aware is enabled, verify the proxy's Envoy config
					// is actually using zone-aware LB (not just locality LB defaults):
					//   1. local_cluster is present as a static cluster
					//   2. local_cluster has exactly the expected endpoint count (CLA populated via EDS)
					//   3. the remote-host cluster uses zone_aware_lb_config, not locality_weighted
					// For disabled cases, wait for EDS to converge on the destination cluster
					// before sending traffic so the distribution assertion is meaningful.
					if !tc.disableZoneAware {
						assertZoneAwareConfig(t, caller, input.RemoteHost, len(tc.localClusterWorkloads), tc.destinationWorkloads)
					} else {
						waitForDestinationEndpoints(t, caller, input.RemoteHost, len(tc.destinationWorkloads))
					}

					sendTrafficOrFail(t, caller, input.RemoteHost, tc.expected)
				})
			}
		})
}

func workloadNameForEcho(inst echo.Instance) string {
	cfg := inst.Config()
	version := cfg.Version
	if len(cfg.Subsets) > 0 {
		version = cfg.Subsets[0].Version
	}
	return fmt.Sprintf("%s-%s", cfg.Service, version)
}

func localClusterLabelsForEcho(t framework.TestContext, inst echo.Instance) map[string]string {
	t.Helper()
	workload := inst.WorkloadsOrFail(t)[0]
	pod, err := workload.Cluster().Kube().CoreV1().Pods(inst.NamespaceName()).Get(
		t.Context(), workload.PodName(), metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed getting caller pod %s/%s labels: %v", inst.NamespaceName(), workload.PodName(), err)
	}

	out := map[string]string{}
	for _, k := range []string{"pod-template-hash", "rollouts-pod-template-hash"} {
		if v := pod.Labels[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

// assertZoneAwareConfig waits for the caller's Envoy config to reflect zone-aware routing.
// Envoy's /config_dump endpoint omits endpoint data by default, so we use two separate
// admin queries: /config_dump for cluster definitions (LocalityConfigSpecifier) and
// /clusters?format=json for runtime host status (proves local_cluster is populated).
func assertZoneAwareConfig(
	t framework.TestContext,
	caller echo.Instance,
	remoteHost string,
	expectedLocalClusterHosts int,
	destinationWorkloads []weEntry,
) {
	t.Helper()
	sidecar := caller.WorkloadsOrFail(t)[0].Sidecar()
	remoteClusterName := fmt.Sprintf("outbound|80||%s", remoteHost)
	expectedDestinationHosts := expectedDestinationHostPriorities(destinationWorkloads)

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

	// Second: assert local_cluster and the destination cluster have the expected live hosts
	// via /clusters (config_dump excludes EDS data).
	retry.UntilSuccessOrFail(t, func() error {
		clusters, err := sidecar.Clusters()
		if err != nil {
			return err
		}
		foundLocalCluster := false
		foundRemoteCluster := false
		for _, cs := range clusters.GetClusterStatuses() {
			switch cs.GetName() {
			case localClusterName:
				foundLocalCluster = true
				got := len(cs.GetHostStatuses())
				if got != expectedLocalClusterHosts {
					return fmt.Errorf("cluster %q has %d hosts, expected %d — local_cluster EDS has unexpected endpoints",
						localClusterName, got, expectedLocalClusterHosts)
				}
			case remoteClusterName:
				foundRemoteCluster = true
				if err := assertDestinationHosts(cs, expectedDestinationHosts); err != nil {
					return err
				}
			}
		}
		if !foundLocalCluster {
			return fmt.Errorf("cluster %q not present in /clusters output", localClusterName)
		}
		if !foundRemoteCluster {
			return fmt.Errorf("cluster %q not present in /clusters output", remoteClusterName)
		}
		return nil
	}, retry.Delay(time.Second), retry.Timeout(30*time.Second))
}

// waitForDestinationEndpoints waits until the remote cluster is present in the caller's
// /clusters output and has exactly expectedCount live hosts. Used for cases where
// assertZoneAwareConfig is not called (e.g. zone-aware LB is disabled in the DR) to
// ensure EDS has converged before sending traffic.
func waitForDestinationEndpoints(
	t framework.TestContext,
	caller echo.Instance,
	remoteHost string,
	expectedCount int,
) {
	t.Helper()
	sidecar := caller.WorkloadsOrFail(t)[0].Sidecar()
	remoteClusterName := fmt.Sprintf("outbound|80||%s", remoteHost)

	retry.UntilSuccessOrFail(t, func() error {
		clusters, err := sidecar.Clusters()
		if err != nil {
			return err
		}
		for _, cs := range clusters.GetClusterStatuses() {
			if cs.GetName() != remoteClusterName {
				continue
			}
			got := len(cs.GetHostStatuses())
			if got != expectedCount {
				return fmt.Errorf("cluster %q has %d hosts, waiting for %d",
					remoteClusterName, got, expectedCount)
			}
			return nil
		}
		return fmt.Errorf("cluster %q not yet present in /clusters output", remoteClusterName)
	}, retry.Delay(time.Second), retry.Timeout(30*time.Second))
}

func expectedDestinationHostPriorities(workloads []weEntry) map[string]uint32 {
	raw := make(map[string]int, len(workloads))
	seenRawPriority := map[int]bool{}
	for _, we := range workloads {
		priority := 0
		if localityRegion(we.Locality) != localityRegion(localLocality) {
			priority = 1
		}
		raw[we.Address] = priority
		seenRawPriority[priority] = true
	}

	compacted := map[int]uint32{}
	if seenRawPriority[0] {
		compacted[0] = 0
		if seenRawPriority[1] {
			compacted[1] = 1
		}
	} else if seenRawPriority[1] {
		compacted[1] = 0
	}

	out := make(map[string]uint32, len(raw))
	for address, priority := range raw {
		out[address] = compacted[priority]
	}
	return out
}

func assertDestinationHosts(cs *admin.ClusterStatus, expected map[string]uint32) error {
	got := map[string]uint32{}
	for _, hs := range cs.GetHostStatuses() {
		socketAddress := hs.GetAddress().GetSocketAddress()
		if socketAddress == nil {
			return fmt.Errorf("cluster %q has host without socket address: %v", cs.GetName(), hs.GetAddress())
		}
		got[socketAddress.GetAddress()] = hs.GetPriority()
	}
	for address, priority := range expected {
		if gotPriority, ok := got[address]; !ok {
			return fmt.Errorf("cluster %q missing destination host %s; expected hosts %v, got hosts %v",
				cs.GetName(), address, expected, got)
		} else if gotPriority != priority {
			return fmt.Errorf("cluster %q destination host %s has priority %d, expected %d; expected hosts %v, got hosts %v",
				cs.GetName(), address, gotPriority, priority, expected, got)
		}
	}
	for address := range got {
		if _, ok := expected[address]; !ok {
			return fmt.Errorf("cluster %q has unexpected destination host %s; expected hosts %v, got hosts %v",
				cs.GetName(), address, expected, got)
		}
	}
	return nil
}

func localityRegion(locality string) string {
	region, _, _ := strings.Cut(locality, "/")
	return region
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
