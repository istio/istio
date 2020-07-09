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
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/pilot/vm"
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
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "traffic-shifting",
				Inject: true,
			})

			var instances [4]echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instances[0], echoConfig(ns, "a")).
				With(&instances[1], echoConfig(ns, "b")).
				With(&instances[2], echoConfig(ns, "c")).
				With(&instances[3], echoConfig(ns, "d")).
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

					vm.SendTraffic(t, 100, instances[0], instances[1], hosts, v, errorThreshold)
				})
			}
		})
}
