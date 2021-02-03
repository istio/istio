// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package multiclusterasm

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	stackdriver "istio.io/istio/pkg/test/framework/components/stackdriverasm"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/multicluster"
)

const (
	appCN = "app"
	// metricDuration defines time range backward to check metrics
	metricDuration         = 5 * time.Minute
	containerResourceType  = "k8s_container"
	auditPolicyForLogEntry = "testdata/security/v1beta1-audit-authorization-policy.yaml.tmpl"
)

var (
	ist    istio.Instance
	appCtx multicluster.AppContext
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(multicluster.Setup(&appCtx)).
		Setup(istio.Setup(&ist, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = appCtx.ControlPlaneValues
		})).
		Setup(multicluster.SetupApps(&appCtx)).
		Run()
}

// TestMetrics validates that metrics and logs are collected in stackdriver.
func TestMetrics(t *testing.T) {
	framework.NewTest(t).
		Features("installation.multicluster.multimaster_telemetry").
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("telemetry_asm").
				Run(func(ctx framework.TestContext) {
					clusters := ctx.Environment().Clusters()
					cluster1, cluster2 := clusters[0], clusters[1]
					src := appCtx.LBEchos.GetOrFail(ctx, echo.InCluster(cluster1))
					dest := appCtx.LBEchos.GetOrFail(ctx, echo.InCluster(cluster2))

					const (
						requestCount = 100
					)

					retry.UntilSuccessOrFail(t, func() error {
						r, err := src.Call(echo.CallOptions{
							Count:    requestCount,
							Target:   dest,
							PortName: "grpc",
							Headers: map[string][]string{
								"Host": {dest.Config().FQDN()},
							},
							Message: t.Name(),
						})
						if err != nil {
							return err
						}
						if err := r.CheckOK(); err != nil {
							return err
						}
						return nil
					})
					testStackdriver(t, appCtx.Namespace, src, dest)
				})
		})
}

func validateTelemetry(t *testing.T, projectID string, sd *stackdriver.Instance, ns namespace.Instance, src, dest echo.Instance, filter string) {
	param := &stackdriver.ResourceFilterParam{
		Namespace:     ns.Name(),
		ContainerName: appCN,
		ResourceType:  containerResourceType,
	}

	now := time.Now()
	startTime := now.Add(-metricDuration).Format(time.RFC3339)
	// Add a buffer to endTime for metrics to show up.
	endTime := now.Add(metricDuration).Format(time.RFC3339)

	retry.UntilSuccessOrFail(t, func() error {
		if err := validateMetrics(t, projectID, sd, ns, src, dest, startTime, endTime, param); err != nil {
			t.Logf("failed to validate metrics for %v, resending new traffics to check", err)
			r, err := src.Call(echo.CallOptions{
				Count:    30,
				Target:   dest,
				PortName: "grpc",
				Headers: map[string][]string{
					"Host": {dest.Config().FQDN()},
				},
				Message: t.Name(),
			})
			if err != nil {
				return err
			}
			if err := r.CheckOK(); err != nil {
				return err
			}
			return fmt.Errorf("resending traffic for: %v, check for metrics again", err)
		}

		return nil
	}, retry.Timeout(15*time.Minute))

	validateLog(t, projectID, sd, src, dest, param, startTime, filter)
}

func testStackdriver(t *testing.T, ns namespace.Instance, src, dest echo.Instance) {
	st := stackdriver.NewOrFail(context.Background(), t)

	// Verify metrics and logs on cluster1 in project id 1
	projectID1 := os.Getenv("GCR_PROJECT_ID_1")
	projectID2 := os.Getenv("GCR_PROJECT_ID_2")
	t.Logf("Validating Telemetry for Cluster in project %v", projectID1)
	validateTelemetry(t, projectID1, st, ns, src, dest, "server-accesslog-stackdriver")

	// Verify metrics and logs on cluster1 in project id 2
	t.Logf("Validating Telemetry for for Cluster in project %v", projectID2)
	validateTelemetry(t, projectID2, st, ns, src, dest, "server-accesslog-stackdriver")
}

