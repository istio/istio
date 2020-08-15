// // Copyright Istio Authors. All Rights Reserved.
// //
// // Licensed under the Apache License, Version 2.0 (the "License");
// // you may not use this file except in compliance with the License.
// // You may obtain a copy of the License at
// //
// //     http://www.apache.org/licenses/LICENSE-2.0
// //
// // Unless required by applicable law or agreed to in writing, software
// // distributed under the License is distributed on an "AS IS" BASIS,
// // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// // See the License for the specific language governing permissions and
// // limitations under the License.

package stackdriver

// import (
// 	"fmt"
// 	"strings"
// 	"testing"
// 	"time"

// 	"istio.io/istio/pkg/config/protocol"
// 	"istio.io/istio/pkg/test/echo/common/response"
// 	"istio.io/istio/pkg/test/echo/common/scheme"
// 	"istio.io/istio/pkg/test/framework"
// 	"istio.io/istio/pkg/test/framework/components/echo"
// 	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
// 	"istio.io/istio/pkg/test/framework/components/namespace"

// 	// "istio.io/istio/pkg/test/framework/components/pilot"
// 	"istio.io/istio/pkg/test/util/file"
// 	"istio.io/istio/pkg/test/util/retry"
// 	"istio.io/istio/pkg/test/util/tmpl"
// 	"istio.io/istio/pkg/util/protomarshal"
// )

// // const (
// // 	stackdriverBootstrapOverride = "testdata/authz_logging/custom_bootstrap.yaml.tmpl"
// // 	serverRequestCount           = "testdata/authz_logging/server_request_count.json.tmpl"
// // 	clientRequestCount           = "testdata/authz_logging/client_request_count.json.tmpl"
// // 	serverLogEntry               = "testdata/authz_logging/server_access_log.json.tmpl"
// // 	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
// // )

// // var (
// // 	ist        istio.Instance
// // 	echoNsInst namespace.Instance
// // 	sdInst     stackdriver.Instance
// // 	srv        echo.Instance
// // 	clt        echo.Instance
// // )

// var (
// 	cltRequestCountFile = "testdata/authz_policy/clt_request_count.json.tmpl"
// 	srvRequestCountFile = "testdata/authz_policy/srv_request_count.json.tmpl"
// 	srvAccessLogFile    = "testdata/authz_policy/srv_access_log.json.tmpl"
// 	// p                   pilot.Instance
// )

// // func getWantServerLogEntryNew(srvFile, ns, sourceName, destName, expectCode, port,
// // 	protocol string) (srvLogEntry loggingpb.LogEntry, err error) {

// // 	scheme := protocol
// // 	if expectCode == "403" {
// // 		scheme = ""
// // 	}

// // 	args := map[string]interface{}{
// // 		"Namespace":  ns,
// // 		"DestName":   destName,
// // 		"SourceName": sourceName,
// // 		"Protocol":   protocol,
// // 		"Port":       port,
// // 		"Code":       expectCode,
// // 		"Scheme":     scheme,
// // 	}

// // 	// Get server want LogEntry
// // 	srvLogEntryTmpl, err := ioutil.ReadFile(srvFile)
// // 	if err != nil {
// // 		return
// // 	}

// // 	srvLE, err := tmpl.Evaluate(string(srvLogEntryTmpl), args)

// // 	if err != nil {
// // 		return
// // 	}
// // 	if err = jsonpb.UnmarshalString(srvLE, &srvLogEntry); err != nil {
// // 		return
// // 	}

// // 	return
// // }

// type testCase struct {
// 	Namespace       string
// 	From            echo.Instance
// 	Target          echo.Instance
// 	Protocol        string
// 	Port            string
// 	ExpectedLog     bool
// 	ExpectedAllowed bool
// }

// // Makes call from source to target instance, and checks the call is properly allowed/denied
// func (tc testCase) checkCall() error {
// 	resp, err := tc.From.Call(echo.CallOptions{
// 		Target:   tc.Target,
// 		PortName: tc.Protocol,
// 		Scheme:   scheme.Instance(tc.Protocol),
// 	})

// 	if err != nil {
// 		return fmt.Errorf("error in during echo call: %s", err)
// 	}

// 	// Check request is properly allowed/denied
// 	if tc.ExpectedAllowed {
// 		if err = resp.CheckOK(); err != nil {
// 			return fmt.Errorf("expected  %s but got %v", response.StatusCodeOK, err)
// 		}
// 	} else {
// 		if resp[0].Code != response.StatusCodeForbidden {
// 			return fmt.Errorf("expected %s but got %s", response.StatusCodeForbidden, resp[0].Code)
// 		}
// 	}

// 	return nil
// }

