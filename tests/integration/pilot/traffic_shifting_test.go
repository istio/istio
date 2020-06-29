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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/tests/integration/pilot/vm"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"

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
	Name       string
	Namespace  string
	TargetHost string
	Hosts      []string
	Weights    []int
}

// Traffic shifting test body. This test will call from client to 3 instances
// to see if the traffic distribution follows the weights set by the VirtualService
func TestTrafficShifting(t *testing.T) {
	// Traffic distribution
	weights := map[string][]int{
		"20-80":    {20, 80},
		"50-50":    {50, 50},
		"33-33-34": {33, 33, 34},
	}

	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("traffic.shifting").
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "traffic-shifting",
				Inject: true,
			})

			var instances [4]echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instances[0], echoVMConfig(ns, "a")).
				With(&instances[1], echoVMConfig(ns, "b")).
				With(&instances[2], echoVMConfig(ns, "c", vm.DefaultVMImage)).
				With(&instances[3], echoVMConfig(ns, "d")).
				BuildOrFail(t)

			hosts := []string{"b", "c", "d"}

			for k, v := range weights {
				t.Run(k, func(t *testing.T) {
					v = append(v, make([]int, 3-len(v))...)

					vsc := VirtualServiceConfig{
						Name:       "traffic-shifting-rule",
						Namespace:  ns.Name(),
						TargetHost: hosts[0],
						Hosts:      hosts,
						Weights:    v,
					}

					deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/traffic-shifting.yaml"), vsc)
					ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)

					sendTraffic(t, 100, instances[0], instances[1], hosts, v, errorThreshold)
				})
			}
		})
}

func TestCrossClusterTrafficShifting(t *testing.T) {
	framework.NewTest(t).
		RequiresMinClusters(2).
		Features("traffic.shifting").
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "x-cluster-traffic-shifting",
				Inject: true,
			})

			var hosts []string
			var weights []int
			builder := echoboot.NewBuilderOrFail(ctx, ctx)
			svcs := make([][2]echo.Instance, len(ctx.Environment().Clusters()))
			for _, c := range ctx.Environment().Clusters() {
				serverHost := fmt.Sprintf("server-%d", c.Index())
				weights = append(weights, 100/len(ctx.Environment().Clusters()))
				hosts = append(hosts, serverHost)
				svcs[c.Index()] = [2]echo.Instance{}
				builder.
					With(&svcs[c.Index()][0], echoConfigForCluster(ns, fmt.Sprintf("client-%d", c.Index()), c)).
					With(&svcs[c.Index()][1], echoConfigForCluster(ns, serverHost, c))
			}
			builder.BuildOrFail(ctx)

			// ensure weights total to 100
			total := 0
			for _, w := range weights {
				total += w
			}
			weights[0] += 100 - total

			vsTmpl := file.AsStringOrFail(ctx, "testdata/traffic-shifting.yaml")
			for _, c := range ctx.Environment().Clusters() {
				cp, err := ctx.Environment().GetControlPlaneCluster(c)
				if err != nil {
					scopes.Framework.Warnf("failed to get control-plane for cluster %d; assuming it is the control-plane", c.Index())
					cp = c
				}
				ctx.NewSubTest(fmt.Sprintf("from-%d", c.Index())).
					Run(func(ctx framework.TestContext) {
						deployment := tmpl.EvaluateOrFail(ctx, vsTmpl, VirtualServiceConfig{
							Name:       fmt.Sprintf("x-cluster-shifting-rule-%d", cp.Index()),
							Namespace:  ns.Name(),
							TargetHost: svcs[c.Index()][1].Config().Service,
							Hosts:      hosts,
							Weights:    weights,
						})

						ctx.Config(cp).ApplyYAMLOrFail(ctx, ns.Name(), deployment)

						src, dst := svcs[c.Index()][0], svcs[c.Index()][1]
						sendTraffic(ctx, 100, src, dst, hosts, weights, float64(weights[0])*.25)
					})
			}
		})

}

// Wrapper to initialize instance with ServicePort without affecting other tests
// If ServicePort is set to InstancePort, tests such as TestDescribe would fail.
func echoVMConfig(ns namespace.Instance, name string, vmImage ...string) echo.Config {
	image := ""
	if len(vmImage) > 0 {
		image = vmImage[0]
	}
	config := echoConfig(ns, name)
	config.DeployAsVM = image != ""
	config.VMImage = image

	// This is necessary because there exists a bug in WorkloadEntry
	// The ServicePort has to be the same with the InstancePort
	config.Ports[0].ServicePort = config.Ports[0].InstancePort
	return config
}

func sendTraffic(t test.Failer, batchSize int, from, to echo.Instance, hosts []string, weight []int, errorThreshold float64) {
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

		err = nil
		for i, v := range hosts {
			percentOfTrafficToHost := float64(hitCount[v]) * 100.0 / float64(totalRequests)
			deltaFromExpected := math.Abs(float64(weight[i]) - percentOfTrafficToHost)
			if errorThreshold-deltaFromExpected < 0 {
				err = multierror.Append(err, fmt.Errorf("unexpected traffic weight for host %v. Expected %d%%, got %g%% (thresold: %g%%)",
					v, weight[i], percentOfTrafficToHost, errorThreshold))
			} else {
				scopes.Framework.Debugf("Got expected traffic weight for host %v. Expected %d%%, got %g%% (thresold: %g%%)",
					v, weight[i], percentOfTrafficToHost, errorThreshold)
			}
		}
		return err
	}, retry.Delay(time.Second))
}
