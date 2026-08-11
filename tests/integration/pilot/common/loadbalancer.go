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

package common

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/slices"
	echot "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	echodeployment "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

// ---------- Zone-Aware Load Balancer ----------

const (
	localLocality           = "region/zone/subzone"
	sameRegionZone2Locality = "region/zone2/subzone"
	sameRegionZone3Locality = "region/zone3/subzone"
	sameRegionZone4Locality = "region/zone4/subzone"
	remoteRegionLocality    = "notregion/notzone/notsubzone"
	localClusterName        = "local_cluster"
)

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

// WeEntry holds address and locality for a WorkloadEntry used in zone-aware LB tests.
type WeEntry struct {
	Address  string
	Locality string
}

type zoneAwareInput struct {
	LocalHost                string
	RemoteHost               string
	LocalClusterWorkloadName string
	LocalClusterLabels       map[string]string
	LocalClusterWorkloads    []WeEntry
	DestinationWorkloads     []WeEntry
	WithOutlierDetection     bool
	MinClusterSize           int
	DisableZoneAware         bool
}

// RunZoneAwareLBTests validates Envoy zone-aware load balancing via ServiceEntry +
// WorkloadEntry topology. Requires a single-cluster environment.
func RunZoneAwareLBTests(t framework.TestContext, apps cdeployment.SingleNamespaceView) {
	t.Helper()
	callerInstances := echodeployment.New(t).
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
	oneLocalClusterEndpoint := []WeEntry{
		{Address: proxyAddress, Locality: localLocality},
	}
	twoLocalClusterZones := []WeEntry{
		{Address: proxyAddress, Locality: localLocality},
		{Address: sourcePeerAddress, Locality: sameRegionZone2Locality},
	}

	cases := []struct {
		name                  string
		localClusterWorkloads []WeEntry
		destinationWorkloads  []WeEntry
		withOutlierDetection  bool
		minClusterSize        int
		disableZoneAware      bool
		expected              map[string]int
	}{
		{
			name:                  "OneSourceEndpointOneDestinationEndpointSameLocality",
			localClusterWorkloads: oneLocalClusterEndpoint,
			destinationWorkloads: []WeEntry{
				{Address: destB.Address(), Locality: localLocality},
			},
			expected: expectAllTrafficTo(destB.Config().Service),
		},
		{
			name:                  "OneSourceEndpointTwoDestinationLocalitiesSameRegionOverflows",
			localClusterWorkloads: oneLocalClusterEndpoint,
			destinationWorkloads: []WeEntry{
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
			destinationWorkloads: []WeEntry{
				{Address: destB.Address(), Locality: localLocality},
				{Address: destC.Address(), Locality: sameRegionZone2Locality},
			},
			expected: expectAllTrafficTo(destB.Config().Service),
		},
		{
			name:                  "ZoneAwareDisabledTwoSourceZonesTwoDestinationEndpointsMatchingSourceZones",
			localClusterWorkloads: twoLocalClusterZones,
			destinationWorkloads: []WeEntry{
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
			name:                  "HighMinClusterSizeDisablesZoneAwareRouting",
			localClusterWorkloads: twoLocalClusterZones,
			destinationWorkloads: []WeEntry{
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
			destinationWorkloads: []WeEntry{
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
			destinationWorkloads: []WeEntry{
				{Address: destB.Address(), Locality: localLocality},
				{Address: destC.Address(), Locality: remoteRegionLocality},
			},
			expected: expectAllTrafficTo(destB.Config().Service),
		},
		{
			name:                  "ZoneAwareDisabledCrossRegionEndpointNotIgnored",
			localClusterWorkloads: oneLocalClusterEndpoint,
			destinationWorkloads: []WeEntry{
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
			name:                  "CrossRegionEndpointIgnoredWhenSameRegionDifferentZoneAvailable",
			localClusterWorkloads: oneLocalClusterEndpoint,
			destinationWorkloads: []WeEntry{
				{Address: destB.Address(), Locality: sameRegionZone2Locality},
				{Address: destC.Address(), Locality: remoteRegionLocality},
			},
			expected: expectAllTrafficTo(destB.Config().Service),
		},
		{
			name:                  "CrossRegionEndpointUsedWhenSameRegionAbsent",
			localClusterWorkloads: oneLocalClusterEndpoint,
			destinationWorkloads: []WeEntry{
				{Address: destC.Address(), Locality: remoteRegionLocality},
			},
			expected: expectAllTrafficTo(destC.Config().Service),
		},
		{
			name:                  "CrossRegionEndpointUsedAfterSameRegionEjection",
			localClusterWorkloads: oneLocalClusterEndpoint,
			withOutlierDetection:  true,
			destinationWorkloads: []WeEntry{
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

			if !tc.disableZoneAware {
				assertZoneAwareConfig(t, caller, input.RemoteHost, len(tc.localClusterWorkloads), tc.destinationWorkloads)
			} else {
				waitForDestinationEndpoints(t, caller, input.RemoteHost, len(tc.destinationWorkloads))
			}

			sendTrafficOrFail(t, caller, input.RemoteHost, tc.expected)
		})
	}
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
		context.TODO(), workload.PodName(), metav1.GetOptions{},
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

func assertZoneAwareConfig(
	t framework.TestContext,
	caller echo.Instance,
	remoteHost string,
	expectedLocalClusterHosts int,
	destinationWorkloads []WeEntry,
) {
	t.Helper()
	sidecar := caller.WorkloadsOrFail(t)[0].Sidecar()
	remoteClusterName := fmt.Sprintf("outbound|80||%s", remoteHost)
	expectedDestinationHosts := expectedDestinationHostPriorities(destinationWorkloads)

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

func expectedDestinationHostPriorities(workloads []WeEntry) map[string]uint32 {
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

// ---------- Dual-Stack Endpoint Load Balancer ----------

func getDualStackEchoConfigs() []echo.Config {
	return []echo.Config{
		{
			Service: "client",
			Ports:   ports.All(),
		},
		{
			Service:        "echo-dual",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
		},
		{
			Service:        "echo-v4",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv4",
		},
		{
			Service:        "echo-v4only",
			Ports:          ports.All(),
			IPFamilyPolicy: "SingleStack",
			BindFamily:     "IPv4",
			IPFamilies:     "IPv4",
		},
		{
			Service:        "echo-v6only",
			Ports:          ports.All(),
			IPFamilyPolicy: "SingleStack",
			BindFamily:     "IPv6",
			IPFamilies:     "IPv6",
		},
		{
			Service:        "echo-v6",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv6",
		},
		{
			Service:        "echo-v4-naked",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv4",
			Subsets:        []echo.SubsetConfig{{Annotations: map[string]string{annotation.SidecarInject.Name: "false"}}},
		},
		{
			Service:        "echo-v6-naked",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv6",
			Subsets:        []echo.SubsetConfig{{Annotations: map[string]string{annotation.SidecarInject.Name: "false"}}},
		},
	}
}

// RunDualStackLBTests validates that traffic is correctly load-balanced across
// both IPv4 and IPv6 endpoints when dual-stack services are used.
func RunDualStackLBTests(t framework.TestContext) {
	t.Helper()
	echoDS := namespace.NewOrFail(t, namespace.Config{
		Prefix: "echo-ds",
		Inject: true,
	})
	echos := echodeployment.New(t)
	echos.WithClusters(t.Clusters()...)
	for _, config := range getDualStackEchoConfigs() {
		config.Namespace = echoDS
		echos.WithConfig(config)
	}
	echosDeployment := echos.BuildOrFail(t)
	fromMatch := match.ServiceName(echo.NamespacedName{
		Name:      "client",
		Namespace: echoDS,
	})
	toMatch := match.Not(fromMatch)

	echotest.New(t, echosDeployment).
		FromMatch(fromMatch).
		ToMatch(toMatch).
		Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
			for _, toInstance := range to.Instances() {
				for _, testFamily := range []int{4, 6} {
					t.NewSubTestf("%sVia%d", to.ServiceName(), testFamily).Run(func(t framework.TestContext) {
						defaultFamily := 6
						address := ""
						for i, addr := range toInstance.Addresses() {
							if net.ParseIP(addr).To4() != nil {
								if i == 0 {
									defaultFamily = 4
								}
								if testFamily == 4 {
									address = addr
									break
								}
							} else if testFamily == 6 {
								address = addr
							}
						}

						opts := echo.CallOptions{
							Port: echo.Port{
								Name: "http-instance",
							},
							To:      toInstance,
							Address: address,
							Check:   check.OK(),
						}
						opts.HTTP.Headers = headers.New().
							WithHost(fmt.Sprintf("%s.%s.svc.cluster.local", to.ServiceName(), to.NamespaceName())).
							Build()

						bindFamily := to.Config().BindFamily
						if bindFamily != "" {
							if !to.Config().IsNaked() && ((bindFamily == "IPv4" && defaultFamily == 6) || (bindFamily == "IPv6" && defaultFamily == 4)) {
								opts.Check = check.NotOK()
							}
						}
						res := from.CallOrFail(t, opts)
						responses := slices.Filter(res.Responses, func(response echot.Response) bool {
							return response.Code == "200"
						})
						if len(responses) > 0 {
							response := responses[0]
							ipBodyFamily := 6
							if net.ParseIP(response.RawBody["IP"]).To4() != nil {
								ipBodyFamily = 4
							}
							if to.Config().IsNaked() {
								expectedIP := testFamily
								if to.Config().BindFamily == "IPv4" {
									expectedIP = 4
								} else if to.Config().BindFamily == "IPv6" {
									expectedIP = 6
								}
								assert.Equal(t, ipBodyFamily, expectedIP)
							} else {
								assert.Equal(t, ipBodyFamily, defaultFamily)
							}
						}
					})
				}
			}
		})
}
