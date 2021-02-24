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
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/pilot/common"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	// Side-effect import to register cmd line flags
	_ "istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	stackdriver "istio.io/istio/pkg/test/framework/components/stackdriverasm"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	appCN = "app"
	// metricDuration defines time range backward to check metrics
	metricDuration         = 5 * time.Minute
	containerResourceType  = "k8s_container"
	auditPolicyForLogEntry = "testdata/security/v1beta1-audit-authorization-policy.yaml.tmpl"
	requestCount           = 100
)

var (
	apps echo.Instances
	ns   namespace.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		RequireMaxClusters(2).
		Setup(setupApps).
		Run()
}

func setupApps(ctx resource.Context) error {
	var err error
	ns, err = namespace.New(ctx, namespace.Config{Prefix: "echo-stackdriver", Inject: true})
	if err != nil {
		return err
	}
	apps, err = echoboot.NewBuilder(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(echo.Config{
			Namespace: ns,
			Service:   "app",
			Ports:     common.EchoPorts,
		}).
		WithConfig(echo.Config{
			Namespace:  ns,
			Service:    "vm",
			Ports:      common.EchoPorts,
			DeployAsVM: true,
		}).
		Build()
	return err
}

func TestStackdriver(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		testMulticluster(ctx, func(ctx framework.TestContext, ns namespace.Instance, src, dest echo.Instance) {
			st := stackdriver.NewOrFail(context.Background(), ctx)
			projectID1 := os.Getenv("GCR_PROJECT_ID_1")
			projectID2 := os.Getenv("GCR_PROJECT_ID_2")

			// Verify metrics and logs on cluster1 in project id 1
			ctx.Logf("Validating Telemetry for Cluster in project %v", projectID1)
			validateTelemetry(ctx, projectID1, st, ns, src, dest, "grpc", "server-accesslog-stackdriver")

			// Verify metrics and logs on cluster1 in project id 2
			ctx.Logf("Validating Telemetry for for Cluster in project %v", projectID2)
			validateTelemetry(ctx, projectID2, st, ns, src, dest, "http", "server-accesslog-stackdriver")
		})
	})
}

func TestStackdriverAudit(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		testMulticluster(ctx, func(ctx framework.TestContext, ns namespace.Instance, src, dest echo.Instance) {
			st := stackdriver.NewOrFail(context.Background(), ctx)

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(ctx, args, file.AsStringOrFail(ctx, auditPolicyForLogEntry))
			ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), policies...)

			// TODO: audit feature supports only single project scenario
			projectID := os.Getenv("GCR_PROJECT_ID_2")
			ctx.Logf("Validating Telemetry for for Cluster in project %v", projectID)
			validateTelemetry(ctx, projectID, st, ns, src, dest, "http", "server-istio-audit-log")
		})
	})
}

func testMulticluster(ctx framework.TestContext, doTest func(ctx framework.TestContext, ns namespace.Instance, src, dest echo.Instance)) {
	for _, src := range apps {
		for _, dst := range apps {
			src, dst := src, dst
			ctx.
				NewSubTest(fmt.Sprintf("from %s to %s", src.Config().Cluster.Name(), dst.Config().Cluster.Name())).
				Run(func(ctx framework.TestContext) {
					doTest(ctx, ns, src, dst)
				})
		}
	}
}

func validateTelemetry(ctx framework.TestContext,
	projectID string,
	sd *stackdriver.Instance, ns namespace.Instance,
	src, dest echo.Instance,
	protocolPort string,
	filter string,
) {
	param := &stackdriver.ResourceFilterParam{
		Namespace:     ns.Name(),
		ContainerName: appCN,
		ResourceType:  containerResourceType,
	}

	now := time.Now()
	startTime := now.Add(-metricDuration).Format(time.RFC3339)
	// Add a buffer to endTime for metrics to show up.
	endTime := now.Add(metricDuration).Format(time.RFC3339)

	// Get 1 successful traffic call first
	retry.UntilSuccessOrFail(ctx, func() error {
		return sendTraffic(ctx, src, dest, protocolPort)
	})
	// Then validate telemetry
	retry.UntilSuccessOrFail(ctx, func() error {
		if err := sendTraffic(ctx, src, dest, protocolPort); err != nil {
			return err
		}
		if err := validateMetrics(ctx, projectID, sd, ns, src, dest, startTime, endTime, param); err != nil {
			ctx.Logf("failed to validate metrics for %v, resending new traffics to check", err)
			return fmt.Errorf("resending traffic for: %v, check for metrics again", err)
		}
		return nil
	}, retry.Timeout(15*time.Minute))

	validateLog(ctx, projectID, sd, src, dest, param, startTime, filter)
}

func sendTraffic(t framework.TestContext, src, dest echo.Instance, portName string) error {
	r, err := src.Call(echo.CallOptions{
		Count:    30,
		Target:   dest,
		PortName: portName,
		Headers: map[string][]string{
			"Host": {dest.Config().FQDN()},
		},
		Message: t.Name(),
	})
	if err != nil {
		return err
	}
	return r.CheckOK()
}

func validateMetrics(t framework.TestContext, projectID string, sd *stackdriver.Instance, ns namespace.Instance, src, dest echo.Instance,
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

func validateLog(ctx framework.TestContext, projectID string, sd *stackdriver.Instance, src, dest echo.Instance,
	param *stackdriver.ResourceFilterParam, startTime string, filter string) {
	lf := fmt.Sprintf("%s AND timestamp > %q AND logName=\"projects/%s/logs/%s\"", param, startTime, projectID, filter)
	sd.CheckForLogEntry(context.Background(), ctx, lf, projectID, map[string]string{
		"source_workload":      fmt.Sprintf("%s-v1", src.Config().Service),
		"destination_workload": fmt.Sprintf("%s-v1", dest.Config().Service),
	})
}
