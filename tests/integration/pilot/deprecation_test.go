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
	"bufio"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// This test checks sidecar logs to see if there's any deprecation message.
// We deploy a simple workload and make some requests from `a` to `b` just
// to generate some traffic and make sure sidecars were configured and are
// running properly.
func TestDeprecations(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "deprecation",
				Inject: true,
			})

			var instances [2]echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instances[0], echoConfig(ns, "a")).
				With(&instances[1], echoConfig(ns, "b")).
				BuildOrFail(t)

			// Generate dummy traffic
			_, err := instances[0].Call(echo.CallOptions{
				Target:   instances[1],
				PortName: "http",
				Count:    10,
			})
			if err != nil {
				t.Fatalf("error calling service b: %v", err)
			}

			// Check the logs for deprecation messages
			for _, instance := range instances {
				for _, w := range instance.WorkloadsOrFail(t) {
					logs := w.Sidecar().LogsOrFail(t)
					scanner := bufio.NewScanner(strings.NewReader(logs))
					for scanner.Scan() {
						line := scanner.Text()
						if strings.Contains(strings.ToLower(line), "deprecated") {
							t.Fatalf("usage of deprecated stuff in Envoy: %s", line)
						}
					}
				}
			}

		})
}
