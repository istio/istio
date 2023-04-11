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

package customizemetrics

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
	"istio.io/istio/tests/integration/telemetry/common"
)

var (
	client, server echo.Instances
	appNsInst      namespace.Instance
	promInst       prometheus.Instance
	registry       registryredirector.Instance
)

const (
	removedTag   = "source_principal"
	httpProtocol = "http"
	grpcProtocol = "grpc"

	// Same user name and password as specified at pkg/test/fakes/imageregistry
	registryUser   = "user"
	registryPasswd = "passwd"
)

func TestMetricDefinitions(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("observability.telemetry.stats.prometheus.customize-metric").
		Run(func(t framework.TestContext) {
			t.Cleanup(func() {
				if t.Failed() {
					util.PromDump(t.Clusters().Default(), promInst, prometheus.Query{Metric: "istio_custom_total"})
				}
			})
			urlPath := "/custom_path"
			httpSourceQuery := buildCustomQuery(urlPath)
			cluster := t.Clusters().Default()
			retry.UntilSuccessOrFail(t, func() error {
				if err := sendHTTPTraffic(urlPath); err != nil {
					t.Log("failed to send traffic")
					return err
				}
				if _, err := util.QueryPrometheus(t, cluster, httpSourceQuery, promInst); err != nil {
					util.PromDiff(t, promInst, cluster, httpSourceQuery)
					return err
				}
				return nil
			}, retry.Delay(1*time.Second), retry.Timeout(300*time.Second))
			util.ValidateMetric(t, cluster, promInst, httpSourceQuery, 1)
		})
}

func buildCustomQuery(urlPath string) (destinationQuery prometheus.Query) {
	labels := map[string]string{
		"url_path":        urlPath,
		"response_status": "200",
	}
	sourceQuery := prometheus.Query{}
	sourceQuery.Metric = "istio_custom_total"
	sourceQuery.Labels = labels

	return sourceQuery
}

func TestCustomizeMetrics(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("observability.telemetry.stats.prometheus.customize-metric").
		Features("observability.telemetry.request-classification").
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			t.Cleanup(func() {
				if t.Failed() {
					util.PromDump(t.Clusters().Default(), promInst, prometheus.Query{Metric: "istio_requests_total"})
				}
			})
			httpDestinationQuery := buildQuery(httpProtocol)
			grpcDestinationQuery := buildQuery(grpcProtocol)
			var httpMetricVal string
			cluster := t.Clusters().Default()
			httpChecked := false
			retry.UntilSuccessOrFail(t, func() error {
				if err := sendTraffic(); err != nil {
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
			// By default, envoy histogram has 20 buckets, testdata/bootstrap_patch.yaml change it to 10.
			if err := common.ValidateBucket(cluster, promInst, "client", 10); err != nil {
				t.Errorf("failed to validate bucket: %v", err)
			}
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35915
		Setup(istio.Setup(common.GetIstioInstance(), setupConfig)).
		Setup(testSetup).
		Setup(setupWasmExtension).
		Run()
}

//go:embed testdata/bootstrap_patch.yaml
var bootstrapPatch string

func testSetup(ctx resource.Context) (err error) {
	if err := ctx.ConfigIstio().YAML("istio-system", bootstrapPatch).Apply(apply.Wait); err != nil {
		return err
	}

	registry, err = registryredirector.New(ctx, registryredirector.Config{Cluster: ctx.AllClusters().Default()})
	if err != nil {
		return
	}

	var nsErr error
	appNsInst, nsErr = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if nsErr != nil {
		return nsErr
	}
	proxyMetadata := fmt.Sprintf(`
proxyMetadata:
  BOOTSTRAP_XDS_AGENT: "true"
  WASM_INSECURE_REGISTRIES: %q`, registry.Address())

	echos, err := deployment.New(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(echo.Config{
			Service:   "client",
			Namespace: appNsInst,
			Ports:     nil,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarProxyConfig: {
							Value: proxyMetadata,
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
							Value: proxyMetadata,
						},
					},
				},
			},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8090,
				},
				{
					Name:         "grpc",
					Protocol:     protocol.GRPC,
					WorkloadPort: 7070,
				},
			},
		}).
		Build()
	if err != nil {
		return err
	}
	client = match.ServiceName(echo.NamespacedName{Name: "client", Namespace: appNsInst}).GetMatches(echos)
	server = match.ServiceName(echo.NamespacedName{Name: "server", Namespace: appNsInst}).GetMatches(echos)
	promInst, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return
	}
	return nil
}

//go:embed testdata/setup_config.yaml
var cfgValue string

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = fmt.Sprintf(cfgValue, removedTag)
	cfg.RemoteClusterValues = cfg.ControlPlaneValues
}

func setupWasmExtension(ctx resource.Context) error {
	proxySHA := "359dcd3a19f109c50e97517fe6b1e2676e870c4d"
	attrGenURL := fmt.Sprintf("https://storage.googleapis.com/istio-build/proxy/attributegen-%v.wasm", proxySHA)
	attrGenImageURL := fmt.Sprintf("oci://%v/istio-testing/wasm/attributegen:%v", registry.Address(), proxySHA)
	useRemoteWasmModule := false
	resp, err := http.Get(attrGenURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		useRemoteWasmModule = true
	}

	args := map[string]any{
		"WasmRemoteLoad":  useRemoteWasmModule,
		"AttributeGenURL": attrGenImageURL,
		"DockerConfigJson": base64.StdEncoding.EncodeToString(
			[]byte(createDockerCredential(registryUser, registryPasswd, registry.Address()))),
	}
	if err := ctx.ConfigIstio().EvalFile(appNsInst.Name(), args, "testdata/attributegen_envoy_filter.yaml").
		Apply(); err != nil {
		return err
	}

	return nil
}

func sendHTTPTraffic(urlPath string) error {
	for _, cltInstance := range client {
		httpOpts := echo.CallOptions{
			To: server,
			Port: echo.Port{
				Name: "http",
			},
			HTTP: echo.HTTP{
				Path:   urlPath,
				Method: "GET",
			},
			Retry: echo.Retry{
				NoRetry: true,
			},
		}

		if _, err := cltInstance.Call(httpOpts); err != nil {
			return err
		}
	}

	return nil
}

func sendTraffic() error {
	for _, cltInstance := range client {
		httpOpts := echo.CallOptions{
			To: server,
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
			To: server,
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

func buildQuery(protocol string) (destinationQuery prometheus.Query) {
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

func createDockerCredential(user, passwd, registry string) string {
	credentials := `{
	"auths":{
		"%v":{
			"username": "%v",
			"password": "%v",
			"email": "test@abc.com",
			"auth": "%v"
		}
	}
}`
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + passwd))
	return fmt.Sprintf(credentials, registry, user, passwd, auth)
}