func testStackdriverAudit(t *testing.T, ns namespace.Instance, src, dest echo.Instance) {
	st := stackdriver.NewOrFail(context.Background(), t)

	// TODO: audit feature supports only single project scenario
	projectID := os.Getenv("GCR_PROJECT_ID_2")
	t.Logf("Validating Telemetry for for Cluster in project %v", projectID)
	validateTelemetry(t, projectID, st, ns, src, dest, "server-istio-audit-log")
}

// TestMetricsAudit validates Audit metrics and logs are collected in stackdriver with filter server-istio-audit-log.
func TestMetricsAudit(t *testing.T) {
	framework.NewTest(t).
		Features("installation.multicluster.multimaster_telemetry").
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("telemetry_asm").
				Run(func(ctx framework.TestContext) {
					clusters := ctx.Environment().Clusters()
					cluster1, cluster2 := clusters[0], clusters[1]
					srcAudit := appCtx.LBEchos.GetOrFail(ctx, echo.InCluster(cluster1))
					destAudit := appCtx.LBEchos.GetOrFail(ctx, echo.InCluster(cluster2))

					ns := appCtx.Namespace.Name()
					args := map[string]string{
						"Namespace": ns,
					}
					policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, auditPolicyForLogEntry))
					ctx.Config().ApplyYAMLOrFail(t, ns, policies...)
					defer ctx.Config().DeleteYAMLOrFail(t, ns, policies...)
					t.Logf("Audit policy deployed to namespace %v", ns)

					const (
						requestCount = 100
					)

					retry.UntilSuccessOrFail(t, func() error {
						r, err := srcAudit.Call(echo.CallOptions{
							Count:    requestCount,
							Target:   destAudit,
							PortName: "http",
							Headers: map[string][]string{
								"Host": {destAudit.Config().FQDN()},
							},
							Message: t.Name(),
						})
						if err != nil {
							return err
						}
						if err := r.CheckOK(); err != nil {
							return err
						}
						return nil
					})
					testStackdriverAudit(t, appCtx.Namespace, srcAudit, destAudit)
				})
		})
}

func validateMetrics(t *testing.T, projectID string, sd *stackdriver.Instance, ns namespace.Instance, src, dest echo.Instance,
	startTime, endTime string, param *stackdriver.ResourceFilterParam) error {
	filter := fmt.Sprintf("%s AND metric.type = %q", param, "istio.io/service/server/request_count")

	expLabel, err := ioutil.ReadFile("testdata/server_request_count.json")
	if err != nil {
		t.Fatalf("failed to read expected label file for asm: %v", err)
	}
	_, err = sd.GetAndValidateTimeSeries(context.Background(), t, []string{filter}, "ALIGN_RATE", startTime, endTime, projectID, expLabel, map[string]interface{}{
		"projectID": projectID,
		"namespace": ns.Name(),
		"meshID":    os.Getenv("MESH_ID"),
		"srcSvc":    src.Config().Service,
		"destSvc":   dest.Config().Service,
	})
	if err != nil {
		t.Errorf("failed to fetch time series for %s container: %v", "test", err)
	}
	return err
}

func validateLog(t *testing.T, projectID string, sd *stackdriver.Instance, src, dest echo.Instance,
	param *stackdriver.ResourceFilterParam, startTime string, filter string) {
	lf := fmt.Sprintf("%s AND timestamp > %q AND logName=\"projects/%s/logs/%s\"", param, startTime, projectID, filter)
	sd.CheckForLogEntry(context.Background(), t, lf, projectID, map[string]string{
		"source_workload":      fmt.Sprintf("%s-v1", src.Config().Service),
		"destination_workload": fmt.Sprintf("%s-v1", dest.Config().Service),
	})
}
