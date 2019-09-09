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

package pilot

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/structpath"
	"istio.io/istio/pkg/test/util/tmpl"
)

//	Virtual service topology
//
//	    a                      b                     c
//	|-------|             |-------|    mirror   |-------|
//	| Host0 | ----------> | Host1 | ----------> | Host2 |
//	|-------|             |-------|             |-------|
//

type VirtualServiceMirrorConfig struct {
	Name      string
	Namespace string
	Absent    bool
	Percent   float64
}

type testCaseMirror struct {
	name       string
	absent     bool
	percentage float64
	threshold  float64
}

func TestMirroring(t *testing.T) {
	cases := []testCaseMirror{
		{
			name:       "mirror-percent-absent",
			absent:     true,
			percentage: 100.0,
			threshold:  0.0,
		},
		{
			name:       "mirror-50",
			percentage: 50.0,
			threshold:  10.0,
		},
		{
			name:       "mirror-10",
			percentage: 10.0,
			threshold:  5.0,
		},
		{
			name:       "mirror-80",
			percentage: 80.0,
			threshold:  10.0,
		},
		{
			name:       "mirror-0",
			percentage: 0.0,
			threshold:  0.0,
		},
	}

	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "mirroring",
				Inject: true,
			})

			var instances [3]echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instances[0], echoConfig(ns, "a")). // client
				With(&instances[1], echoConfig(ns, "b")). // target
				With(&instances[2], echoConfig(ns, "c")). // receives mirrored requests
				BuildOrFail(t)

			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					vsc := VirtualServiceMirrorConfig{
						c.name,
						ns.Name(),
						c.absent,
						c.percentage,
					}

					deployment := tmpl.EvaluateOrFail(t,
						file.AsStringOrFail(t, "testdata/traffic-mirroring-template.yaml"), vsc)
					g.ApplyConfigOrFail(t, ns, deployment)
					defer g.DeleteConfigOrFail(t, ns, deployment)

					workloads, err := instances[0].Workloads()
					if err != nil {
						t.Fatalf("Failed to get workloads. Error: %v", err)
					}

					for _, w := range workloads {
						if err = w.Sidecar().WaitForConfig(func(cfg *envoyAdmin.ConfigDump) (bool, error) {
							validator := structpath.ForProto(cfg)
							if err = checkIfMirrorWasApplied(instances[1], instances[2], c, validator); err != nil {
								return false, err
							}
							return true, nil
						}); err != nil {
							t.Fatalf("Failed to apply configuration. Error: %v", err)
						}
					}

					testID := util.RandomString(16)
					sendTrafficMirror(t, instances, testID)
					verifyTrafficMirror(t, instances, c, testID)
				})
			}
		})
}

func checkIfMirrorWasApplied(target, mirror echo.Instance, tc testCaseMirror, validator *structpath.Instance) error {
	for _, port := range target.Config().Ports {
		vsName := vsName(target, port)
		instance := validator.Select(
			"{.configs[*].dynamicRouteConfigs[*].routeConfig.virtualHosts[?(@.name == %q)].routes[*].route}",
			vsName)

		if tc.percentage > 0 {
			instance.Exists("{.requestMirrorPolicy}")

			clusterName := clusterName(mirror, port)
			instance.Equals(clusterName, "{.requestMirrorPolicy.cluster}")

			instance.Equals(tc.percentage, "{.requestMirrorPolicy.runtimeFraction.defaultValue.numerator}")
		} else {
			instance.NotExists("{.requestMirrorPolicy}")
		}

		if err := instance.Check(); err != nil {
			return err
		}
	}
	return nil
}

func vsName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("%s.%s.svc.%s:%d", cfg.Service, cfg.Namespace.Name(), cfg.Domain, port.ServicePort)
}

func sendTrafficMirror(t *testing.T, instances [3]echo.Instance, testID string) {
	const totalThreads = 10
	errs := make(chan error, totalThreads)

	wg := sync.WaitGroup{}
	wg.Add(totalThreads)

	for i := 0; i < totalThreads; i++ {
		go func() {
			_, err := instances[0].Call(echo.CallOptions{
				Target:   instances[1],
				PortName: "http",
				Path:     "/" + testID,
				Count:    50,
			})
			if err != nil {
				errs <- err
			}
			wg.Done()
		}()
	}

	wg.Wait()
	close(errs)

	var callerrors error
	for err := range errs {
		callerrors = multierror.Append(err, callerrors)
	}
	if callerrors != nil {
		t.Fatalf("Error occurred during call: %v", callerrors)
	}
}

func verifyTrafficMirror(t *testing.T, instances [3]echo.Instance, tc testCaseMirror, testID string) {
	_, err := retry.Do(func() (interface{}, bool, error) {
		countB := logCount(t, instances[1], testID)
		countC := logCount(t, instances[2], testID)
		actualPercent := (countC / countB) * 100
		deltaFromExpected := math.Abs(actualPercent - tc.percentage)

		if tc.threshold-deltaFromExpected < 0 {
			err := fmt.Errorf("unexpected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, testID: %s)",
				tc.percentage, actualPercent, tc.threshold, testID)
			t.Logf("%v", err)
			return nil, false, err
		}

		t.Logf("Got expected mirror traffic. Expected %g%%, got %.1f%% (threshold: %g%%, , testID: %s)",
			tc.percentage, actualPercent, tc.threshold, testID)
		return nil, true, nil
	}, retry.Delay(time.Second))

	if err != nil {
		t.Fatalf("%v", err)
	}
}

func logCount(t *testing.T, instance echo.Instance, testID string) float64 {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf("Failed to get workloads. Error: %v", err)
	}

	var logs string
	for _, w := range workloads {
		logs += w.Sidecar().LogsOrFail(t)
	}

	return float64(strings.Count(logs, testID))
}
