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
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

// const (
// 	stackdriverBootstrapOverride = "testdata/authz_logging/custom_bootstrap.yaml.tmpl"
// 	serverRequestCount           = "testdata/authz_logging/server_request_count.json.tmpl"
// 	clientRequestCount           = "testdata/authz_logging/client_request_count.json.tmpl"
// 	serverLogEntry               = "testdata/authz_logging/server_access_log.json.tmpl"
// 	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
// )

// var (
// 	ist        istio.Instance
// 	echoNsInst namespace.Instance
// 	sdInst     stackdriver.Instance
// 	srv        echo.Instance
// 	clt        echo.Instance
// )

var (
	cltRequestCountFile = "testdata/authz_policy/clt_request_count.json.tmpl"
	srvRequestCountFile = "testdata/authz_policy/srv_request_count.json.tmpl"
	srvAccessLogFile    = "testdata/authz_policy/srv_access_log.json.tmpl"
	p                   pilot.Instance
)

func getWantRequestCountTSNew(cltFile, srvFile, ns, sourceName, destName, expectCode, port,
	protocol string) (srvRequestCount monitoring.TimeSeries, cltRequestCount monitoring.TimeSeries, err error) {

	args := map[string]interface{}{
		"Namespace":  ns,
		"DestName":   destName,
		"SourceName": sourceName,
		"Protocol":   protocol,
		"Port":       port,
		"Code":       expectCode,
	}

	// Get client want TimeSeries
	cltRequestCountTmpl, err := ioutil.ReadFile(cltFile)
	if err != nil {
		return
	}

	cltRC, err := tmpl.Evaluate(string(cltRequestCountTmpl), args)

	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(cltRC, &cltRequestCount); err != nil {
		return
	}

	// Get server want TimeSeries
	srvRequestCountTmpl, err := ioutil.ReadFile(srvFile)
	if err != nil {
		return
	}

	srvRC, err := tmpl.Evaluate(string(srvRequestCountTmpl), args)

	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(srvRC, &srvRequestCount); err != nil {
		return
	}

	return
}

func getWantServerLogEntryNew(srvFile, ns, sourceName, destName, expectCode, port,
	protocol string) (srvLogEntry loggingpb.LogEntry, err error) {

	scheme := protocol
	if expectCode == "403" {
		scheme = ""
	}

	args := map[string]interface{}{
		"Namespace":  ns,
		"DestName":   destName,
		"SourceName": sourceName,
		"Protocol":   protocol,
		"Port":       port,
		"Code":       expectCode,
		"Scheme":     scheme,
	}

	// Get server want LogEntry
	srvLogEntryTmpl, err := ioutil.ReadFile(srvFile)
	if err != nil {
		return
	}

	srvLE, err := tmpl.Evaluate(string(srvLogEntryTmpl), args)

	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(srvLE, &srvLogEntry); err != nil {
		return
	}

	return
}

type testCase struct {
	Namespace       string
	From            echo.Instance
	Target          echo.Instance
	Protocol        string
	Port            string
	ExpectedLog     bool
	ExpectedAllowed bool
}

func (tc testCase) checkTestCase() error {

	resp, err := tc.From.Call(echo.CallOptions{
		Target:   tc.Target,
		PortName: tc.Protocol,
		Scheme:   scheme.Instance(tc.Protocol),
	})

	if err != nil {
		return err
	}

	// Check request is properly allowed/denied
	if tc.ExpectedAllowed {
		if err = resp.CheckOK(); err != nil {
			return fmt.Errorf("expected  %s but got %v", response.StatusCodeOK, err)
		}
	} else {
		if resp[0].Code != response.StatusCodeForbidden {
			return fmt.Errorf("expected %s but got %s", response.StatusCodeForbidden, resp[0].Code)
		}
	}

	expStatusCode := response.StatusCodeForbidden
	if tc.ExpectedAllowed {
		expStatusCode = response.StatusCodeOK
	}

	//Check Logs
	wantClt, wantSrv, err := getWantRequestCountTSNew(cltRequestCountFile, srvRequestCountFile, tc.Namespace, tc.From.Config().Service,
		tc.Target.Config().Service, expStatusCode, tc.Port, tc.Protocol)

	if err != nil {
		return err
	}

	// Verify log entry
	wantLog, err := getWantServerLogEntryNew(srvAccessLogFile, tc.Namespace, tc.From.Config().Service, tc.Target.Config().Service,
		expStatusCode, tc.Port, tc.Protocol)

	// fmt.Printf("%v", wantLog)

	if err != nil {
		return fmt.Errorf("failed to parse wanted log entry: %v", err)
	}

	srvReceived := false
	cltReceived := false
	logReceived := false

	// Traverse all time series received and compare with expected client and server time series.
	if tc.ExpectedLog {
		tss, err := sdInst.ListTimeSeries()

		if err != nil {
			return err
		}
		for _, ts := range tss {
			if proto.Equal(ts, &wantSrv) {
				// fmt.Printf("%v", ts)
				// t.Logf("%+v\n", ts)
				srvReceived = true
			}
			if proto.Equal(ts, &wantClt) {
				cltReceived = true
			}
		}

		// Traverse all log entries received and compare with expected server log entry.
		entries, err := sdInst.ListLogEntries()

		if err != nil {
			return fmt.Errorf("failed to get received log entries: %v", err)
		}
		for _, l := range entries {
			// fmt.Printf("%v\n", l)
			if proto.Equal(l, &wantLog) {
				logReceived = true
			}
			// else {
			// 	fmt.Printf("WANT: %v\nGOT: %v\n", wantLog, l)
			// }
		}
	}

	if !srvReceived || !cltReceived {
		return fmt.Errorf("stackdriver server does not received expected server or client request count, server %v client %v", srvReceived, cltReceived)
		// return fmt.Errorf("xx: %v, %v, %v", wantClt, srvReceived, cltReceived)
	}

	if !logReceived {
		return fmt.Errorf("stackdriver server does not received expected log entry")
	}

	return nil
}

