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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/apps"
)

func sendTraffic(t *testing.T, duration time.Duration, batchSize int, from apps.KubeApp, to apps.KubeApp, hosts []string, weight []int32, errorBand float64) {
	const totalThreads = 10
	var hitCount [totalThreads]map[string]int
	var errorFailures [totalThreads]map[string]int

	for i := 0; i < totalThreads; i++ {
		hitCount[i] = map[string]int{}
		errorFailures[i] = map[string]int{}

		go func(id int) {
			for {
				resp, err := from.Call(to.EndpointForPort(80), apps.AppCallOptions{})

				for _, r := range resp {
					for _, h := range hosts {
						if strings.HasPrefix(r.Hostname, h+"-") {
							hitCount[id][h]++
							break
						}
					}
				}
				if err != nil {
					errorFailures[id][fmt.Sprintf("send to %v failed: %v", to, err)]++
				}
			}
		}(i)
	}

	checkTimeout := time.NewTicker(time.Second)
	timeout := time.After(duration)

	for {
		select {
		case <-timeout:
			t.Error("Failed to hit the weight ratio.")
			return
		case <-checkTimeout.C:
			hit := true
			log := "\n"

			hCount := map[string]int{}
			eFailures := map[string]int{}
			totalRequests := 0

			for i := 0; i < totalThreads; i++ {
				for k, v := range errorFailures[i] {
					eFailures[k] += v
				}

				for k, v := range hitCount[i] {
					totalRequests += v
					hCount[k] += v
				}
			}

			if totalRequests < batchSize {
				continue
			}

			if len(eFailures) > 0 {
				t.Errorf("Total requests: %v, requests failed: %v.", totalRequests, eFailures)
			}

			for i, v := range hosts {
				var actual = float64(hCount[v] * 100 / totalRequests)
				if errorBand-math.Abs(float64(weight[i])-actual) < 0 {
					hit = false
					break
				}

				log = fmt.Sprintf(
					"%sTraffic weight matches. Total request: %v, expected: %d%%, actually: %.2f%%, error band: %.2f%%\n",
					log,
					totalRequests,
					weight[i],
					actual,
					errorBand)
			}

			if hit {
				t.Log(log)
				return
			}
		}
	}
}
