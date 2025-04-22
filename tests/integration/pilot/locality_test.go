//go:build integ
// +build integ

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
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/integration/pilot/common"
)

const localityTemplate = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external-service-locality
  {{ with .TrafficDistribution }}
  annotations:
    networking.istio.io/traffic-distribution: {{.}}
  {{ end }}
spec:
  hosts:
  - {{.Host}}
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
  - address: {{.Local}}
    locality: region/zone/subzone
{{with .Network}}
    network: {{.}}
{{end}}
  - address: {{.Remote}}
    locality: notregion/notzone/notsubzone
{{with .Network}}
    network: {{.}}
{{end}}
  {{ if ne .NearLocal "" }}
  - address: {{.NearLocal}}
    locality: "nearregion/zone/subzone"
  {{ end }}
---
{{ if .LocalitySetting }}
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: external-service-locality
spec:
  host: {{.Host}}
  trafficPolicy:
    connectionPool:
      tcp:
        connectTimeout: 250ms
    loadBalancer:
      simple: ROUND_ROBIN
      localityLbSetting:
{{.LocalitySetting | indent 8 }}
    outlierDetection:
      interval: 1s
      baseEjectionTime: 10m
      maxEjectionPercent: 100
{{ end }}`

type LocalityInput struct {
	LocalitySetting     string
	TrafficDistribution string
	Host                string
	Resolution          string
	Local               string
	NearLocal           string
	Remote              string
	Network             string
}

const localityFailover = `
failover:
- from: region
  to: nearregion`

const failoverPriority = `
failoverPriority:
- "topology.kubernetes.io/region"
- "topology.kubernetes.io/zone"
- "topology.istio.io/subzone"`

const localityDistribute = `
distribute:
- from: region
  to:
    nearregion: 80
    region: 20`

func TestLocality(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			destA := apps.B[0]
			destB := apps.C[0]
			destC := apps.Naked[0]
			if !t.Settings().Skip(echo.VM) {
				// TODO do we even need this to be a VM
				destC = apps.VM[0]
			}

			var network string
			if len(t.Clusters()) == 1 {
				network = t.Clusters()[0].NetworkName()
			}
			cases := []struct {
				name     string
				input    LocalityInput
				expected map[string]int
			}{
				{
					"Prioritized/CDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "DNS",
						Local:           destA.Config().Service,
						Remote:          destB.Config().Service,
					},
					expectAllTrafficTo(destA.Config().Service),
				},
				{
					"Prioritized/EDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "STATIC",
						Local:           destB.Address(),
						Remote:          destA.Address(),
					},
					expectAllTrafficTo(destB.Config().Service),
				},
				{
					"Failover/CDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "DNS",
						Local:           "fake-should-fail.example.com",
						NearLocal:       destA.Config().Service,
						Remote:          destB.Config().Service,
					},
					expectAllTrafficTo(destA.Config().Service),
				},
				{
					"Failover/EDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "STATIC",
						Local:           "10.10.10.10",
						NearLocal:       destB.Address(),
						Remote:          destA.Address(),
					},
					expectAllTrafficTo(destB.Config().Service),
				},
				{
					"FailoverPriority/EDS",
					LocalityInput{
						LocalitySetting: failoverPriority,
						Resolution:      "STATIC",
						Local:           destA.Address(),
						NearLocal:       destB.Address(),
						Remote:          destC.Address(),
					},
					expectAllTrafficTo(destA.Config().Service),
				},
				{
					"Distribute/CDS",
					LocalityInput{
						LocalitySetting: localityDistribute,
						Resolution:      "DNS",
						Local:           destB.Config().Service,
						NearLocal:       destA.Config().Service,
						Remote:          "fake-should-fail.example.com",
					},
					map[string]int{
						destA.Config().Service: sendCount * .8,
						destB.Config().Service: sendCount * .2,
					},
				},
				{
					"Distribute/EDS",
					LocalityInput{
						LocalitySetting: localityDistribute,
						Resolution:      "STATIC",
						Local:           destA.Address(),
						NearLocal:       destB.Address(),
						Remote:          "10.10.10.10",
					},
					map[string]int{
						destB.Config().Service: sendCount * .8,
						destA.Config().Service: sendCount * .2,
					},
				},
				{
					"TrafficDistributionPreferClose/EDS",
					LocalityInput{
						TrafficDistribution: "PreferClose",
						Resolution:          "STATIC",
						Local:               destB.Address(),
						Remote:              destA.Address(),
						Network:             network,
					},
					expectAllTrafficTo(destB.Config().Service),
				},
				{
					"TrafficDistributionPreferSameZone/EDS",
					LocalityInput{
						TrafficDistribution: "PreferSameZone",
						Resolution:          "STATIC",
						Local:               destB.Address(),
						Remote:              destA.Address(),
						Network:             network,
					},
					expectAllTrafficTo(destB.Config().Service),
				},
				{
					// Not terribly meaningful because we only have one node, but we can still test that traffic passes
					"TrafficDistributionPreferSameNode/EDS",
					LocalityInput{
						TrafficDistribution: "PreferSameNode",
						Resolution:          "STATIC",
						Local:               destB.Address(),
						Remote:              destA.Address(),
						Network:             network,
					},
					expectAllTrafficTo(destB.Config().Service),
				},
			}
			for _, tt := range cases {
				t.NewSubTest(tt.name).Run(func(t framework.TestContext) {
					hostname := fmt.Sprintf("%s-fake-locality.example.com", strings.ToLower(strings.ReplaceAll(tt.name, "/", "-")))
					tt.input.Host = hostname
					t.ConfigIstio().
						Eval(apps.Namespace.Name(), tt.input, localityTemplate).
						ApplyOrFail(t)
					sendTrafficOrFail(t, apps.A[0], hostname, tt.expected)
				})
			}
		})
}

const sendCount = 50

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
			// Hostname will take form of svc-v1-random. We want to extract just 'svc'
			parts := strings.SplitN(r.Hostname, "-", 2)
			if len(parts) < 2 {
				return fmt.Errorf("unexpected hostname: %v", r)
			}
			gotHost := parts[0]
			got[gotHost]++
		}
		scopes.Framework.Infof("Got responses: %+v", got)
		for svc, reqs := range got {
			expect := expected[svc]
			if !common.AlmostEquals(reqs, expect, 3) {
				return fmt.Errorf("unexpected request distribution. Expected: %+v, got: %+v", expected, got)
			}
		}
		return nil
	}
	// This is a hack to remain infrastructure agnostic when running these tests
	// We actually call the host set above not the endpoint we pass
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
