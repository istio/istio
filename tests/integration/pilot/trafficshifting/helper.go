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

package trafficshifting

import (
	"bytes"
	"fmt"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"math"
	"net/http"
	"strings"
	"testing"
	"text/template"
	"time"
)

//	Virtual service topology
//
//						 a
//						|-------|
//						| Host0 |
//						|-------|
//							|
//							|
//							|
//		-------------------------------------
//		|weight1	|weight2	|weight3	|weight4
//		|a			|b			|c			|d
//	|-------|	|-------|	|-------|	|-------|
//	| Host0 |	| Host1	|	| Host2 |	| Host3 |
//	|-------|	|-------|	|-------|	|-------|
//
//

const (
	testDuration = 10 * time.Second

	errorBand = 10.0 // Error band %10. As the test duration is 10 seconds, the distribution of traffic might be different from assigned weight.

	VirtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
 name: {{.Name}}
 namespace: {{.Namespace}}
spec:
 hosts:
 - {{.Host0}}
 http:
 - route:
   - destination:
       host: {{.Host0}}
     weight: {{.Weight0}}
   - destination:
       host: {{.Host1}}
     weight: {{.Weight1}}
   - destination:
       host: {{.Host2}}
     weight: {{.Weight2}}
   - destination:
       host: {{.Host3}}
     weight: {{.Weight3}}
`
)

var (
	hosts = []string{"a", "b", "c", "d"}
)

type VirtualServiceConfig struct {
	Name      string
	Host0     string
	Host1     string
	Host2     string
	Host3     string
	Namespace string
	Weight0   int32
	Weight1   int32
	Weight2   int32
	Weight3   int32
}

func RunTrafficShiftingTest(t *testing.T, weight []int32) {
	if len(weight) == 0 || len(weight) > 4 {
		t.Error("Input parameter is invalid. The length of weight is in [1, 4].")
	}

	var sum int32 = 0
	for _, v := range weight {
		if v < 0 || 100 < v {
			t.Error("Input parameter is invalid. The weight should be in [0, 100].")
		}
		sum += v
	}

	if sum != 100 {
		t.Error("Input parameter is invalid. The sum of the weight should be 100.0.")
	}

	weight = append(weight, make([]int32, 4-len(weight))...)

	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

			instance := apps.NewOrFail(t, ctx, apps.Config{Pilot: p, Galley: g})

			vsc := VirtualServiceConfig{
				"traffic-shifting-rule",
				hosts[0],
				hosts[1],
				hosts[2],
				hosts[3],
				instance.Namespace().Name(),
				weight[0],
				weight[1],
				weight[2],
				weight[3],
			}

			tmpl, _ := template.New("VirtualServiceConfig").Parse(VirtualService)
			var buf bytes.Buffer
			tmpl.Execute(&buf, vsc)

			g.ApplyConfigOrFail(t, instance.Namespace(), buf.String())
			time.Sleep(10 * time.Second)

			from := instance.GetAppOrFail(hosts[0], t).(apps.KubeApp)

			sendTraffic(t, testDuration, from, hosts[0], weight)
		})
}

func sendTraffic(t *testing.T, duration time.Duration, from apps.KubeApp, to string, weight []int32) {
	timeout := time.After(duration)
	totalRequests := 0
	hostnameHitCount := map[string]int{}

	for _, v := range hosts {
		hostnameHitCount[v] = 0
	}

	errorFailures := map[string]int{}
	for {
		select {
		case <-timeout:
			if totalRequests < 1 {
				t.Error("No requests made.")
			}
			if len(errorFailures) > 0 {
				t.Errorf("Total requests: %v, requests failed: %v.", totalRequests, errorFailures)
			}

			for i, v := range hosts {
				var actual = float64(hostnameHitCount[v] * 100 / totalRequests)
				if errorBand-math.Abs(float64(weight[i])-actual) < 0 {
					t.Errorf("Traffic weight doesn't match. Total request: %v, expected: %d%%, actually: %.2f%%, error band: %.2f%%", totalRequests, weight[i], actual, errorBand)
				}

				t.Logf("Traffic weight matches. Total request: %v, expected: %d%%, actually: %.2f%%, error band: %.2f%%", totalRequests, weight[i], actual, errorBand)
			}

			return
		default:
			headers := http.Header{}
			headers.Add("Host", to)
			// This is a hack to remain infrastructure agnostic when running these tests
			// We actually call the host set above not the endpoint we pass
			resp, err := from.Call(from.EndpointForPort(80), apps.AppCallOptions{
				Protocol: apps.AppProtocolHTTP,
				Headers:  headers,
			})
			totalRequests++
			for _, r := range resp {
				for _, h := range hosts {
					if strings.HasPrefix(r.Hostname, h+"-") {
						hostnameHitCount[h]++
						break
					}
				}
			}
			if err != nil {
				errorFailures[fmt.Sprintf("send to %v failed: %v", to, err)]++
			}
		}
	}
}
