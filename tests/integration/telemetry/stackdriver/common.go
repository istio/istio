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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"cloud.google.com/go/compute/metadata"
	"google.golang.org/genproto/googleapis/devtools/cloudtrace/v1"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
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

	FakeGCEMetadataServerValues = `
  defaultConfig:
    proxyMetadata:
      GCE_METADATA_HOST: `
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

	err = ctx.ConfigKube().ApplyYAML(EchoNsInst.Name(), sdBootstrap)
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
func SendTraffic(cltInstance echo.Instance, headers http.Header, onlyTCP bool) error {
	//  All server instance have same names, so setting target as srv[0].
	// Sending the number of total request same as number of servers, so that load balancing gets a chance to send request to all the clusters.
	if onlyTCP {
		_, err := cltInstance.Call(echo.CallOptions{
			Target:   Srv[0],
			PortName: "tcp",
			Count:    telemetry.RequestCountMultipler * len(Srv),
		})
		return err
	}
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
	// TODO: this looks at the machine the node is running on. This would not work if the host and test
	// cluster differ.
	if !metadata.OnGCE() {
		if GCEInst, err = gcemetadata.New(ctx, gcemetadata.Config{}); err != nil {
			return
		}
	}
	return nil
}

func ValidateLogs(t test.Failer, srvLogEntry, clName, trustDomain string, filter stackdriver.LogType) error {
	var wantLog loggingpb.LogEntry
	if err := unmarshalFromTemplateFile(srvLogEntry, &wantLog, clName, trustDomain); err != nil {
		return fmt.Errorf("logs: failed to parse wanted log entry: %v", err)
	}
	return ValidateLogEntry(t, &wantLog, filter)
}

func ValidateLogEntry(t test.Failer, want *loggingpb.LogEntry, filter stackdriver.LogType) error {
	// Traverse all log entries received and compare with expected server log entry.
	entries, err := SDInst.ListLogEntries(filter, EchoNsInst.Name())
	if err != nil {
		return fmt.Errorf("logs: failed to get received log entries: %v", err)
	}

	for _, l := range entries {
		l.Trace = ""
		l.SpanId = ""
		if proto.Equal(l, want) {
			return nil
		}
	}
	LogAccessLogsDiff(t, want, entries)
	return fmt.Errorf("logs: did not get expected log entry")
}

func LogAccessLogsDiff(t test.Failer, wantRaw *loggingpb.LogEntry, entries []*loggingpb.LogEntry) {
	query := normalizeLogs(wantRaw)
	existing := []map[string]string{}
	for _, e := range entries {
		existing = append(existing, normalizeLogs(e))
	}
	logDiff(t, query, existing)
}

func LogTraceDiff(t test.Failer, wantRaw *cloudtrace.Trace, entries []*cloudtrace.Trace) {
	query := normalizeTrace(wantRaw)
	existing := []map[string]string{}
	for _, e := range entries {
		existing = append(existing, normalizeTrace(e))
	}
	logDiff(t, query, existing)
}

func LogMetricsDiff(t test.Failer, wantRaw *monitoring.TimeSeries, entries []*monitoring.TimeSeries) {
	query := normalizeMetrics(wantRaw)
	existing := []map[string]string{}
	for _, e := range entries {
		existing = append(existing, normalizeMetrics(e))
	}
	logDiff(t, query, existing)
}

func logDiff(t test.Failer, query map[string]string, entries []map[string]string) {
	if len(entries) == 0 {
		t.Logf("no entries found")
		return
	}
	allMismatches := []map[string]string{}
	full := []map[string]string{}
	seen := sets.NewSet()
	for _, s := range entries {
		b, _ := json.Marshal(s)
		ss := string(b)
		if seen.Contains(ss) {
			continue
		}
		seen.Insert(ss)
		misMatched := map[string]string{}
		for k, want := range query {
			got := s[k]
			if want != got {
				misMatched[k] = got
			}
		}
		if len(misMatched) == 0 {
			continue
		}
		allMismatches = append(allMismatches, misMatched)
		full = append(full, s)
	}
	if len(allMismatches) == 0 {
		t.Log("no diff found")
		return
	}
	t.Logf("query returned %d entries (%d distinct), but none matched our query exactly.", len(entries), len(seen))
	for i, m := range allMismatches {
		t.Logf("Entry %d)", i)
		missing := []string{}
		for k, v := range m {
			if v == "" {
				missing = append(missing, k)
			} else {
				t.Logf("  for label %q, wanted %q but got %q", k, query[k], v)
			}
		}
		if len(missing) > 0 {
			t.Logf("  missing labels: %v", missing)
		}
	}
}

func normalizeLogs(l *loggingpb.LogEntry) map[string]string {
	r := map[string]string{}
	if l.HttpRequest != nil {
		r["http.RequestMethod"] = l.HttpRequest.RequestMethod
		r["http.RequestUrl"] = l.HttpRequest.RequestUrl
		r["http.RequestSize"] = fmt.Sprint(l.HttpRequest.RequestSize)
		r["http.Status"] = fmt.Sprint(l.HttpRequest.Status)
		r["http.ResponseSize"] = fmt.Sprint(l.HttpRequest.ResponseSize)
		r["http.UserAgent"] = l.HttpRequest.UserAgent
		r["http.RemoteIp"] = l.HttpRequest.RemoteIp
		r["http.ServerIp"] = l.HttpRequest.ServerIp
		r["http.Referer"] = l.HttpRequest.Referer
		r["http.Latency"] = fmt.Sprint(l.HttpRequest.Latency)
		r["http.CacheLookup"] = fmt.Sprint(l.HttpRequest.CacheLookup)
		r["http.CacheHit"] = fmt.Sprint(l.HttpRequest.CacheHit)
		r["http.CacheValidatedWithOriginServer"] = fmt.Sprint(l.HttpRequest.CacheValidatedWithOriginServer)
		r["http.CacheFillBytes"] = fmt.Sprint(l.HttpRequest.CacheFillBytes)
		r["http.Protocol"] = l.HttpRequest.Protocol
	}
	for k, v := range l.Labels {
		r["labels."+k] = v
	}
	r["traceSampled"] = fmt.Sprint(l.TraceSampled)
	return r
}

func normalizeMetrics(l *monitoring.TimeSeries) map[string]string {
	r := map[string]string{}
	for k, v := range l.Metric.Labels {
		r["metric.labels."+k] = v
	}
	r["metric.type"] = l.Metric.Type
	return r
}

func normalizeTrace(l *cloudtrace.Trace) map[string]string {
	r := map[string]string{}
	r["projectId"] = l.ProjectId
	for i, s := range l.Spans {
		for k, v := range s.Labels {
			r[fmt.Sprintf("span[%d-%s].%s", i, s.Name, k)] = v
		}
	}
	return r
}
