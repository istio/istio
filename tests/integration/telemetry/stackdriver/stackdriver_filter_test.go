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

package stackdriver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cloud.google.com/go/compute/metadata"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"golang.org/x/sync/errgroup"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/gcemetadata"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	stackdriverBootstrapOverride = "tests/integration/telemetry/stackdriver/testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "tests/integration/telemetry/stackdriver/testdata/server_request_count.json.tmpl"
	clientRequestCount           = "tests/integration/telemetry/stackdriver/testdata/client_request_count.json.tmpl"
	serverLogEntry               = "tests/integration/telemetry/stackdriver/testdata/server_access_log.json.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"

	fakeGCEMetadataServerValues = `
  defaultConfig:
    proxyMetadata:
      GCE_METADATA_HOST: `
)

var (
	ist        istio.Instance
	echoNsInst namespace.Instance
	gceInst    gcemetadata.Instance
	sdInst     stackdriver.Instance
	srv        echo.Instances
	clt        echo.Instances
)

func getIstioInstance() *istio.Instance {
	return &ist
}

func getEchoNamespaceInstance() namespace.Instance {
	return echoNsInst
}

func unmarshalFromTemplateFile(file string, out proto.Message, clName, trustDomain string) error {
	templateFile, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	resource, err := tmpl.Evaluate(string(templateFile), map[string]interface{}{
		"EchoNamespace": getEchoNamespaceInstance().Name(),
		"ClusterName":   clName,
		"TrustDomain":   trustDomain,
		"OnGCE":         metadata.OnGCE(),
	})
	if err != nil {
		return err
	}
	return jsonpb.UnmarshalString(resource, out)
}

// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := sendTraffic(t, cltInstance, http.Header{}); err != nil {
							return err
						}
						clName := cltInstance.Config().Cluster.Name()
						trustDomain := telemetry.GetTrustDomain(cltInstance.Config().Cluster, ist.Settings().SystemNamespace)
						scopes.Framework.Infof("Validating for cluster %s", clName)

						// Validate cluster names in telemetry below once https://github.com/istio/istio/issues/28125 is fixed.
						if err := validateMetrics(t, filepath.Join(env.IstioSrc, serverRequestCount), filepath.Join(env.IstioSrc, clientRequestCount),
							clName, trustDomain); err != nil {
							return err
						}
						t.Logf("Metrics validated")
						if err := validateLogs(t, filepath.Join(env.IstioSrc, serverLogEntry), clName, trustDomain, stackdriver.ServerAccessLog); err != nil {
							return err
						}
						t.Logf("logs validated")
						if err := validateTraces(t); err != nil {
							return err
						}
						t.Logf("Traces validated")

						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(conditionallySetupMetadataServer).
		Setup(istio.Setup(getIstioInstance(), setupConfig)).
		Setup(testSetup).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  enableTracing: true
