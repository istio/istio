//go:build integ
// +build integ

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

package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

func TestAccessLogs(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("enabled").Run(func(t framework.TestContext) {
				applyTelemetryResource(t, true)
				runAccessLogsTests(t, true, false)
				deleteTelemetryResource(t, true)
			})
			t.NewSubTest("enabled-with-targetref").Run(func(t framework.TestContext) {
				args := map[string]any{
					"To":                GetTarget().(echo.Instances),
					"TargetGatewayName": GetTarget().ServiceName() + "-gateway",
					"Address":           fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", GetTarget().ServiceName(), apps.Namespace.Name()),
				}
				crd.DeployGatewayAPIOrSkip(t)
				t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "./testdata/gateway-api.yaml").ApplyOrFail(t)
				applyTelemetryResourceWithTargetRef(t, true)
				// We should not get logs from the client, since the policy only applies to the Gateway.
				runAccessLogsTests(t, false, true)
				deleteTelemetryResource(t, true)
			})
			t.NewSubTest("disabled").Run(func(t framework.TestContext) {
				applyTelemetryResource(t, false)
				runAccessLogsTests(t, false, false)
				deleteTelemetryResource(t, false)
			})
		})
}

func TestAccessLogsFilter(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			runAccessLogFilterTests(t, false)
			t.ConfigIstio().File(apps.Namespace.Name(), "./testdata/accesslog/filter.yaml").ApplyOrFail(t)
			runAccessLogFilterTests(t, true)
		})
}

func TestAccessLogsMode(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("client").Run(func(t framework.TestContext) {
				t.ConfigIstio().File(apps.Namespace.Name(), "./testdata/accesslog/mode-client.yaml").ApplyOrFail(t)
				runAccessLogModeTests(t, true, false)
			})
			t.NewSubTest("server").Run(func(t framework.TestContext) {
				t.ConfigIstio().File(apps.Namespace.Name(), "./testdata/accesslog/mode-server.yaml").ApplyOrFail(t)
				runAccessLogModeTests(t, false, true)
			})
			t.NewSubTest("client-and-server").Run(func(t framework.TestContext) {
				t.ConfigIstio().File(apps.Namespace.Name(), "./testdata/accesslog/mode-clientserver.yaml").ApplyOrFail(t)
				runAccessLogModeTests(t, true, true)
			})
		})
}

func TestAccessLogsDefaultProvider(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("disabled").Run(func(t framework.TestContext) {
				runAccessLogsTests(t, false, false)
			})
			t.NewSubTest("enabled").Run(func(t framework.TestContext) {
				cfg := `
defaultProviders:
  accessLogging:
  - envoy
`
				ist.PatchMeshConfigOrFail(t, cfg)
				runAccessLogsTests(t, true, false)
			})
		})
}

func TestAccessLogWithFilterState(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.ConfigIstio().File(apps.Namespace.Name(), "./testdata/accesslog/enable-filter-state-log.yaml").ApplyOrFail(t)
			to := GetTarget()
			from := GetClientInstances()[0]
			err := retry.UntilSuccess(func() error {
				from.CallOrFail(t, echo.CallOptions{
					To: to,
					Port: echo.Port{
						Name: "http",
					},
					HTTP: echo.HTTP{
						Path: "/filter-state-test",
					},
				})
				lines := logs(t, to, "filter-state-test")
				if len(lines) == 0 {
					return errors.New("no logs found")
				}
				// if FILTER_STATE is not working, then the log will look like this:
				// `/filter-state-test - -`
				target := fmt.Sprintf("/%s - -", "filter-state-test")
				for _, line := range lines {
					t.Logf("line: %s", line)
					if line == target {
						t.Logf("FILTER_STATE is not working, logs: %s", line)
						return errors.New("FILTER_STATE is not working")
					}
				}
				return nil
			}, retry.Timeout(framework.TelemetryRetryTimeout))
			if err != nil {
				t.Fatalf("expected FILTER_STATE but got nil, err: %v", err)
			}
		})
}

