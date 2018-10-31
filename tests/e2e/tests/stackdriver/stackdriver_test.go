// Copyright 2018 Istio Authors
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

package stackdriver

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/periodic"
	"golang.org/x/oauth2/google"
	cloudtrace "google.golang.org/api/cloudtrace/v1"
	"google.golang.org/api/iterator"
	monitoring "google.golang.org/api/monitoring/v3"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	fortioYaml  = "tests/e2e/tests/stackdriver/fortio-rules.yaml"
	adapterYaml = "tests/e2e/tests/stackdriver/adapter.yaml"
)

type (
	fortioTemplate struct {
		FortioImage string
	}
	adapterTemplate struct {
		Namespace string
	}
	testConfig struct {
		*framework.CommonConfig
	}
)

var (
	tc      *testConfig
	gcpProj *string
)

func (t *testConfig) Setup() error {
	if !util.CheckPodsRunning(t.Kube.Namespace, t.Kube.KubeConfig) {
		return fmt.Errorf("could not get all pods running")
	}

	filled, err := util.CreateAndFill(
		t.Kube.TmpDir,
		util.GetResourcePath(adapterYaml),
		&adapterTemplate{
			Namespace: t.Kube.Namespace,
		},
	)
	if err != nil {
		return err
	}

	if err := util.KubeApply(t.Kube.Namespace, filled, t.Kube.KubeConfig); err != nil {
		return fmt.Errorf("setup failed: kubectl apply -f %s: %v", adapterYaml, err)
	}

	gateway, errGw := tc.Kube.IngressGateway()
	if errGw != nil {
		return errGw
	}
	if _, err := sendTrafficToCluster(gateway); err != nil {
		return fmt.Errorf("generating HTTP traffic failed: %v", err)
	}

	// Push interval is 5s, wait for metrics to be written
	time.Sleep(10 * time.Second)

	return nil
}

func sendTrafficToCluster(gateway string) (*fhttp.HTTPRunnerResults, error) {
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        10,
			Exactly:    60,
			NumThreads: 5,
			Out:        os.Stderr,
		},
		HTTPOptions: fhttp.HTTPOptions{
			URL: gateway + "/?status=418:10,520:15&size=1024:10,512:5",
		},
		AllowInitialErrors: true,
	}
	return fhttp.RunHTTPTest(&opts)
}

func (t *testConfig) Teardown() error {
	return nil
}

func TestMain(m *testing.M) {
	gcpProj = flag.String("gcp_proj", os.Getenv("GCP_PROJ"), "GCP Project ID, required")
	flag.Parse()
	check(framework.InitLogging(), "cannot setup logging")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}

func TestContextGraph(t *testing.T) {
	// FIXME add test when read API is available.
}

func createMetricsService(ctx context.Context) (*monitoring.Service, error) {
	hc, err := google.DefaultClient(ctx, monitoring.MonitoringReadScope)
	if err != nil {
		return nil, err
	}
	s, err := monitoring.New(hc)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func TestMetrics(t *testing.T) {
	const (
		metricType     = "istio.io/service/server/request_count"
		filterTemplate = `metric.type="%s" AND
                      resource.labels.namespace_name = "%s" AND
                      resource.type = "%s"`
	)
	projectResource := "projects/" + *gcpProj

	ctx := context.Background()
	s, err := createMetricsService(ctx)
	if err != nil {
		t.Fatal(err)
	}

	f := fmt.Sprintf(filterTemplate, metricType, tc.Kube.Namespace, "k8s_container")
	t.Logf("Filter: %v", f)
	startTime := time.Now().UTC().Add(time.Minute * -10)
	endTime := time.Now().UTC()
	resp, err := s.Projects.TimeSeries.List(projectResource).
		Filter(f).
		IntervalStartTime(startTime.Format(time.RFC3339Nano)).
		IntervalEndTime(endTime.Format(time.RFC3339Nano)).
		Do()
	if err != nil {
		t.Fatalf("Could not read time series value, %v ", err)
	}

	if len(resp.TimeSeries) == 0 {
		t.Fatalf("No TimeSeries found")
	}
	t.Logf("Got %v timeseries", len(resp.TimeSeries))

	totalPoints := 0
	for i, v := range resp.TimeSeries {
		t.Logf("Timeseries %v has %v points", i, len(v.Points))
		totalPoints += len(v.Points)
	}
	if totalPoints == 0 {
		t.Fatalf("No Points found in TimeSeries")
	}
}

func TestLogs(t *testing.T) {
	const (
		logNameTmpl = "projects/%s/logs/server-accesslog-stackdriver.logentry.%s"
		filterTmpl  = `logName = "%s" AND resource.type = "k8s_container" AND resource.labels.namespace_name = "%s"`
	)

	ctx := context.Background()
	adminClient, err := logadmin.NewClient(ctx, *gcpProj)
	if err != nil {
		t.Fatalf("Failed to create logadmin client: %v", err)
	}

	logName := fmt.Sprintf(logNameTmpl, *gcpProj, tc.Kube.Namespace)
	filter := fmt.Sprintf(filterTmpl, logName, tc.Kube.Namespace)
	iter := adminClient.Entries(ctx, logadmin.Filter(filter), logadmin.NewestFirst())
	entry, err := iter.Next()
	if err == iterator.Done {
		t.Fatalf("No logs found: %v", err)
	}
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}
	t.Logf("Got a log, dest: %v", entry.Labels["destination_service_host"])
}

func createTraceService(ctx context.Context) (*cloudtrace.Service, error) {
	hc, err := google.DefaultClient(ctx, cloudtrace.TraceReadonlyScope)
	if err != nil {
		return nil, err
	}
	s, err := cloudtrace.New(hc)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func TestTrace(t *testing.T) {
	ctx := context.Background()
	s, err := createTraceService(ctx)
	if err != nil {
		t.Fatal(err)
	}

	f := fmt.Sprintf("+source_workload_namespace:%s", tc.Kube.Namespace)
	t.Logf("Trace filter: %v", f)
	startTime := time.Now().UTC().Add(time.Minute * -10)
	endTime := time.Now().UTC()

	resp, err := s.Projects.Traces.List(*gcpProj).
		Filter(f).
		StartTime(startTime.Format(time.RFC3339Nano)).
		EndTime(endTime.Format(time.RFC3339Nano)).
		PageSize(10).
		Do()
	if err != nil {
		t.Fatalf("Could not query traces, %v ", err)
	}

	if len(resp.Traces) == 0 {
		t.Fatal("No Traces found")
	}
	t.Logf("Got %v traces", len(resp.Traces))
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("stackdriver_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	hub := os.Getenv("FORTIO_HUB")
	tag := os.Getenv("FORTIO_TAG")
	image := hub + "/fortio:" + tag
	if hub == "" || tag == "" {
		image = "fortio/fortio:latest" // TODO: change
	}
	log.Infof("Fortio hub %s tag %s -> image %s", hub, tag, image)
	services := []framework.App{
		{
			KubeInject:      true,
			AppYamlTemplate: util.GetResourcePath(fortioYaml),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
	}
	for i := range services {
		tc.Kube.AppManager.AddApp(&services[i])
	}
	return nil
}

func check(err error, msg string) {
	if err != nil {
		log.Errorf("%s. Error %s", msg, err)
		os.Exit(-1)
	}
}
