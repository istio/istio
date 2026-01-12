//go:build integ

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

package api

import (
	_ "embed"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
)

const (
	removedTag   = "source_principal"
	httpProtocol = "http"
	grpcProtocol = "grpc"
)

func TestCustomizeMetrics(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			setupWasmExtension(t)
			t.ConfigIstio().YAML(apps.Namespace.Name(), `
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: ns-default
spec:
  metrics:
  - providers:
    - name: prometheus
    overrides:
    - match:
        metric: REQUEST_COUNT
      tagOverrides:
        response_code:
          value: filter_state["wasm.istio_responseClass"]
        request_operation: 
          value: filter_state["wasm.istio_operationId"]
        grpc_response_status: 
          value: filter_state["wasm.istio_grpcResponseStatus"]
        custom_dimension: 
          value: "'test'"
        source_principal:
          operation: REMOVE

`).ApplyOrFail(t)
			t.Cleanup(func() {
				if t.Failed() {
					util.PromDump(t.Clusters().Default(), promInst, prometheus.Query{Metric: "istio_requests_total"})
				}
			})
			httpDestinationQuery := buildCustomMetricsQuery(httpProtocol)
			grpcDestinationQuery := buildCustomMetricsQuery(grpcProtocol)
			var httpMetricVal string
			cluster := t.Clusters().Default()
			httpChecked := false
			retry.UntilSuccessOrFail(t, func() error {
				if err := sendCustomizeMetricsTraffic(); err != nil {
					t.Log("failed to send traffic")
					return err
				}
				var err error
				if !httpChecked {
					httpMetricVal, err = util.QueryPrometheus(t, cluster, httpDestinationQuery, promInst)
					if err != nil {
						util.PromDiff(t, promInst, cluster, httpDestinationQuery)
						return err
					}
					httpChecked = true
				}
				_, err = util.QueryPrometheus(t, cluster, grpcDestinationQuery, promInst)
				if err != nil {
					util.PromDiff(t, promInst, cluster, grpcDestinationQuery)
					return err
				}
				return nil
			}, retry.Delay(1*time.Second), retry.Timeout(300*time.Second))
			// check tag removed
			if strings.Contains(httpMetricVal, removedTag) {
				t.Errorf("failed to remove tag: %v", removedTag)
			}
			util.ValidateMetric(t, cluster, promInst, httpDestinationQuery, 1)
			util.ValidateMetric(t, cluster, promInst, grpcDestinationQuery, 1)
			// By default, envoy histogram has 20 buckets, annotation changes it to 10
			if err := ValidateBucket(cluster, promInst, "a", "destination", 10); err != nil {
				t.Errorf("failed to validate bucket: %v", err)
			}
		})
}

func setupWasmExtension(t framework.TestContext) {
	isKind := t.Clusters().IsKindCluster()

	// By default, for any platform, the test will pull the test image from public "gcr.io" registry.
	// For "Kind" environment, it will pull the images from the "kind-registry".
	// For "Kind", this is due to DNS issues in IPv6 cluster
	attrGenImageTag := "359dcd3a19f109c50e97517fe6b1e2676e870c4d"
	if isKind {
		attrGenImageTag = "0.0.1"
	}
	attrGenImageURL := fmt.Sprintf("oci://%v/istio-testing/wasm/attributegen:%v", registry.Address(), attrGenImageTag)
	args := map[string]any{
		"AttributeGenURL": attrGenImageURL,
	}
	t.ConfigIstio().
		EvalFile(apps.Namespace.Name(), args, "testdata/attributegen.yaml").
		ApplyOrFail(t)
}

func sendCustomizeMetricsTraffic() error {
	for _, cltInstance := range GetClientInstances() {
		httpOpts := echo.CallOptions{
			To: GetTarget(),
			Port: echo.Port{
				Name: "http",
			},
			HTTP: echo.HTTP{
				Path:   "/path",
				Method: "GET",
			},
			Retry: echo.Retry{
				NoRetry: true,
			},
		}

		if _, err := cltInstance.Call(httpOpts); err != nil {
			return err
		}

		httpOpts.HTTP.Method = "POST"
		if _, err := cltInstance.Call(httpOpts); err != nil {
			return err
		}

		grpcOpts := echo.CallOptions{
			To: GetTarget(),
			Port: echo.Port{
				Name: "grpc",
			},
		}
		if _, err := cltInstance.Call(grpcOpts); err != nil {
			return err
		}
	}

	return nil
}

func buildCustomMetricsQuery(protocol string) (destinationQuery prometheus.Query) {
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "2xx",
		"destination_app":                "b",
		"destination_version":            "v1",
		"destination_service":            "b." + apps.Namespace.Name() + ".svc.cluster.local",
		"destination_service_name":       "b",
		"destination_workload_namespace": apps.Namespace.Name(),
		"destination_service_namespace":  apps.Namespace.Name(),
		"source_app":                     "a",
		"source_version":                 "v1",
		"source_workload":                "a-v1",
		"source_workload_namespace":      apps.Namespace.Name(),
		"custom_dimension":               "test",
	}
	if protocol == httpProtocol {
		labels["request_operation"] = "getoperation"
	}
	if protocol == grpcProtocol {
		labels["grpc_response_status"] = "OK"
		labels["request_protocol"] = "grpc"
	}

	_, destinationQuery, _ = BuildQueryCommon(labels, apps.Namespace.Name())
	return destinationQuery
}
