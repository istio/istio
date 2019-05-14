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
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/echo"
)

func sendTraffic(t *testing.T, duration time.Duration, batchSize int, from, to echo.Instance, hosts []string, weight []int32, errorThreshold float64) {
	const totalThreads = 10
	var lock sync.RWMutex
	var totalRequests int
	hitCount := map[string]int{}

	for i := 0; i < totalThreads; i++ {
		go func() {
			resp, _ := from.Call(echo.CallOptions{
				Target:   to,
				PortName: "http",
				Count:    batchSize,
			})

			for _, r := range resp {
				for _, h := range hosts {
					if strings.HasPrefix(r.Hostname, h+"-") {
						lock.Lock()
						hitCount[h]++
						totalRequests++
						lock.Unlock()
						break
					}
				}
			}
		}()
	}

	checkTimeout := time.NewTicker(time.Second)
	timeout := time.After(duration)

	for {
		select {
		case <-timeout:
			t.Error("Failed to hit the weight ratio.")
			return
		case <-checkTimeout.C:
			lock.RLock()
			if totalRequests >= batchSize {
				hit := true
				log := "\n"
				for i, v := range hosts {
					percentOfTrafficToHost := float64(hitCount[v]) * 100.0 / float64(totalRequests)
					deltaFromExpected := math.Abs(float64(weight[i]) - percentOfTrafficToHost)
					if errorThreshold-deltaFromExpected < 0 {
						hit = false
						break
					}

					log = fmt.Sprintf(
						"%sTraffic weight matches. Total request: %v, expected: %d%%, actually: %g%%, error threshold: %g%%\n",
						log,
						totalRequests,
						weight[i],
						percentOfTrafficToHost,
						errorThreshold)
				}

				if hit {
					t.Log(log)
					lock.RUnlock()
					return
				}
			}

			lock.RUnlock()
		}
	}
}
