// Copyright Istio Authors
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

package vm

import (
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/pilot/vm"
)

func TestVMTelemetry(t *testing.T) {
	framework.
		NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			// Set up strict mTLS. This gives a bit more assurance the calls are actually going through envoy,
			// and certs are set up correctly.
			ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: send-mtls
spec:
  host: "*.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`)
			ports := []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// Due to a bug in WorkloadEntry, service port must equal target port for now
					InstancePort: 8090,
					ServicePort:  8090,
				},
			}

			// builder to build the instances iteratively
			echoboot.NewBuilderOrFail(t, ctx).
				With(&clt, echo.Config{
					Service:   "client",
					Namespace: ns,
					Ports:     ports,
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
				BuildOrFail(t)

			echoboot.NewBuilderOrFail(t, ctx).
				With(&srv, echo.Config{
					Service:       "server",
					Namespace:     ns,
					Ports:         ports,
					DeployAsVM:    true,
					VMImage:       vm.DefaultVMImage,
					VMEnvironment: vmEnv,
				}).
				BuildOrFail(t)

			srvReceived := false
			cltReceived := false
			logReceived := false
			retry.UntilSuccessOrFail(t, func() error {
				_, err := clt.Call(echo.CallOptions{
					Target:   srv,
					PortName: "http",
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
				for _, tt := range ts {
					// Making resource nil, as test can run on various platforms.
					tt.Resource = nil
					if proto.Equal(tt, &wantSrv) {
						srvReceived = true
					}
					if proto.Equal(tt, &wantClt) {
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

func getWantRequestCountTS() (cltRequestCount, srvRequestCount monitoring.TimeSeries, err error) {
	srvRequestCountTmpl, err := ioutil.ReadFile(serverRequestCount)
	if err != nil {
		return
	}
	sr, err := tmpl.Evaluate(string(srvRequestCountTmpl), map[string]interface{}{
		"EchoNamespace": ns.Name(),
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
		"EchoNamespace": ns.Name(),
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
		"EchoNamespace": ns.Name(),
	})
	if err != nil {
		return
	}
	if err = jsonpb.UnmarshalString(sr, &srvLogEntry); err != nil {
		return
	}
	return
}
