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
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework/components/apps"
)

func sendTraffic(t *testing.T, batchSize int, from apps.KubeApp, to string, hosts []string, weight []int32, errorBand float64) {

	stop := make(chan bool, 1)
	totalRequests := 0
	hostnameHitCount := map[string]int{}
	errorFailures := map[string]int{}

	go func() {
		for totalRequests < batchSize {
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

		stop <- true
	}()

	<-stop
	if totalRequests < 1 {
		t.Error("No requests made.")
	}
	if len(errorFailures) > 0 {
		t.Errorf("Total requests: %v, requests failed: %v.", totalRequests, errorFailures)
	}

	for i, v := range hosts {
		var actual = float64(hostnameHitCount[v] * 100 / totalRequests)
		if errorBand-math.Abs(float64(weight[i])-actual) < 0 {
			t.Errorf(
				"Traffic weight doesn't match. Total request: %v, expected: %d%%, actually: %.2f%%, error band: %.2f%%",
				totalRequests,
				weight[i],
				actual,
				errorBand)
		}

		t.Logf(
			"Traffic weight matches. Total request: %v, expected: %d%%, actually: %.2f%%, error band: %.2f%%",
			totalRequests,
			weight[i],
			actual,
			errorBand)
	}
}
