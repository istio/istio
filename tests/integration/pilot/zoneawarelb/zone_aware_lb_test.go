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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/integration/pilot/common"
)

const (
	// localLocality must match the locality of the calling proxy (apps.A defaults to
	// "region.zone.subzone" in the standard echo deployment).
	localLocality  = "region/zone/subzone"
	remoteLocality = "notregion/notzone/notsubzone"

	sendCount = 50
)

// Zone-aware-routing configuration:
//   - LocalSvc: a ServiceEntry whose endpoint matches the calling proxy's address.
//     This makes the proxy's local_cluster (the static cluster injected when
//     ENABLE_ZONE_AWARE_LOAD_BALANCER=true) resolve to a real service, exercising
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
			// apps.A is the caller (proxy locality = "region/zone/subzone").
			caller := apps.A[0]
			// destB/destC are real backends we point WorkloadEntries at; their actual k8s
			// localities are irrelevant — what matters is the locality declared on each WE.
			destB := apps.B[0]
			destC := apps.C[0]
			proxyAddress := caller.WorkloadsOrFail(t)[0].Address()

			cases := []struct {
				name      string
				workloads []weEntry
				// expected is a map from echo service name → expected response count.
				expected map[string]int
			}{
				{
					// One backend in the proxy's locality + one in a different region.
					// Zone-aware LB keeps same-region endpoints at priority 0 (cluster-level
					// ZoneAwareLbConfig only balances zones at p0), so all traffic goes local.
					name: "LocalEndpointAttractsTraffic",
					workloads: []weEntry{
						{Address: destB.Address(), Locality: localLocality},
						{Address: destC.Address(), Locality: remoteLocality},
					},
					expected: expectAllTrafficTo(destB.Config().Service),
				},
				{
					// Multiple local endpoints, multiple remote. Zone-aware should still
					// pin everything to the local zone (priority 0 holds every local).
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
					// No local endpoint — zone-aware DR cannot route locally, so traffic
					// must fall over to the remote-region endpoint (priority 1).
					name: "NoLocalFailsOverToRemote",
					workloads: []weEntry{
						{Address: destC.Address(), Locality: remoteLocality},
					},
					expected: expectAllTrafficTo(destC.Config().Service),
				},
				{
					// Local endpoint exists but is unreachable. Outlier detection ejects it,
					// Envoy drops to priority 1 (remote) and traffic ends up on destC.
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
					sendTrafficOrFail(t, caller, input.RemoteHost, tc.expected)
				})
			}
		})
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
			// Hostname has form svc-version-randomid; extract just svc.
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
	// We call our own service but with a Host header pointing at the zone-aware service —
	// matches the locality_test pattern of indirection through the ServiceEntry hostname.
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
