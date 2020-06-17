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
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

const (
	stackdriverBootstrapOverride = "testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "testdata/server_request_count.json.tmpl"
	clientRequestCount           = "testdata/client_request_count.json.tmpl"
	serverLogEntry               = "testdata/server_access_log.json.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
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

func getWantRequestCountTS() (cltRequestCount, srvRequestCount monitoring.TimeSeries, err error) {
	srvRequestCountTmpl, err := ioutil.ReadFile(serverRequestCount)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": getEchoNamespaceInstance().Name(),
	})
	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(sr, &srvRequestCount); err != nil {
		return
	}
	cltRequestCountTmpl, err := ioutil.ReadFile(clientRequestCount)
	if err != nil {
		return
	}
	cr, err := tmpl.Evaluate(string(cltRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": getEchoNamespaceInstance().Name(),
	})
	if err != nil {
		return
	}
	err = jsonpb.UnmarshalString(cr, &cltRequestCount)
	return
}

func getWantServerLogEntry() (srvLogEntry loggingpb.LogEntry, err error) {
	srvlogEntryTmpl, err := ioutil.ReadFile(serverLogEntry)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvlogEntryTmpl), map[string]interface{}{
		"EchoNamespace": getEchoNamespaceInstance().Name(),
	})
	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(sr, &srvLogEntry); err != nil {
		return
	}
	return
}

// TODO: add test for log, trace and edge.
// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			srvReceived := false
			cltReceived := false
			logReceived := false
			retry.UntilSuccessOrFail(t, func() error {
				_, err := clt.Call(echo.CallOptions{
					Target:   srv,
					PortName: "grpc",
					Count:    1,
				})
				if err != nil {
					return err
				}
				// Verify stackdriver metrics
				wantClt, wantSrv, err := getWantRequestCountTS()
				if err != nil {
					return err
				}
				// Traverse all time series received and compare with expected client and server time series.
				ts, err := sdInst.ListTimeSeries()
				if err != nil {
					return err
				}
				for _, t := range ts {
					if proto.Equal(t, &wantSrv) {
						srvReceived = true
					}
					if proto.Equal(t, &wantClt) {
						cltReceived = true
					}
				}

				// Verify log entry
				wantLog, err := getWantServerLogEntry()
				if err != nil {
					return fmt.Errorf("failed to parse wanted log entry: %v", err)
				}
				// Traverse all log entries received and compare with expected server log entry.
				entries, err := sdInst.ListLogEntries()
				if err != nil {
					return fmt.Errorf("failed to get received log entries: %v", err)
				}
				for _, l := range entries {
					if proto.Equal(l, &wantLog) {
						logReceived = true
					}
				}

				// Check if both client and server side request count metrics are received
				if !srvReceived || !cltReceived {
					return fmt.Errorf("stackdriver server does not received expected server or client request count, server %v client %v", srvReceived, cltReceived)
				}
				if !logReceived {
					return fmt.Errorf("stackdriver server does not received expected log entry")
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite("stackdriver_filter_test", m).
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
	// disable mixer telemetry and enable stackdriver filter
	cfg.Values["telemetry.enabled"] = "true"
	cfg.Values["telemetry.v1.enabled"] = "false"
	cfg.Values["telemetry.v2.enabled"] = "true"
	cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
	cfg.Values["telemetry.v2.stackdriver.logging"] = "true"
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

	err = ctx.ApplyConfig(echoNsInst.Name(), sdBootstrap)
	if err != nil {
		return
	}
	builder, err := echoboot.NewBuilder(ctx)
	if err != nil {
		return
	}
	err = builder.
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
