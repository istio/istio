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

package http

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/features"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
	promUtil "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var (
	client, server echo.Instance
	ist            istio.Instance
	appNsInst      namespace.Instance
	promInst       prometheus.Instance
)

// GetIstioInstance gets Istio instance.
func GetIstioInstance() *istio.Instance {
	return &ist
}

// GetAppNamespace gets bookinfo instance.
func GetAppNamespace() namespace.Instance {
	return appNsInst
}

// GetPromInstance gets prometheus instance.
func GetPromInstance() prometheus.Instance {
	return promInst
}

// TestStatsFilter includes common test logic for stats and mx exchange filters running
// with nullvm and wasm runtime.
func TestStatsFilter(t *testing.T, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			sourceQuery, destinationQuery, appQuery := buildQuery()
			retry.UntilSuccessOrFail(t, func() error {
				if err := SendTraffic(); err != nil {
					return err
				}
				// Query client side metrics
				if err := promUtil.QueryPrometheus(t, sourceQuery, GetPromInstance()); err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(promInst, "istio_requests_total"))
					return err
				}
				if err := promUtil.QueryPrometheus(t, destinationQuery, GetPromInstance()); err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(promInst, "istio_requests_total"))
					return err
				}
				// This query will continue to increase due to readiness probe; don't wait for it to converge
				if err := promUtil.QueryFirstPrometheus(t, appQuery, GetPromInstance()); err != nil {
					t.Logf("prometheus values for istio_echo_http_requests_total: \n%s", util.PromDump(promInst, "istio_echo_http_requests_total"))
					return err
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
		})
}

// TestSetup set up bookinfo app for stats testing.
func TestSetup(ctx resource.Context) (err error) {
	appNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	err = echoboot.NewBuilder(ctx).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: appNsInst,
			Ports:     nil,
			Subsets:   []echo.SubsetConfig{{}},
		}).
		With(&server, echo.Config{
			Service:   "server",
			Namespace: appNsInst,
			Subsets:   []echo.SubsetConfig{{}},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
			},
		}).
		Build()
	if err != nil {
		return err
	}
	promInst, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return
	}
	return nil
}

// SendTraffic makes a client call to the "server" service on the http port.
func SendTraffic() error {
	_, err := client.Call(echo.CallOptions{
		Target:   server,
		PortName: "http",
	})
	return err
}

func SetupStrictMTLS(ctx resource.Context) error {
	return ctx.Config().ApplyYAML(appNsInst.Name(), fmt.Sprintf(`
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: %s
spec:
  mtls:
    mode: STRICT`, appNsInst.Name()))
}

func buildQuery() (sourceQuery, destinationQuery, appQuery string) {
	ns := GetAppNamespace()
	sourceQuery = `istio_requests_total{reporter="source",`
	destinationQuery = `istio_requests_total{reporter="destination",`
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "200",
		"destination_app":                "server",
		"destination_version":            "v1",
		"destination_service":            "server." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "server",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "client",
		"source_version":                 "v1",
		"source_workload":                "client-v1",
		"source_workload_namespace":      ns.Name(),
	}
	for k, v := range labels {
		sourceQuery += fmt.Sprintf(`%s=%q,`, k, v)
		destinationQuery += fmt.Sprintf(`%s=%q,`, k, v)
	}
	sourceQuery += "}"
	destinationQuery += "}"
	appQuery += `istio_echo_http_requests_total{kubernetes_namespace="` + ns.Name() + `"}`
	return
}