`
	// enable stackdriver filter
	cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
	cfg.Values["telemetry.v2.stackdriver.logging"] = "true"
	cfg.Values["telemetry.v2.stackdriver.topology"] = "true"
	cfg.Values["telemetry.v2.stackdriver.configOverride.enable_audit_log"] = "true"
	cfg.Values["global.proxy.tracer"] = "stackdriver"
	cfg.Values["pilot.traceSampling"] = "100"
	cfg.Values["telemetry.v2.accessLogPolicy.enabled"] = "true"
	cfg.Values["telemetry.v2.accessLogPolicy.logWindowDuration"] = "1s"
	cfg.Values["global.proxy.componentLogLevel"] = "rbac:debug,wasm:debug"

	// conditionally use a fake metadata server for testing off of GCP
	if gceInst != nil {
		cfg.ControlPlaneValues = strings.Join([]string{cfg.ControlPlaneValues, fakeGCEMetadataServerValues, gceInst.Address()}, "")
	}
}

func conditionallySetupMetadataServer(ctx resource.Context) (err error) {
	if !metadata.OnGCE() {
		if gceInst, err = gcemetadata.New(ctx, gcemetadata.Config{}); err != nil {
			return
		}
	}
	return nil
}

func testSetup(ctx resource.Context) (err error) {
	echoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	sdInst, err = stackdriver.New(ctx, stackdriver.Config{})
	if err != nil {
		return
	}
	templateBytes, err := os.ReadFile(filepath.Join(env.IstioSrc, stackdriverBootstrapOverride))
	if err != nil {
		return
	}
	sdBootstrap, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"StackdriverAddress": sdInst.Address(),
		"EchoNamespace":      getEchoNamespaceInstance().Name(),
		"UseRealSD":          stackdriver.UseRealStackdriver(),
	})
	if err != nil {
		return
	}

	err = ctx.Config().ApplyYAML(echoNsInst.Name(), sdBootstrap)
	if err != nil {
		return
	}

	builder := echoboot.NewBuilder(ctx)
	for _, cls := range ctx.Clusters() {
		clName := cls.Name()
		builder.
			WithConfig(echo.Config{
				Service:   fmt.Sprintf("clt-%s", clName),
				Cluster:   cls,
				Namespace: getEchoNamespaceInstance(),
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarBootstrapOverride: {
								Value: sdBootstrapConfigMap,
							},
						},
					},
				},
			}).
			WithConfig(echo.Config{
				Service:   "srv",
				Cluster:   cls,
				Namespace: getEchoNamespaceInstance(),
				Ports: []echo.Port{
					{
						Name:     "grpc",
						Protocol: protocol.GRPC,
						// We use a port > 1024 to not require root
						InstancePort: 7070,
					},
					{
						Name:     "http",
						Protocol: protocol.HTTP,
						// We use a port > 1024 to not require root
						InstancePort: 8888,
					},
					{
						Name:     "tcp",
						Protocol: protocol.TCP,
						// We use a port > 1024 to not require root
						InstancePort: 9000,
					},
				},
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[echo.Annotation]*echo.AnnotationValue{
							echo.SidecarBootstrapOverride: {
								Value: sdBootstrapConfigMap,
							},
						},
					},
				},
			})
	}
	echos, err := builder.Build()
	if err != nil {
		return
	}
	clt = echos.Match(echo.ServicePrefix("clt"))
	srv = echos.Match(echo.Service("srv"))
	return nil
}

// send both a grpc and http requests (http with forced tracing).
func sendTraffic(t *testing.T, cltInstance echo.Instance, headers http.Header) error {
	t.Helper()
	//  All server instance have same names, so setting target as srv[0].
	// Sending the number of total request same as number of servers, so that load balancing gets a chance to send request to all the clusters.
	grpcOpts := echo.CallOptions{
		Target:   srv[0],
		PortName: "grpc",
		Count:    telemetry.RequestCountMultipler * len(srv),
	}
	// an HTTP request with forced tracing
	httpOpts := echo.CallOptions{
		Target:   srv[0],
		PortName: "http",
		Headers:  headers,
		Count:    telemetry.RequestCountMultipler * len(srv),
	}
	if _, err := cltInstance.Call(grpcOpts); err != nil {
		return err
	}
	if _, err := cltInstance.Call(httpOpts); err != nil {
		return err
	}
	return nil
}

func validateMetrics(t *testing.T, serverReqCount, clientReqCount, clName, trustDomain string) error {
	t.Helper()

	var wantClient, wantServer monitoring.TimeSeries
	if err := unmarshalFromTemplateFile(serverReqCount, &wantServer, clName, trustDomain); err != nil {
		return fmt.Errorf("metrics: error generating wanted server request: %v", err)
	}
	if err := unmarshalFromTemplateFile(clientReqCount, &wantClient, clName, trustDomain); err != nil {
		return fmt.Errorf("metrics: error generating wanted client request: %v", err)
	}

	// Traverse all time series received and compare with expected client and server time series.
	ts, err := sdInst.ListTimeSeries(getEchoNamespaceInstance().Name())
	if err != nil {
		return fmt.Errorf("metrics: error getting time-series from Stackdriver: %v", err)
	}

	t.Logf("number of timeseries: %v", len(ts))
	var gotServer, gotClient bool
	for _, tt := range ts {
		if tt == nil {
			continue
		}
		if tt.Metric.Type != wantClient.Metric.Type && tt.Metric.Type != wantServer.Metric.Type {
			continue
		}
		// TODO temp hack to get the change that actually sets these fields merged
		if labels := tt.GetMetric().GetLabels(); labels != nil {
			delete(labels, "api_version")
			delete(labels, "api_name")
		}
		if proto.Equal(tt, &wantServer) {
			gotServer = true
		}
		if proto.Equal(tt, &wantClient) {
			gotClient = true
		}
	}
	if !gotServer || !gotClient {
		return fmt.Errorf("metrics: did not get expected metrics for cluster %s; got %v\n want client %v\n want server %v",
			clName, ts, wantClient.String(), wantServer.String())
	}
	return nil
}

func validateLogs(t *testing.T, srvLogEntry, clName, trustDomain string, filter stackdriver.LogType) error {
	t.Helper()
	var wantLog loggingpb.LogEntry
	if err := unmarshalFromTemplateFile(srvLogEntry, &wantLog, clName, trustDomain); err != nil {
		return fmt.Errorf("logs: failed to parse wanted log entry: %v", err)
	}

	// Traverse all log entries received and compare with expected server log entry.
	entries, err := sdInst.ListLogEntries(filter, getEchoNamespaceInstance().Name())
	if err != nil {
		return fmt.Errorf("logs: failed to get received log entries: %v", err)
	}

	for _, l := range entries {
		if proto.Equal(l, &wantLog) {
			return nil
		}
	}
	return fmt.Errorf("logs: did not get expected log entry: got %v\n want %v", entries, wantLog.String())
}

func validateTraces(t *testing.T) error {
	t.Helper()

	// we are looking for a trace that looks something like:
	//
	// project_id:"test-project"
	// trace_id:"99bc9a02417c12c4877e19a4172ae11a"
	// spans:{
	//   span_id:440543054939690778
	//   name:"srv.istio-echo-1-92573.svc.cluster.local:80/*"
	//   start_time:{seconds:1594418699  nanos:648039133}
	//   end_time:{seconds:1594418699  nanos:669864006}
	//   parent_span_id:18050098903530484457
	// }
	//
	// we only need to validate the span value in the labels and project_id for
	// the purposes of this test at the moment.
	//
	// future improvements include adding canonical service info, etc. in the
	// span.

	wantSpanName := fmt.Sprintf("srv.%s.svc.cluster.local:80/*", getEchoNamespaceInstance().Name())
	traces, err := sdInst.ListTraces(getEchoNamespaceInstance().Name())
	if err != nil {
		return fmt.Errorf("traces: could not retrieve traces from Stackdriver: %v", err)
	}
	for _, trace := range traces {
		t.Logf("trace: %v\n", trace)
		for _, span := range trace.Spans {
			if span.Name == wantSpanName {
				return nil
			}
		}
	}
	return errors.New("traces: could not find expected trace")
}
