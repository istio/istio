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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	edgespb "istio.io/istio/pkg/test/framework/components/stackdriver/edges"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	stackdriverBootstrapOverride = "testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "testdata/server_request_count.json.tmpl"
	clientRequestCount           = "testdata/client_request_count.json.tmpl"
	serverLogEntry               = "testdata/server_access_log.json.tmpl"
	trafficAssertionTmpl         = "testdata/traffic_assertion.json.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"

	projectsPrefix = "projects/test-project"
)

var (
	ist        istio.Instance
	echoNsInst namespace.Instance
	sdInst     stackdriver.Instance
	srv        echo.Instance
	clt        echo.Instance
)

func getIstioInstance() *istio.Instance {
	return &ist
}

func getEchoNamespaceInstance() namespace.Instance {
	return echoNsInst
}

func unmarshalFromTemplateFile(file string, out proto.Message) error {
	templateFile, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	resource, err := tmpl.Evaluate(string(templateFile), map[string]interface{}{
		"EchoNamespace": getEchoNamespaceInstance().Name(),
	})
	if err != nil {
		return err
	}
	return jsonpb.UnmarshalString(resource, out)
}

// TODO: add test for log, trace and edge.
// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			retry.UntilSuccessOrFail(t, func() error {
				if err := sendTraffic(t); err != nil {
					return fmt.Errorf("could not generate traffic: %v", err)
				}
				if err := validateMetrics(t, serverRequestCount, clientRequestCount); err != nil {
					return err
				}
				if err := validateLogs(t, serverLogEntry); err != nil {
					return err
				}
				if err := validateTraces(t); err != nil {
					return err
				}
				if err := validateEdges(t); err != nil {
					return err
				}
				return nil
			}, retry.Delay(10*time.Second), retry.Timeout(40*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(getIstioInstance(), setupConfig)).
		Setup(testSetup).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  enableTracing: true
values:
  telemetry:
    v2:
      stackdriver:
        configOverride:
          meshEdgesReportingDuration: "5s"
          enable_mesh_edges_reporting: true