func applyTelemetryResourceWithTargetRef(t framework.TestContext, enableLogs bool) {
	args := map[string]any{
		"TargetGatewayName": GetTarget().ServiceName() + "-gateway",
		"DisableLogs":       !enableLogs,
	}
	t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "./testdata/accesslog/targetref.yaml").ApplyOrFail(t)
}

func applyTelemetryResource(t framework.TestContext, enableLogs bool) {
	config := fmt.Sprintf(`apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: logs
spec:
  accessLogging:
  - providers:
    - name: envoy
    disabled: %v
`, !enableLogs)
	t.ConfigIstio().YAML(apps.Namespace.Name(), config).ApplyOrFail(t)
}

func deleteTelemetryResource(t framework.TestContext, enableLogs bool) {
	config := fmt.Sprintf(`apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: logs
spec:
  accessLogging:
  - disabled: %v
`, enableLogs)
	t.ConfigIstio().YAML(apps.Namespace.Name(), config).DeleteOrFail(t)
}

func runAccessLogsTests(t framework.TestContext, expectLogs bool, hasTargetRef bool) {
	testID := rand.String(16)
	to := GetTarget()
	if len(GetClientInstances()) == 0 {
		t.Fatal("there is no client")
	}
	cltInstance := GetClientInstances()[0]
	from := GetClientInstances()[0]
	var count float64
	if expectLogs {
		var http echo.HTTP
		if hasTargetRef {
			http = echo.HTTP{
				Path:    "/" + testID,
				Method:  "GET",
				Headers: headers.New().WithHost(fmt.Sprintf("%s.com", GetTarget().ServiceName())).Build(),
			}
		} else {
			http = echo.HTTP{
				Path: "/" + testID,
			}
		}
		// For positive test, we use the same ID and repeatedly send requests and check the count
		// Retry a bit to get the logs. There is some delay before they are output(MeshConfig will not take effect immediately),
		// so they may not be immediately ready. If not ready, we retry sending a call again.
		err := retry.UntilSuccess(func() error {
			if hasTargetRef {
				hostname := fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", GetTarget().ServiceName(), apps.Namespace.Name())
				defaultOptions := []retry.Option{retry.Delay(100 * time.Millisecond), retry.Timeout(200 * time.Second)}
				httpOpts := echo.CallOptions{
					Address: hostname,
					Port: echo.Port{
						Name:        "http",
						ServicePort: 80,
						Protocol:    protocol.HTTP,
					},
					HTTP:  http,
					Count: 1,
					Retry: echo.Retry{
						Options: append(defaultOptions, retry.Timeout(framework.TelemetryRetryTimeout)),
					},
					Check: check.ResponseHeader(injectedHeader, ""),
				}
				count = logCount(t, from, testID)
				_ = cltInstance.CallOrFail(t, httpOpts)
			} else {
				GetClientInstances()[0].CallOrFail(t, echo.CallOptions{
					To: to,
					Port: echo.Port{
						Name: "http",
					},
					HTTP: http,
				})
				count = logCount(t, to, testID)
			}
			if count > 0 != expectLogs {
				return fmt.Errorf("expected logs '%v', got %v", expectLogs, count)
			}
			return nil
		}, retry.Timeout(framework.TelemetryRetryTimeout))
		if err != nil {
			t.Fatalf("expected logs but got nil, err: %v", err)
		}
	} else {
		// For negative case, we retry with a new ID each time. This ensures that a previous failure
		// (due to hitting old code path with logs still enabled) doesn't stop us from succeeding later
		// once we stop logging.
		retry.UntilSuccessOrFail(t, func() error {
			testID := rand.String(16)
			GetClientInstances()[0].CallOrFail(t, echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path: "/" + testID,
				},
			})
			// This is a negative test; there isn't much we can do other than wait a few seconds and ensure we didn't emit logs
			// Logs should flush every 1s, so 2s should be plenty of time for logs to be emitted
			time.Sleep(time.Second * 2)
			count = logCount(t, to, testID)
			if count > 0 != expectLogs {
				return fmt.Errorf("expected logs '%v', got %v", expectLogs, count)
			}
			return nil
		})
	}
}