// func getWantLogEntry(filename string, ns namespace.Instance) (logEntry loggingpb.LogEntry, err error) {
// 	logEntryTmpl, err := ioutil.ReadFile(filename)
// 	if err != nil {
// 		return
// 	}
// 	sr, err := tmpl.Evaluate(string(logEntryTmpl), map[string]interface{}{
// 		"EchoNamespace": getEchoNamespaceInstance().Name(),
// 	})
// 	if err != nil {
// 		return
// 	}
// 	if err = jsonpb.UnmarshalString(sr, &logEntry); err != nil {
// 		return
// 	}
// 	return
// }

// TODO: add test for log, trace and edge.
// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestAuthzStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := getEchoNamespaceInstance()

			p := pilot.NewOrFail(t, ctx, pilot.Config{})

			ports := []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
				{
					Name:         "tcp",
					Protocol:     protocol.TCP,
					InstancePort: 8092,
				},
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}

			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/authz_policy/authz_policy.yaml.tmpl"))
			ctx.ApplyConfigOrFail(t, ns.Name(), policies...)
			defer ctx.DeleteConfigOrFail(t, ns.Name(), policies...)

			echoConfig := func(ns namespace.Instance, name string, ports []echo.Port) echo.Config {
				return echo.Config{
					Namespace: ns,
					Subsets: []echo.SubsetConfig{
						{
							Annotations: map[echo.Annotation]*echo.AnnotationValue{
								echo.SidecarBootstrapOverride: {
									Value: sdBootstrapConfigMap,
								},
							},
						},
					},
					Pilot:          p,
					Service:        name,
					Ports:          ports,
					ServiceAccount: true,
				}
			}

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, echoConfig(ns, "a", ports)).
				With(&b, echoConfig(ns, "b", ports)).
				With(&c, echoConfig(ns, "c", ports)).BuildOrFail(t)

			testCases := []testCase{
				{ns.Name(), a, c, "http", "8090", true, true},
				{ns.Name(), b, c, "http", "8090", true, false},
			}
			for _, tc := range testCases {
				t.Run("authz-test", func(t *testing.T) {
					retry.UntilSuccessOrFail(t,
						tc.checkTestCase,
						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}

			// srvReceived := false
			// cltReceived := false
			// logReceived := false

			// retry.UntilSuccessOrFail(t, func() error {
			// 	_, err := clt.Call(echo.CallOptions{
			// 		Target:   srv,
			// 		PortName: "grpc",
			// 		Count:    1,
			// 	})
			// 	if err != nil {
			// 		return err
			// 	}
			// 	// Verify stackdriver metrics
			// 	wantClt, wantSrv, err := getWantRequestCountTSt(cltRequestCountFile, srvRequestCountFile, ns.Name(), "clt", "srv", "200", "7070", "grpc")
			// 	// t.Logf("tHE want: %v\n", wantSrv)
			// 	if err != nil {
			// 		return err
			// 	}
			// 	// Traverse all time series received and compare with expected client and server time series.
			// 	tss, err := sdInst.ListTimeSeries()
			// 	if err != nil {
			// 		return err
			// 	}
			// 	for _, ts := range tss {
			// 		if proto.Equal(ts, &wantSrv) {
			// 			// t.Logf("%+v\n", ts)
			// 			srvReceived = true
			// 		}
			// 		if proto.Equal(ts, &wantClt) {
			// 			cltReceived = true
			// 		}
			// 	}

			// 	// Verify log entry
			// 	wantLog, err := getWantServerLogEntry()
			// 	if err != nil {
			// 		return fmt.Errorf("failed to parse wanted log entry: %v", err)
			// 	}
			// 	// Traverse all log entries received and compare with expected server log entry.
			// 	entries, err := sdInst.ListLogEntries()
			// 	if err != nil {
			// 		return fmt.Errorf("failed to get received log entries: %v", err)
			// 	}
			// 	for _, l := range entries {
			// 		if proto.Equal(l, &wantLog) {
			// 			logReceived = true
			// 		}
			// 	}

			// 	// Check if both client and server side request count metrics are received
			// 	if !srvReceived || !cltReceived {
			// 		return fmt.Errorf("stackdriver server does not received expected server or client request count, server %v client %v", srvReceived, cltReceived)
			// 	}
			// 	if !logReceived {
			// 		return fmt.Errorf("stackdriver server does not received expected log entry")
			// 	}
			// 	return nil
			// }, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
		})
}
