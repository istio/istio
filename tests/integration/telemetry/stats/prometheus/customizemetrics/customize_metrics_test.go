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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	util "istio.io/istio/tests/integration/telemetry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var (
	client, server echo.Instances
	appNsInst      namespace.Instance
	promInst       prometheus.Instance
)

const (
	removedTag            = "source_principal"
	requestCountMultipler = 3
	httpProtocol          = "http"
	grpcProtocol          = "grpc"
)

func TestCustomizeMetrics(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stats.prometheus.customize-metric").
		Features("observability.telemetry.request-classification").
		Features("extensibility.wasm.remote-load").
		Run(func(ctx framework.TestContext) {
			httpDestinationQuery := buildQuery(httpProtocol)
			grpcDestinationQuery := buildQuery(grpcProtocol)
			var httpMetricVal string
			cluster := ctx.Clusters().Default()
			httpChecked := false
			retry.UntilSuccessOrFail(t, func() error {
				if err := sendTraffic(t); err != nil {
					t.Log("failed to send traffic")
					return err
				}
				var err error
				if !httpChecked {
					httpMetricVal, err = common.QueryPrometheus(t, ctx.Clusters().Default(), httpDestinationQuery, promInst)
					if err != nil {
						t.Logf("http: prometheus values for istio_requests_total: \n%s", util.PromDump(ctx.Clusters().Default(), promInst, "istio_requests_total"))
						return err
					}
					httpChecked = true
				}
				_, err = common.QueryPrometheus(t, ctx.Clusters().Default(), grpcDestinationQuery, promInst)
				if err != nil {
					t.Logf("grpc: prometheus values for istio_requests_total: \n%s", util.PromDump(ctx.Clusters().Default(), promInst, "istio_requests_total"))
					return err
				}
				return nil
			}, retry.Delay(6*time.Second), retry.Timeout(300*time.Second))
			// check tag removed
			if strings.Contains(httpMetricVal, removedTag) {
				t.Errorf("failed to remove tag: %v", removedTag)
			}
			common.ValidateMetric(t, cluster, promInst, httpDestinationQuery, "istio_requests_total", 1)
			common.ValidateMetric(t, cluster, promInst, grpcDestinationQuery, "istio_requests_total", 1)
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(common.GetIstioInstance(), setupConfig)).
		Setup(setupEnvoyFilter).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
	enableBootstrapDiscovery := `
proxyMetadata:
  BOOTSTRAP_XDS_AGENT: "true"`

	echos, err := echoboot.NewBuilder(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(echo.Config{
			Service:   "client",
			Namespace: appNsInst,
			Ports:     nil,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarProxyConfig: {
							Value: enableBootstrapDiscovery,
						},
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:   "server",
			Namespace: appNsInst,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarProxyConfig: {
							Value: enableBootstrapDiscovery,
						},
					},
				},
			},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
				{
					Name:         "grpc",
					Protocol:     protocol.GRPC,
					InstancePort: 7070,
				},
			},
		}).
		Build()
	if err != nil {
		return err
	}
	client = echos.Match(echo.Service("client"))
	server = echos.Match(echo.Service("server"))
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
               grpc_response_status: istio_grpcResponseStatus
               custom_dimension: "'test'"
             tags_to_remove:
             - %s
`
	cfg.ControlPlaneValues = fmt.Sprintf(cfValue, removedTag)
}

func setupEnvoyFilter(ctx resource.Context) error {
	var nsErr error
	appNsInst, nsErr = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if nsErr != nil {
		return nsErr
	}
	proxyDepFile := path.Join(env.IstioSrc, "istio.deps")
	depJSON, err := ioutil.ReadFile(proxyDepFile)
	if err != nil {
		return err
	}
	var deps []interface{}
	if err := json.Unmarshal(depJSON, &deps); err != nil {
		return err
	}
	proxySHA := ""
	for _, d := range deps {
		if dm, ok := d.(map[string]interface{}); ok && dm["repoName"].(string) == "proxy" {
			proxySHA = dm["lastStableSHA"].(string)
		}
	}
	content, err := ioutil.ReadFile("testdata/attributegen_envoy_filter.yaml")
	if err != nil {
		return err
	}
	attrGenURL := fmt.Sprintf("https://storage.googleapis.com/istio-build/proxy/attributegen-%v.wasm", proxySHA)
	useRemoteWasmModule := false
	resp, err := http.Get(attrGenURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		useRemoteWasmModule = true
	}
	con, err := tmpl.Evaluate(string(content), map[string]interface{}{
		"WasmRemoteLoad":  useRemoteWasmModule,
		"AttributeGenURL": attrGenURL,
	})
	if err != nil {
		return err
	}
	if err := ctx.Config().ApplyYAML(appNsInst.Name(), con); err != nil {
		return err
	}

	// enable custom tag in the stats
	bootstrapPatch := `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: bootstrap-tag
  namespace: istio-system
spec:
  configPatches:
    - applyTo: BOOTSTRAP
      patch:
        operation: MERGE
        value:
          stats_config:
            stats_tags:
            - regex: "(custom_dimension=\\.=(.*?);\\.;)"
              tag_name: "custom_dimension"
`
	if err := ctx.Config().ApplyYAML("istio-system", bootstrapPatch); err != nil {
		return err
	}
	// Ensure bootstrap patch is applied before starting echo.
	time.Sleep(time.Minute)
	return nil
}

func sendTraffic(t *testing.T) error {
	t.Helper()
	for _, cltInstance := range client {
		count := requestCountMultipler * len(server)
		httpOpts := echo.CallOptions{
			Target:   server[0],
			PortName: "http",
			Path:     "/path",
			Count:    count,
			Method:   "GET",
		}

		if _, err := cltInstance.Call(httpOpts); err != nil {
			return err
		}

		httpOpts.Method = "POST"
		if _, err := cltInstance.Call(httpOpts); err != nil {
			return err
		}

		grpcOpts := echo.CallOptions{
			Target:   server[0],
			PortName: "grpc",
			Count:    count,
		}
		if _, err := cltInstance.Call(grpcOpts); err != nil {
			return err
		}
	}

	return nil
}

func buildQuery(protocol string) (destinationQuery string) {
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "2xx",
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
		"custom_dimension":               "test",
	}
	if protocol == httpProtocol {
		labels["request_operation"] = "getoperation"
	}
	if protocol == grpcProtocol {
		labels["grpc_response_status"] = "OK"
		labels["request_protocol"] = "grpc"
	}

	_, destinationQuery, _ = common.BuildQueryCommon(labels, appNsInst.Name())
	return destinationQuery
}
