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
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	// Error threshold. For example, we expect 25% traffic, traffic distribution within [15%, 35%] is accepted.
	errorThreshold = 10.0
)

type TrafficShiftConfig struct {
	Name      string
	Namespace string
	Host      string
	Subset0   string
	Subset1   string
	Weight0   int32
	Weight1   int32
}

func TestTrafficShifting(t *testing.T) {
	weights := map[string][]int32{
		"20-80": {20, 80},
		"50-50": {50, 50},
	}
	framework.
		NewTest(t).
		Features("traffic.shifting").
		Run(func(ctx framework.TestContext) {
			ns = namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "vm-traffic-shifting",
				Inject: true,
			})

			ports := []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  8090,
					InstancePort: 10090,
				},
			}

			var client, vm echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echo.Config{
					Service:   "client",
					Namespace: ns,
					Ports:     ports,
				}).
				With(&vm, echo.Config{
					Service:    "vm",
					Namespace:  ns,
					Ports:      ports,
					DeployAsVM: true,
					VMImage:    DefaultVMImage,
				}).
				// now launch another proper k8s pod for the same VM service, such that the VM service is made
				// of a pod and a VM. Set the subset for this pod to v2 so that we can check traffic shift
				// Traffic distribution
				With(&vm, echo.Config{
					Service:    "vm",
					Namespace:  ns,
					Ports:      ports,
					DeployAsVM: false,
					Version:    "v2",
					Subsets: []echo.SubsetConfig{
						{
							Version: "v2",
						},
					},
				}).
				BuildOrFail(t)

			for k, v := range weights {
				ctx.NewSubTest(k).
					Run(func(ctx framework.TestContext) {
						vsc := TrafficShiftConfig{
							"traffic-shifting-rule-wle",
							ns.Name(),
							"vm",
							"v1",
							"v2",
							v[0],
							v[1],
						}
						deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/traffic-shifting.yaml"), vsc)
						ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)
						SendTraffic(t, 100, client, vm, []string{"v1", "v2"}, v, errorThreshold)
					})
			}
		})
}
