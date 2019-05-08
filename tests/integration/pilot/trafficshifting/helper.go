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

	"istio.io/istio/pkg/test/framework/components/apps"
)

func sendTraffic(t *testing.T, duration time.Duration, batchSize int, from apps.KubeApp, to apps.KubeApp, hosts []string, weight []int32, errorBand float64) {
	const totalThreads = 10
	var hitCount sync.Map

	for i := 0; i < totalThreads; i++ {
		go func() {
			for {
				resp, _ := from.Call(to.EndpointForPort(80), apps.AppCallOptions{})

				for _, r := range resp {
					for _, h := range hosts {
						if strings.HasPrefix(r.Hostname, h+"-") {
							if cnt, found := hitCount.Load(h); found {
								hitCount.Store(h, cnt.(int)+1)
							} else {
								hitCount.Store(h, 1)
							}
							break
						}
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
			hit := true
			log := "\n"

			totalRequests := 0

			hCount := map[string]int{}
			hitCount.Range(func(key, val interface{}) bool {
				totalRequests += val.(int)
				hCount[key.(string)] += val.(int)
				return true
			})

			if totalRequests < batchSize {
				continue
			}

			for i, v := range hosts {
				actual := float64(hCount[v]) * 100.0 / float64(totalRequests)
				if errorBand-math.Abs(float64(weight[i])-actual) < 0 {
					hit = false
					break
				}

				log = fmt.Sprintf(
					"%sTraffic weight matches. Total request: %v, expected: %d%%, actually: %g%%, error band: %g%%\n",
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
