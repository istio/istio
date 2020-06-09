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
	"math"
	"strings"
	"testing"
	"time"

	"istio.io/istio/tests/integration/pilot/vm"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
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
//		-------------------------
//		|weight1	|weight2	|weight3
//		|b			|c			|d
//	|-------|	|-------|	|-------|
//	| Host0 |	| Host1	|	| Host2 |
//	|-------|	|-------|	|-------|
//
//

const (
	// Error threshold. For example, we expect 25% traffic, traffic distribution within [15%, 35%] is accepted.
	errorThreshold = 10.0
)

type VirtualServiceConfig struct {
	Name      string
	Host0     string
	Host1     string
	Host2     string
	Namespace string
	Weight0   int32
	Weight1   int32
	Weight2   int32
}

// Traffic shifting test body. This test will call from client to 3 instances
// to see if the traffic distribution follows the weights set by the VirtualService
func TestTrafficShifting(t *testing.T) {
	// Traffic distribution
	weights := map[string][]int32{
		"20-80":    {20, 80},
		"50-50":    {50, 50},
		"33-33-34": {33, 33, 34},
	}

	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "traffic-shifting",
				Inject: true,
			})

			var instances [4]echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instances[0], echoVMConfig(ns, "a", ctx.Environment().Clusters()[0])).
				With(&instances[1], echoVMConfig(ns, "b", ctx.Environment().Clusters()[0])).
				With(&instances[2], echoVMConfig(ns, "c", ctx.Environment().Clusters()[0], vm.DefaultVMImage)).
				With(&instances[3], echoVMConfig(ns, "d", ctx.Environment().Clusters()[0])).
				BuildOrFail(t)

			hosts := []string{"b", "c", "d"}

			for k, v := range weights {
				t.Run(k, func(t *testing.T) {
					v = append(v, make([]int32, 3-len(v))...)

					vsc := VirtualServiceConfig{
						"traffic-shifting-rule",
						hosts[0],
						hosts[1],
						hosts[2],
						ns.Name(),
						v[0],
						v[1],
						v[2],
					}

					deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/traffic-shifting.yaml"), vsc)
					ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)

					sendTraffic(t, 100, instances[0], instances[1], hosts, v, errorThreshold)
				})
			}
		})
}

// Wrapper to initialize instance with ServicePort without affecting other tests
// If ServicePort is set to InstancePort, tests such as TestDescribe would fail
func echoVMConfig(ns namespace.Instance, name string, cluster resource.Cluster, vmImage ...string) echo.Config {
	image := ""
	if len(vmImage) > 0 {
		image = vmImage[0]
	}
	config := echoConfig(ns, name, cluster)
	config.DeployAsVM = image != ""
	config.VMImage = image

	// This is necessary because there exists a bug in WorkloadEntry
	// The ServicePort has to be the same with the InstancePort
	config.Ports[0].ServicePort = config.Ports[0].InstancePort
	return config
}

func sendTraffic(t *testing.T, batchSize int, from, to echo.Instance, hosts []string, weight []int32, errorThreshold float64) {
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
