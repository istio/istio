/*
 * // Copyright Istio Authors
 * //
 * // Licensed under the Apache License, Version 2.0 (the "License");
 * // you may not use this file except in compliance with the License.
 * // You may obtain a copy of the License at
 * //
 * //     http://www.apache.org/licenses/LICENSE-2.0
 * //
 * // Unless required by applicable law or agreed to in writing, software
 * // distributed under the License is distributed on an "AS IS" BASIS,
 * // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * // See the License for the specific language governing permissions and
 * // limitations under the License.
 */

package vm

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	DefaultVMImage = "app_sidecar_ubuntu_bionic"
)

func GetSupportedOSVersion() []string {
	return []string{"app_sidecar_ubuntu_xenial", "app_sidecar_ubuntu_focal", "app_sidecar_ubuntu_bionic",
		"app_sidecar_debian_9", "app_sidecar_debian_10", "app_sidecar_centos_8"}
}

func SendTraffic(t *testing.T, batchSize int, from, to echo.Instance, hosts []string, weight []int32, errorThreshold float64) {
	t.Helper()
	// Send `batchSize` requests and ensure they are distributed as expected.
	retry.UntilSuccessOrFail(t, func() error {
		resp, err := from.Call(echo.CallOptions{
			Target:   to,
			PortName: "http",
			Count:    batchSize,
		})
		if err != nil {
			return fmt.Errorf("error during call: %v", err)
		}
		var totalRequests int
		hitCount := map[string]int{}
		for _, r := range resp {
			for _, h := range hosts {
				if strings.HasPrefix(r.Hostname, h+"-") {
					hitCount[h]++
					totalRequests++
					break
				}
			}
		}

		for i, v := range hosts {
			percentOfTrafficToHost := float64(hitCount[v]) * 100.0 / float64(totalRequests)
			deltaFromExpected := math.Abs(float64(weight[i]) - percentOfTrafficToHost)
			if errorThreshold-deltaFromExpected < 0 {
				return fmt.Errorf("unexpected traffic weight for host %v. Expected %d%%, got %g%% (thresold: %g%%)",
					v, weight[i], percentOfTrafficToHost, errorThreshold)
			}
			t.Logf("Got expected traffic weight for host %v. Expected %d%%, got %g%% (thresold: %g%%)",
				v, weight[i], percentOfTrafficToHost, errorThreshold)
		}
		return nil
	}, retry.Delay(time.Second))
}
