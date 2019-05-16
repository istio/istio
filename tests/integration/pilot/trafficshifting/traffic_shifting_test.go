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
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/structpath"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
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
//		-------------------------------------
//		|weight1	|weight2	|weight3	|weight4
//		|b			|c			|d			|e
//	|-------|	|-------|	|-------|	|-------|
//	| Host0 |	| Host1	|	| Host2 |	| Host3 |
//	|-------|	|-------|	|-------|	|-------|
//
//

var (
	ist   istio.Instance
	hosts = []string{"b", "c", "d", "e"}
	p     pilot.Instance
	g     galley.Instance
)

const (
	batchSize = 100

	// Error threshold. For example, we expect 25% traffic, traffic distribution within [15%, 35%] is accepted.
	errorThreshold = 10.0

	testDuration = 10 * time.Second
)

type VirtualServiceConfig struct {
	Name      string
	Host0     string
	Host1     string
	Host2     string
	Host3     string
	Namespace string
	Weight0   int32
	Weight1   int32
	Weight2   int32
	Weight3   int32
}

func TestMain(m *testing.M) {
	framework.NewSuite("traffic_shifting", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Run()
}

func TestTrafficShifting(t *testing.T) {
	// Traffic distribution
	weights := map[string][]int32{
		"20-80":       {20, 80},
		"50-50":       {50, 50},
		"33-33-34":    {33, 33, 34},
		"25-25-25-25": {25, 25, 25, 25},
	}

	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			g, _ = galley.New(ctx, galley.Config{})
			p, _ = pilot.New(ctx, pilot.Config{Galley: g})

			ns := namespace.NewOrFail(t, ctx, "traffic-shifting", true)

			var instances [5]echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instances[0], echoConfig(ns, "a")).
				With(&instances[1], echoConfig(ns, "b")).
				With(&instances[2], echoConfig(ns, "c")).
				With(&instances[3], echoConfig(ns, "d")).
				With(&instances[4], echoConfig(ns, "e")).
				BuildOrFail(t)

			for k, v := range weights {
				t.Run(k, func(t *testing.T) {
					v = append(v, make([]int32, 4-len(v))...)

					vsc := VirtualServiceConfig{
						"traffic-shifting-rule",
						hosts[0],
						hosts[1],
						hosts[2],
						hosts[3],
						ns.Name(),
						v[0],
						v[1],
						v[2],
						v[3],
					}

					deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/traffic-shifting.yaml"), vsc)
					g.ApplyConfigOrFail(t, ns, deployment)

					workloads, err := instances[0].Workloads()
					if err != nil {
						t.Fatalf("Failed to get workloads. Error: %v", err)
					}

					for _, w := range workloads {
						if err = w.Sidecar().WaitForConfig(func(cfg *envoyAdmin.ConfigDump) (bool, error) {
							validator := structpath.ForProto(cfg)
							for i, instance := range instances[1:] {
								for _, p := range instance.Config().Ports {
									if v[i] == 0 {
										continue
									}
									if err = CheckVirtualServiceConfig(instance, p, v[i], validator); err != nil {
										return false, err
									}
								}
							}
							return true, nil
						}); err != nil {
							t.Fatalf("Failed to apply configuration. Error: %v", err)
						}
					}

					sendTraffic(t, testDuration, batchSize, instances[0], instances[1], hosts, v, errorThreshold)
				})
			}
		})
}

func CheckVirtualServiceConfig(target echo.Instance, port echo.Port, weight int32, validator *structpath.Instance) error {
	clusterName := clusterName(target, port)

	instance := validator.Select(
		"{.configs[*].dynamicRouteConfigs[*].routeConfig.virtualHosts[*].routes[*].route.weightedClusters.clusters[?(@.name == %q)]}",
		clusterName)

	return instance.Equals(weight, "{.weight}").Check()
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

func echoConfig(ns namespace.Instance, name string) echo.Config {
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: model.ProtocolHTTP,
			},
		},
		Galley: g,
		Pilot:  p,
	}
}
