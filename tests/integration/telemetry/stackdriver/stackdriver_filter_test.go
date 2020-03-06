// Copyright 2020 Istio Authors. All Rights Reserved.
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

	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
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
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
)

var (
	ist        istio.Instance
	echoNsInst namespace.Instance
	galInst    galley.Instance
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

func getGalInstance() galley.Instance {
	return galInst
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

// TODO: add test for log, trace and edge.
// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			srvReceived := false
			cltReceived := false
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
				// Check if both client and server side request count metrics are received
				if !srvReceived || !cltReceived {
					return fmt.Errorf("stackdriver server does not received expected server or client request count, server %v client %v", srvReceived, cltReceived)
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite("stackdriver_filter_test", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(getIstioInstance(), setupConfig)).
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
	cfg.Values["telemetry.v2.prometheus.enabled"] = "false"
	cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
}

func testSetup(ctx resource.Context) (err error) {
	galInst, err = galley.New(ctx, galley.Config{})
	if err != nil {
		return
	}
	echoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	sdInst, err = stackdriver.New(ctx)
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

	err = galInst.ApplyConfig(
		echoNsInst,
		sdBootstrap,
	)
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
			Galley:    getGalInstance(),
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
			Galley:    getGalInstance(),
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