`
	// enable stackdriver filter
	cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
	cfg.Values["telemetry.v2.stackdriver.logging"] = "true"
	cfg.Values["telemetry.v2.stackdriver.topology"] = "true"
	cfg.Values["global.proxy.tracer"] = "stackdriver"
	cfg.Values["pilot.traceSampling"] = "100"
	cfg.Values["telemetry.v2.accessLogPolicy.enabled"] = "true"
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
	templateBytes, err := ioutil.ReadFile(stackdriverBootstrapOverride)
	if err != nil {
		return
	}
	sdBootstrap, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"StackdriverNamespace": sdInst.GetStackdriverNamespace(),
		"EchoNamespace":        getEchoNamespaceInstance().Name(),
	})
	if err != nil {
		return
	}

	err = ctx.Config().ApplyYAML(echoNsInst.Name(), sdBootstrap)
	if err != nil {
		return
	}
	err = echoboot.NewBuilder(ctx).
		With(&clt, echo.Config{
			Service:   "clt",
			Namespace: getEchoNamespaceInstance(),
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarBootstrapOverride: {
							Value: sdBootstrapConfigMap,
						},
					},
				},
			}}).
		With(&srv, echo.Config{
			Service:   "srv",
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
			}}).
		Build()
	if err != nil {
		return
	}
	return nil
}

// send both a grpc and http requests (http with forced tracing).
func sendTraffic(t *testing.T) error {
	t.Helper()
	grpcOpts := echo.CallOptions{
		Target:   srv,
		PortName: "grpc",
		Count:    1,
	}
	if _, err := clt.Call(grpcOpts); err != nil {
		return err
	}

	// an HTTP request with forced tracing
	hdr := http.Header{}
	httpOpts := echo.CallOptions{
		Target:   srv,
		PortName: "http",
		Headers:  hdr,
		Count:    1,
	}
	_, err := clt.Call(httpOpts)
	return err
}

func validateMetrics(t *testing.T, serverReqCount, clientReqCount string) error {
	t.Helper()

	var wantClient, wantServer monitoring.TimeSeries
	if err := unmarshalFromTemplateFile(serverReqCount, &wantServer); err != nil {
		return fmt.Errorf("metrics: error generating wanted server request: %v", err)
	}
	if err := unmarshalFromTemplateFile(clientReqCount, &wantClient); err != nil {
		return fmt.Errorf("metrics: error generating wanted client request: %v", err)
	}

	// Traverse all time series received and compare with expected client and server time series.
	ts, err := sdInst.ListTimeSeries()
	if err != nil {
		return fmt.Errorf("metrics: error getting time-series from Stackdriver: %v", err)
	}

	t.Logf("number of timeseries: %v", len(ts))
	var gotServer, gotClient bool
	for _, tt := range ts {
		if proto.Equal(tt, &wantServer) {
			gotServer = true
		}
		if proto.Equal(tt, &wantClient) {
			gotClient = true
		}
	}
	if !(gotServer && gotClient) {
		return fmt.Errorf("metrics: did not get expected metrics; server = %t, client = %t", gotServer, gotClient)
	}
	return nil
}

func validateLogs(t *testing.T, srvLogEntry string) error {
	t.Helper()

	var wantLog loggingpb.LogEntry
	if err := unmarshalFromTemplateFile(srvLogEntry, &wantLog); err != nil {
		return fmt.Errorf("logs: failed to parse wanted log entry: %v", err)
	}
	// Traverse all log entries received and compare with expected server log entry.
	entries, err := sdInst.ListLogEntries()
	if err != nil {
		return fmt.Errorf("logs: failed to get received log entries: %v", err)
	}
	for _, l := range entries {
		if proto.Equal(l, &wantLog) {
			return nil
		}
	}

	return errors.New("logs: did not get expected log entry")
}

func validateEdges(t *testing.T) error {
	t.Helper()

	var wantEdge edgespb.TrafficAssertion
	if err := unmarshalFromTemplateFile(trafficAssertionTmpl, &wantEdge); err != nil {
		return fmt.Errorf("edges: failed to build wanted traffic assertion: %v", err)
	}
	edges, err := sdInst.ListTrafficAssertions()
	if err != nil {
		return fmt.Errorf("edges: failed to get traffic assertions from Stackdriver: %v", err)
	}
	for _, edge := range edges {
		edge.Destination.Uid = ""
		edge.Destination.ClusterName = ""
		edge.Destination.Location = ""
		edge.Source.Uid = ""
		edge.Source.ClusterName = ""
		edge.Source.Location = ""
		t.Logf("edge: %v", edge)
		if proto.Equal(edge, &wantEdge) {
			return nil
		}
	}
	return errors.New("edges: did not get expected traffic assertion")
}

func validateTraces(t *testing.T) error {
	t.Helper()

	// we are looking for a trace that looks something like:
	//
	// project_id:"projects/test-project"
	// trace_id:"99bc9a02417c12c4877e19a4172ae11a"
	// spans:{
	//   span_id:440543054939690778
	//   name:"projects/test-project/traces/99bc9a02417c12c4877e19a4172ae11a/spans/061d1f9309f2171a"
	//   start_time:{seconds:1594418699  nanos:648039133}
	//   end_time:{seconds:1594418699  nanos:669864006}
	//   parent_span_id:18050098903530484457
	//   labels:{
	//     key:"span"
	//     value:"srv.istio-echo-1-92573.svc.cluster.local:80/*"
	//   }
	// }
	//
	// we only need to validate the span value in the labels and project_id for
	// the purposes of this test at the moment.
	//
	// future improvements include adding canonical service info, etc. in the
	// span.

	wantSpanLabel := fmt.Sprintf("srv.%s.svc.cluster.local:80/*", getEchoNamespaceInstance().Name())
	traces, err := sdInst.ListTraces()
	if err != nil {
		return fmt.Errorf("traces: could not retrieve traces from Stackdriver: %v", err)
	}
	for _, trace := range traces {
		t.Logf("trace: %v\n", trace)
		if trace.ProjectId != projectsPrefix {
			continue
		}
		for _, span := range trace.Spans {
			if !strings.HasPrefix(span.Name, projectsPrefix) {
				continue
			}
			if got, ok := span.Labels["span"]; ok && got == wantSpanLabel {
				return nil
			}
		}
	}
	return errors.New("traces: could not find expected trace")
}
