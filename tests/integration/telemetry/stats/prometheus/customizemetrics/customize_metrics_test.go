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

package customizemetrics

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var (
	client, server echo.Instance
	appNsInst      namespace.Instance
	promInst       prometheus.Instance
)

const removedTag = "source_principal"

func TestCustomizeMetrics(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stats.prometheus.customize-metric").
		Run(func(ctx framework.TestContext) {
			destinationQuery := buildQuery()
			var metricVal string
			retry.UntilSuccessOrFail(t, func() error {
				if err := sendTraffic(t); err != nil {
					t.Errorf("failed to send traffic")
					return err
				}
				var err error
				metricVal, err = common.QueryPrometheus(t, ctx.Clusters().Default(), destinationQuery, promInst)
				if err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(ctx.Clusters().Default(), promInst, "istio_requests_total"))
					return err
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(90*time.Second))
			// check tag removed
			if strings.Contains(metricVal, removedTag) {
				t.Errorf("failed to remove tag: %v", removedTag)
			}
			common.ValidateMetric(t, ctx.Clusters().Default(), promInst, destinationQuery, "istio_requests_total", 1)
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(common.GetIstioInstance(), setupConfig)).
		Setup(testSetup).
		Setup(setupEnvoyFilter).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
	appNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	_, err = echoboot.NewBuilder(ctx).
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

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfValue := `
values:
 telemetry:
   v2:
     prometheus:
       configOverride:
         inboundSidecar:
           debug: false
           stat_prefix: istio
           metrics:
           - name: requests_total
             dimensions:
               response_code: istio_responseClass
               request_operation: istio_operationId
             tags_to_remove:
             - %s
`
	cfg.ControlPlaneValues = fmt.Sprintf(cfValue, removedTag)
}

func setupEnvoyFilter(ctx resource.Context) error {
	content, err := ioutil.ReadFile("testdata/attributegen_envoy_filter.yaml")
	if err != nil {
		return err
	}
	if err := ctx.Config().ApplyYAML(appNsInst.Name(), string(content)); err != nil {
		return err
	}

	return nil
}

func sendTraffic(t *testing.T) error {
	t.Helper()
	httpOpts := echo.CallOptions{
		Target:   server,
		PortName: "http",
		Path:     "/path",
		Count:    1,
		Method:   "GET",
	}

	if _, err := client.Call(httpOpts); err != nil {
		return err
	}

	httpOpts.Method = "POST"
	if _, err := client.Call(httpOpts); err != nil {
		return err
	}

	return nil
}

func buildQuery() (destinationQuery string) {
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "2xx",
		"request_operation":              "getoperation",
		"destination_app":                "server",
		"destination_version":            "v1",
		"destination_service":            "server." + appNsInst.Name() + ".svc.cluster.local",
		"destination_service_name":       "server",
		"destination_workload_namespace": appNsInst.Name(),
		"destination_service_namespace":  appNsInst.Name(),
		"source_app":                     "client",
		"source_version":                 "v1",
		"source_workload":                "client-v1",
		"source_workload_namespace":      appNsInst.Name(),
	}

	_, destinationQuery, _ = common.BuildQueryCommon(labels, appNsInst.Name())
	return
}