// // Checks stackdriver receives the expected entries and does not receive any unexpected entries
// func (tc testCase) checkReceiveEntries() error {
// 	expStatusCode := response.StatusCodeForbidden
// 	if tc.ExpectedAllowed {
// 		expStatusCode = response.StatusCodeOK
// 	}

// 	// Verify log entry
// 	// wantLog, err := getWantServerLogEntryNew(srvAccessLogFile, tc.Namespace, tc.From.Config().Service, tc.Target.Config().Service,
// 	// 	expStatusCode, tc.Port, tc.Protocol)

// 	// if err != nil {
// 	// 	return fmt.Errorf("failed to parse wanted log entry: %v", err)
// 	// }

// 	logReceived := false
// 	checkStrs := []string{tc.Namespace + "/sa/" + tc.From.Config().Service, tc.Namespace + "/sa/" + tc.Target.Config().Service, expStatusCode}

// 	// Traverse all log entries received and compare with expected server log entry.
// 	entries, err := sdInst.ListLogEntries()
// 	if err != nil {
// 		return fmt.Errorf("failed to get received log entries: %v", err)
// 	}

// 	fmt.Printf("=========================TRY======================\n")
// 	for _, l := range entries {
// 		fmt.Printf("%v\n", l)
// 		// if proto.Equal(l, &wantLog) {
// 		// 	logReceived = true
// 		// }
// 		var gotYaml string
// 		if gotYaml, err = protomarshal.ToYAML(l); err != nil {
// 			return fmt.Errorf("failed to parse yaml: %s", err)
// 		}

// 		foundAll := true
// 		for _, str := range checkStrs {
// 			if !strings.Contains(gotYaml, str) {
// 				foundAll = false
// 			}
// 		}

// 		if foundAll {
// 			logReceived = true
// 			break
// 		}
// 	}

// 	if tc.ExpectedLog && !logReceived {
// 		return fmt.Errorf("stackdriver server does not receive expected log entry")
// 	} else if !tc.ExpectedLog && logReceived {
// 		return fmt.Errorf("stackdriver server received unexpected log entry")
// 	}

// 	return nil
// }

// // TODO: add test for log, trace and edge.
// // TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
// func TestAuthzStackdriverMonitoring(t *testing.T) {
// 	framework.NewTest(t).
// 		Run(func(ctx framework.TestContext) {

// 			ns := getEchoNamespaceInstance()

// 			// p := pilot.NewOrFail(t, ctx, pilot.Config{})

// 			ports := []echo.Port{
// 				{
// 					Name:         "http",
// 					Protocol:     protocol.HTTP,
// 					InstancePort: 8090,
// 				},
// 				{
// 					Name:         "tcp",
// 					Protocol:     protocol.TCP,
// 					InstancePort: 8092,
// 				},
// 			}

// 			args := map[string]string{
// 				"Namespace": ns.Name(),
// 			}

// 			policies := tmpl.EvaluateAllOrFail(t, args,
// 				file.AsStringOrFail(t, "testdata/authz_policy/authz_policy.yaml.tmpl"))
// 			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
// 			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policies...)

// 			echoConfig := func(ns namespace.Instance, name string, ports []echo.Port) echo.Config {
// 				return echo.Config{
// 					Namespace: ns,
// 					Subsets: []echo.SubsetConfig{
// 						{
// 							Annotations: map[echo.Annotation]*echo.AnnotationValue{
// 								echo.SidecarBootstrapOverride: {
// 									Value: sdBootstrapConfigMap,
// 								},
// 							},
// 						},
// 					},
// 					// Pilot:          p,
// 					Service:        name,
// 					Ports:          ports,
// 					ServiceAccount: true,
// 				}
// 			}

// 			var a, b, c echo.Instance
// 			echoboot.NewBuilder(ctx).
// 				With(&a, echoConfig(ns, "a", ports)).
// 				With(&b, echoConfig(ns, "b", ports)).
// 				With(&c, echoConfig(ns, "c", ports)).BuildOrFail(t)

// 			testCases := []testCase{
// 				{ns.Name(), a, c, "http", "8090", true, false},
// 				{ns.Name(), b, c, "http", "8090", true, true},
// 			}
// 			for _, tc := range testCases {
// 				t.Run("authz-test", func(t *testing.T) {
// 					// Separate call from stackdriver check to avoid excessive calls
// 					retry.UntilSuccessOrFail(t,
// 						tc.checkCall,
// 						retry.Delay(250*time.Millisecond), retry.Timeout(10*time.Second))

// 					retry.UntilSuccessOrFail(t,
// 						tc.checkReceiveEntries,
// 						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
// 				})
// 			}
// 		})
// }
