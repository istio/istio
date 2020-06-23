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

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
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

// Test case for regular echo instances
func TestTrafficShifting(t *testing.T) {
	trafficShifting(t, []string{}) // pass empty list to indicate no VM
}

// Test case for VM traffic shifting. Client a will sent requests to 3 VMs
// VM image version is default to ubuntu:bionic
// All other images will be tested in post-submit to reduce runtime
func TestVMTrafficShifting(t *testing.T) {
	testcases := []string{"app_sidecar_ubuntu_bionic"}
	trafficShifting(t, testcases)
}

// Test case for post-submit to test all OS types/versions
func TestVMTrafficShiftingPost(t *testing.T) {
	testcases := []string{"app_sidecar_ubuntu_xenial", "app_sidecar_ubuntu_focal", "app_sidecar_ubuntu_bionic",
		"app_sidecar_debian_9", "app_sidecar_debian_10"}
	trafficShifting(t, testcases, label.Postsubmit) // mark it to be post-submit
}

func trafficShifting(t *testing.T, vmImages []string, label ...label.Instance) {
	// Traffic distribution
	weights := map[string][]int32{
		"20-80":    {20, 80},
		"50-50":    {50, 50},
		"33-33-34": {33, 33, 34},
	}
	framework.
		NewTest(t).
		Features("traffic.routing").
		Label(label...).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "traffic-shifting",
				Inject: true,
			})

			// placeholder to ensure the test to run for non-vm tests
			if len(vmImages) == 0 {
				vmImages = append(vmImages, "")
			}

			// continue using this one client to call different version of hosts
			var client echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echoConfig(ns, "a")).
				BuildOrFail(t)

			// build and test with set of VM instances
			for i, vmImage := range vmImages {
				if vmImage != "" {
					t.Logf("Running as VMs. Testing %v", vmImage)
				}

				// build instances with their image name
				var instances [3]echo.Instance
				hosts := []string{fmt.Sprintf("b-%v", i),
					fmt.Sprintf("c-%v", i),
					fmt.Sprintf("d-%v", i)}
				echoboot.NewBuilderOrFail(t, ctx).
					With(&instances[0], echoConfig(ns, hosts[0], vmImage)).
					With(&instances[1], echoConfig(ns, hosts[1], vmImage)).
					With(&instances[2], echoConfig(ns, hosts[2], vmImage)).
					BuildOrFail(t)

				// traverse the weights and apply the VirtualService
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
						ctx.ApplyConfigOrFail(t, ns.Name(), deployment)

						sendTraffic(t, 100, client, instances[0], hosts, v, errorThreshold)
					})
				}
			}
		})
}

// Echo config helper function. If no vmImage or empty string is provided, non-vm pod will be created
// Else, it will create a VM pod using the vmImage.
func echoConfig(ns namespace.Instance, name string, vmImage ...string) echo.Config {
	deployAsVM := false
	vmImageName := ""
	if len(vmImage) > 0 && vmImage[0] != "" {
		deployAsVM = true
		vmImageName = vmImage[0]
	}
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
		},
		Subsets:    []echo.SubsetConfig{{}},
		Pilot:      p,
		DeployAsVM: deployAsVM,
		VMImage:    vmImageName,
	}
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
