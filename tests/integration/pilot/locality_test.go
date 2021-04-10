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
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/integration/pilot/common"
)

const localityTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: external-service-locality
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
  - address: {{.Remote}}
    locality: notregion/notzone/notsubzone
  {{ if ne .NearLocal "" }}
  - address: {{.NearLocal}}
    locality: "nearregion/zone/subzone"
  {{ end }}
---
apiVersion: networking.istio.io/v1alpha3
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
      maxEjectionPercent: 100`

type LocalityInput struct {
	LocalitySetting string
	Host            string
	Resolution      string
	Local           string
	NearLocal       string
	Remote          string
}

const localityFailover = `
failover:
- from: region
  to: nearregion`

const localityDistribute = `
distribute:
- from: region
  to:
    nearregion: 80
    region: 20`

func TestLocality(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.locality").
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
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
						Local:           common.PodBSvc,
						Remote:          common.PodCSvc,
					},
					expectAllTrafficTo(common.PodBSvc),
				},
				{
					"Prioritized/EDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "STATIC",
						Local:           apps.PodC[0].Address(),
						Remote:          apps.PodB[0].Address(),
					},
					expectAllTrafficTo(common.PodCSvc),
				},
				{
					"Failover/CDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "DNS",
						Local:           "fake-should-fail.example.com",
						NearLocal:       common.PodBSvc,
						Remote:          common.PodCSvc,
					},
					expectAllTrafficTo(common.PodBSvc),
				},
				{
					"Failover/EDS",
					LocalityInput{
						LocalitySetting: localityFailover,
						Resolution:      "STATIC",
						Local:           "10.10.10.10",
						NearLocal:       apps.PodC[0].Address(),
						Remote:          apps.PodB[0].Address(),
					},
					expectAllTrafficTo(common.PodCSvc),
				},
				{
					"Distribute/CDS",
					LocalityInput{
						LocalitySetting: localityDistribute,
						Resolution:      "DNS",
						Local:           common.PodCSvc,
						NearLocal:       common.PodBSvc,
						Remote:          "fake-should-fail.example.com",
					},
					map[string]int{
						common.PodBSvc: sendCount * .8,
						common.PodCSvc: sendCount * .2,
					},
				},
				{
					"Distribute/EDS",
					LocalityInput{
						LocalitySetting: localityDistribute,
						Resolution:      "STATIC",
						Local:           apps.PodB[0].Address(),
						NearLocal:       apps.PodC[0].Address(),
						Remote:          "10.10.10.10",
					},
					map[string]int{
						common.PodCSvc: sendCount * .8,
						common.PodBSvc: sendCount * .2,
					},
				},
			}
			for _, tt := range cases {
				t.NewSubTest(tt.name).Run(func(t framework.TestContext) {
					hostname := fmt.Sprintf("%s-fake-locality.example.com", strings.ToLower(strings.ReplaceAll(tt.name, "/", "-")))
					tt.input.Host = hostname
					t.Config().ApplyYAMLOrFail(t, apps.Namespace.Name(), runTemplate(t, localityTemplate, tt.input))
					sendTrafficOrFail(t, apps.PodA[0], hostname, tt.expected)
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
	validator := echo.ValidatorFunc(func(resp client.ParsedResponses, inErr error) error {
		if inErr != nil {
			return inErr
		}
		got := map[string]int{}
		for _, r := range resp {
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
	})
	// This is a hack to remain infrastructure agnostic when running these tests
	// We actually call the host set above not the endpoint we pass
	_ = from.CallWithRetryOrFail(t, echo.CallOptions{
		Target:    from,
		PortName:  "http",
		Headers:   headers,
		Count:     sendCount,
		Validator: validator,
	})
}

func runTemplate(t test.Failer, tmpl string, input interface{}) string {
	tt, err := template.New("").Funcs(sprig.TxtFuncMap()).Parse(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tt.Execute(&buf, input); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
