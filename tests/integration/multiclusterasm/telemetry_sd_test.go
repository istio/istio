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
	"strings"
	"testing"
	"time"

	"google.golang.org/api/option"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"

	// Side-effect import to register cmd line flags
	_ "istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	stackdriver "istio.io/istio/pkg/test/framework/components/stackdriverasm"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/pilot/common"
)

const (
	appCN = "app"
	// metricDuration defines time range backward to check metrics
	metricDuration         = 15 * time.Minute
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
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			projectID1 := os.Getenv("GCR_PROJECT_ID_1")
			st := stackdriver.NewOrFail(context.Background(), ctx, option.WithQuotaProject(projectID1))
			testMulticluster(ctx, func(ctx framework.TestContext, ns namespace.Instance, src, dest echo.Instance) {
				// Verify metrics and logs on cluster1 in project id 1
				ctx.Logf("Validating Telemetry for Cluster in project %v", projectID1)
				validateTelemetry(ctx, projectID1, st, ns, src, dest, "grpc", "server-accesslog-stackdriver")

				// Verify metrics and logs on cluster2 in project id 2
				// TODO metrics only seem to be in project1 until traffic is sent from both projects
				// projectID2 := os.Getenv("GCR_PROJECT_ID_2")
				// ctx.Logf("Validating Telemetry for for Cluster in project %v", projectID2)
				// validateTelemetry(ctx, projectID2, st, ns, src, dest, "grpc", "server-accesslog-stackdriver")
			})
		})
}

func TestAuditStackdriver(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			ctx.Skip("https://buganizer.corp.google.com/issues/184872790")
			projectID, projectID2 := os.Getenv("GCR_PROJECT_ID_1"), os.Getenv("GCR_PROJECT_ID_2")

			// TODO: audit feature supports only single project scenario
			if projectID != projectID2 {
				t.Skipf("audit feature only supports single-project, got: %s %s", projectID, projectID2)
			}

			st := stackdriver.NewOrFail(context.Background(), ctx, option.WithQuotaProject(projectID))
			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(ctx, args, file.AsStringOrFail(ctx, auditPolicyForLogEntry))
			ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), policies...)
			testMulticluster(ctx, func(ctx framework.TestContext, ns namespace.Instance, src, dest echo.Instance) {
				ctx.Logf("Validating Audit Telemetry for for Cluster in project %v", projectID)
				validateTelemetry(ctx, projectID, st, ns, src, dest, "http", "server-istio-audit-log")
			})
		})
}

func testMulticluster(ctx framework.TestContext, doTest func(ctx framework.TestContext, ns namespace.Instance, src, dest echo.Instance)) {
	for _, src := range apps {
		for _, dst := range apps {
			src, dst := src, dst
			ctx.
				NewSubTest(fmt.Sprintf("from %s to %s", src.Config().Cluster.StableName(), dst.Config().Cluster.StableName())).
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
	portName string,
	filter string,
) {
	param := &stackdriver.ResourceFilterParam{
		Namespace:     ns.Name(),
		ContainerName: appCN,
		ResourceType:  stackdriver.ContainerResourceType,
	}
	// TODO be mopre dynamic than checking only dest res type.. for now tests only query server/request_count
	if dest.Config().DeployAsVM {
		param.ResourceType = stackdriver.VMResourceType
	}

	now := time.Now()
	startTime := now.Format(time.RFC3339)
	// Add a buffer to endTime for metrics to show up.
	endTime := now.Add(metricDuration).Format(time.RFC3339)

	retry.UntilSuccessOrFail(ctx, func() error {
		if err := sendTraffic(ctx, src, dest, portName); err != nil {
			return err
		}
		if err := validateMetrics(ctx, portName, projectID, sd, ns, src, dest, startTime, endTime, param); err != nil {
			ctx.Logf("failed to validate metrics for %v, resending new traffics to check", err)
			return fmt.Errorf("resending traffic for: %v, check for metrics again", err)
		}
		return nil
	}, retry.Timeout(30*time.Minute))

	validateLog(ctx, projectID, sd, src, dest, param, startTime, filter)
}

func sendTraffic(t framework.TestContext, src, dest echo.Instance, portName string) error {
	r, err := src.Call(echo.CallOptions{
		Count:    requestCount,
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

func validateMetrics(t framework.TestContext, portName string, projectID string, sd *stackdriver.Instance, ns namespace.Instance, src, dest echo.Instance,
	startTime, endTime string, param *stackdriver.ResourceFilterParam) error {
	metricParam := *param
	metricParam.FilterFor = "metric"
	filter := fmt.Sprintf("%s AND metric.type = %q", metricParam, "istio.io/service/server/request_count")

	expLabel, err := ioutil.ReadFile("testdata/server_request_count.json")
	if err != nil {
		t.Fatalf("failed to read expected label file for asm: %v", err)
	}
	echoPort := dest.Config().PortByName(portName)
	if echoPort == nil {
		t.Fatal("%s does not have a port %s", dest.Config().Service, portName)
		return nil
	}

	// TODO We don't have an easy way to get the full owner string for VMs, so we just verify the prefix.
	k8sOwnerFmt := "kubernetes://apis/apps/v1/namespaces/%s/deployments/%s-v1"
	srcOwner := fmt.Sprintf(k8sOwnerFmt, src.Config().Namespace.Name(), src.Config().Service)
	destOwner := fmt.Sprintf(k8sOwnerFmt, dest.Config().Namespace.Name(), dest.Config().Service)
	if src.Config().DeployAsVM {
		srcOwner = stackdriver.VMOwnerPrefix
	}
	if dest.Config().DeployAsVM {
		destOwner = stackdriver.VMOwnerPrefix
	}

	_, err = sd.GetAndValidateTimeSeries(context.Background(), t, []string{filter}, "ALIGN_RATE", startTime, endTime, projectID, expLabel, map[string]interface{}{
		"projectID":        projectID,
		"namespace":        ns.Name(),
		"meshID":           os.Getenv("MESH_ID"),
		"srcSvc":           src.Config().Service,
		"destSvc":          dest.Config().Service,
		"protocol":         strings.ToLower(string(echoPort.Protocol)),
		"port":             echoPort.InstancePort,
		"sourceOwner":      srcOwner,
		"destinationOwner": destOwner,
	})
	if err != nil {
		t.Errorf("failed to fetch time series for %s container: %v", "test", err)
	}
	return err
}

func validateLog(ctx framework.TestContext, projectID string, sd *stackdriver.Instance, src, dest echo.Instance,
	param *stackdriver.ResourceFilterParam, startTime string, filter string) {
	logParam := *param
	logParam.FilterFor = "log"
	lf := fmt.Sprintf("%s AND timestamp > %q AND logName=\"projects/%s/logs/%s\"", logParam, startTime, projectID, filter)
	sd.CheckForLogEntry(context.Background(), ctx, lf, projectID, map[string]string{
		"source_workload":      fmt.Sprintf("%s-v1", src.Config().Service),
		"destination_workload": fmt.Sprintf("%s-v1", dest.Config().Service),
	})
}
