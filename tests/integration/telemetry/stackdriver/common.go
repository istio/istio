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

//go:build integ
// +build integ

package stackdriver

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"cloud.google.com/go/compute/metadata"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/gcemetadata"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	stackdriverBootstrapOverride = "tests/integration/telemetry/stackdriver/testdata/custom_bootstrap.yaml.tmpl"
	serverRequestCount           = "tests/integration/telemetry/stackdriver/testdata/server_request_count.json.tmpl"
	clientRequestCount           = "tests/integration/telemetry/stackdriver/testdata/client_request_count.json.tmpl"
	serverLogEntry               = "tests/integration/telemetry/stackdriver/testdata/server_access_log.json.tmpl"
	sdBootstrapConfigMap         = "stackdriver-bootstrap-config"
)

var (
	Ist        istio.Instance
	EchoNsInst namespace.Instance
	GCEInst    gcemetadata.Instance
	SDInst     stackdriver.Instance
	Srv        echo.Instances
	Clt        echo.Instances
)

func TestSetup(ctx resource.Context) (err error) {
	EchoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	SDInst, err = stackdriver.New(ctx, stackdriver.Config{})
	if err != nil {
		return
	}
	templateBytes, err := os.ReadFile(filepath.Join(env.IstioSrc, stackdriverBootstrapOverride))
	if err != nil {
		return
	}
	sdBootstrap, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"StackdriverAddress": SDInst.Address(),
		"EchoNamespace":      EchoNsInst.Name(),
		"UseRealSD":          stackdriver.UseRealStackdriver(),
	})
	if err != nil {
		return
	}

	err = ctx.Config().ApplyYAML(EchoNsInst.Name(), sdBootstrap)
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
				Namespace: EchoNsInst,
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
				Namespace: EchoNsInst,
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
	Clt = echos.Match(echo.ServicePrefix("clt"))
	Srv = echos.Match(echo.Service("srv"))
	return nil
}

// send both a grpc and http requests (http with forced tracing).
func SendTraffic(t *testing.T, cltInstance echo.Instance, headers http.Header) error {
	t.Helper()
	//  All server instance have same names, so setting target as srv[0].
	// Sending the number of total request same as number of servers, so that load balancing gets a chance to send request to all the clusters.
	grpcOpts := echo.CallOptions{
		Target:   Srv[0],
		PortName: "grpc",
		Count:    telemetry.RequestCountMultipler * len(Srv),
	}
	// an HTTP request with forced tracing
	httpOpts := echo.CallOptions{
		Target:   Srv[0],
		PortName: "http",
		Headers:  headers,
		Count:    telemetry.RequestCountMultipler * len(Srv),
	}
	if _, err := cltInstance.Call(grpcOpts); err != nil {
		return err
	}
	if _, err := cltInstance.Call(httpOpts); err != nil {
		return err
	}
	return nil
}

func ValidateMetrics(t *testing.T, serverReqCount, clientReqCount, clName, trustDomain string) error {
	t.Helper()

	var wantClient, wantServer monitoring.TimeSeries
	if err := unmarshalFromTemplateFile(serverReqCount, &wantServer, clName, trustDomain); err != nil {
		return fmt.Errorf("metrics: error generating wanted server request: %v", err)
	}
	if err := unmarshalFromTemplateFile(clientReqCount, &wantClient, clName, trustDomain); err != nil {
		return fmt.Errorf("metrics: error generating wanted client request: %v", err)
	}

	// Traverse all time series received and compare with expected client and server time series.
	ts, err := SDInst.ListTimeSeries(EchoNsInst.Name())
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

func unmarshalFromTemplateFile(file string, out proto.Message, clName, trustDomain string) error {
	templateFile, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	resource, err := tmpl.Evaluate(string(templateFile), map[string]interface{}{
		"EchoNamespace": EchoNsInst.Name(),
		"ClusterName":   clName,
		"TrustDomain":   trustDomain,
		"OnGCE":         metadata.OnGCE(),
	})
	if err != nil {
		return err
	}
	return protomarshal.Unmarshal([]byte(resource), out)
}

func ConditionallySetupMetadataServer(ctx resource.Context) (err error) {
	if !metadata.OnGCE() {
		if GCEInst, err = gcemetadata.New(ctx, gcemetadata.Config{}); err != nil {
			return
		}
	}
	return nil
}

func ValidateLogs(t *testing.T, srvLogEntry, clName, trustDomain string, filter stackdriver.LogType) error {
	t.Helper()
	var wantLog loggingpb.LogEntry
	if err := unmarshalFromTemplateFile(srvLogEntry, &wantLog, clName, trustDomain); err != nil {
		return fmt.Errorf("logs: failed to parse wanted log entry: %v", err)
	}

	// Traverse all log entries received and compare with expected server log entry.
	entries, err := SDInst.ListLogEntries(filter, EchoNsInst.Name())
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