func logs(t test.Failer, to echo.Target, testID string) []string {
	var result []string
	for _, w := range to.WorkloadsOrFail(t) {
		l, err := w.Sidecar().Logs()
		if err != nil {
			t.Fatalf("failed getting logs: %v", err)
		}
		split := strings.Split(l, "\n")
		for _, line := range split {
			if c := float64(strings.Count(line, testID)); c > 0 {
				result = append(result, line)
			}
		}
	}

	return result
}

func logCount(t test.Failer, to echo.Target, testID string) float64 {
	counts := map[string]float64{}
	for _, w := range to.WorkloadsOrFail(t) {
		var logs string
		l, err := w.Sidecar().Logs()
		if err != nil {
			t.Fatalf("failed getting logs: %v", err)
		}
		logs += l
		if c := float64(strings.Count(logs, testID)); c > 0 {
			counts[w.Cluster().Name()] = c
		}
	}
	var total float64
	for _, c := range counts {
		total += c
	}
	return total
}

func runAccessLogFilterTests(t framework.TestContext, expectLogs bool) {
	to := GetTarget()
	from := GetClientInstances()[0]
	if expectLogs {
		// For positive test, we use the same path in Telemetry API and repeatedly send requests and check the count
		// Retry a bit to get the logs. There is some delay before they are output(MeshConfig will not take effect immediately),
		// so they may not be immediately ready. If not ready, we retry sending a call again.
		err := retry.UntilSuccess(func() error {
			from.CallOrFail(t, echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path: "/filter-test",
				},
			})
			count := logCount(t, to, "filter-test")
			if count > 0 != expectLogs {
				return fmt.Errorf("expected logs '%v', got %v", expectLogs, count)
			}
			return nil
		}, retry.Timeout(framework.TelemetryRetryTimeout))
		if err != nil {
			t.Fatalf("expected logs but got nil, err: %v", err)
		}
	} else {
		// For negative case, we retry with a new ID each time. This ensures that a previous failure
		// (due to hitting old code path with logs still enabled) doesn't stop us from succeeding later
		// once we stop logging.
		retry.UntilSuccessOrFail(t, func() error {
			testID := rand.String(16)
			from.CallOrFail(t, echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path: "/" + testID,
				},
			})
			// This is a negative test; there isn't much we can do other than wait a few seconds and ensure we didn't emit logs
			// Logs should flush every 1s, so 2s should be plenty of time for logs to be emitted
			time.Sleep(time.Second * 2)
			count := logCount(t, to, testID)
			if count > 0 != expectLogs {
				return fmt.Errorf("expected logs '%v', got %v", expectLogs, count)
			}
			return nil
		})
	}
}

func runAccessLogModeTests(t framework.TestContext, exceptClientLog, exceptServerLog bool) {
	testID := rand.String(16)
	to := GetTarget()
	from := GetClientInstances()[0]
	// For positive test, we use the same path in Telemetry API and repeatedly send requests and check the count
	// Retry a bit to get the logs. There is some delay before they are output(MeshConfig will not take effect immediately),
	// so they may not be immediately ready. If not ready, we retry sending a call again.
	err := retry.UntilSuccess(func() error {
		clientCount := logCount(t, from, testID)
		serverCount := logCount(t, to, testID)

		from.CallOrFail(t, echo.CallOptions{
			To: to,
			Port: echo.Port{
				Name: "http",
			},
			HTTP: echo.HTTP{
				Path: "/" + testID,
			},
		})

		return retry.UntilSuccess(func() error {
			clientDeltaCount := logCount(t, from, testID) - clientCount
			if clientDeltaCount > 0 != exceptClientLog {
				return fmt.Errorf("expected client logs %v but got %v", exceptClientLog, clientDeltaCount)
			}

			serverDeltaCount := logCount(t, to, testID) - serverCount
			if serverDeltaCount > 0 != exceptServerLog {
				return fmt.Errorf("expected server logs %v but got %v", exceptServerLog, serverDeltaCount)
			}

			return nil
		}, retry.Timeout(10*time.Second), retry.Delay(time.Second))
	}, retry.Timeout(framework.TelemetryRetryTimeout))
	if err != nil {
		t.Fatalf("expected logs but got err: %v", err)
	}
}
