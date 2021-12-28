//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package logging

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	ist        istio.Instance
	echoNsInst namespace.Instance
	ing        ingress.Instance
	srv        echo.Instance
	clt        echo.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, setupConfig)).
		Setup(testSetup).
		Run()
}

func TestLogging(t *testing.T) {
	framework.
		NewTest(t).
		Features("observability.telemetry.logging").
		Run(func(ctx framework.TestContext) {
			if err := sendTraffic(t); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func testSetup(ctx resource.Context) (err error) {
	echoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		fmt.Println("setup ns err: ", err)
		return
	}

	_, err = echoboot.NewBuilder(ctx).
		With(&clt, echo.Config{
			Service:        "clt",
			Namespace:      echoNsInst,
			ServiceAccount: true,
		}).
		With(&srv, echo.Config{
			Service:   "srv",
			Namespace: echoNsInst,
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// We use a port > 1024 to not require root
					InstancePort: 8888,
				},
			},
			ServiceAccount: true,
		}).
		Build()
	if err != nil {
		return
	}

	ing = ist.IngressFor(ctx.Clusters().Default())

	return nil
}

func setupConfig(ctx resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  extensionProviders:
  - name: file-log
    envoyFileAccessLog:
      path: /dev/stdout
  defaultProviders:
    accessLogging:
    - file-log
`
}

func sendTraffic(t *testing.T) error {
	t.Helper()

	t.Logf("Sending 5 requests...")
	httpOpts := echo.CallOptions{
		Target:   srv,
		PortName: "http",
		Count:    5,
	}
	if _, err := clt.Call(httpOpts); err != nil {
		return err
	}

	// wait for log
	time.Sleep(10 * time.Second)

	// TODO: find a better way to do this
	workloads, _ := clt.Workloads()
	logCount := 0
	for _, w := range workloads {
		s, _ := w.Sidecar().Logs()
		logCount += countAccessLog(s)
	}

	if logCount != httpOpts.Count {
		return fmt.Errorf("error log count: %d", logCount)
	}

	return nil
}

func countAccessLog(s string) int {
	lines := strings.Split(s, "\n")
	total := len(lines)
	count := 0
	for i := total - 1; i >= 0; i-- {
		fmt.Println(lines[i])
		if strings.Contains(lines[i], "via_upstream") {
			count++
		}
	}

	return count
}
