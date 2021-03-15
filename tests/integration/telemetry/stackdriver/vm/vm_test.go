// +build integ
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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/genproto/googleapis/devtools/cloudtrace/v1"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoring "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	edgespb "istio.io/istio/pkg/test/framework/components/stackdriver/edges"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

func TestVMTelemetry(t *testing.T) {
	framework.
		NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			// Set up strict mTLS. This gives a bit more assurance the calls are actually going through envoy,
			// and certs are set up correctly.
			ctx.Config().ApplyYAMLOrFail(ctx, ns.Name(), enforceMTLS)

			clientBuilder.BuildOrFail(t)
			serverBuilder.BuildOrFail(t)

			retry.UntilSuccessOrFail(t, func() error {
				// send single request from client -> server
				if _, err := client.Call(echo.CallOptions{Target: server, PortName: "http", Count: 1}); err != nil {
					return err
				}

				// Verify stackdriver metrics
				gotMetrics := gotRequestCountMetrics(wantClientReqs, wantServerReqs)

				// Verify log entry
				gotLogs := gotLogEntry(wantLogEntry)

				// Verify edges
				gotEdges := gotTrafficAssertion(wantTrafficAssertion)

				// verify traces
				gotTraces := gotTrace(wantTrace)

				if !(gotMetrics && gotLogs && gotEdges && gotTraces) {
					return fmt.Errorf("did not receive all expected telemetry; status: metrics=%t, logs=%t, edges=%t, traces=%t", gotMetrics, gotLogs, gotEdges, gotTraces)
				}

				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
		})
}

func traceEqual(got, want *cloudtrace.Trace) bool {
	if len(got.Spans) != len(want.Spans) {
		log.Infof("incorrect number of spans: got %d, want: %d", len(got.Spans), len(want.Spans))
		return false
	}
	if got.ProjectId != want.ProjectId {
		log.Errorf("mismatched project ids: got %q, want %q", got.ProjectId, want.ProjectId)
		return false
	}

	for _, wantSpan := range want.Spans {
		foundSpan := false
		for _, gotSpan := range got.Spans {
			delete(gotSpan.Labels, "guid:x-request-id")
			delete(gotSpan.Labels, "node_id")
			delete(gotSpan.Labels, "peer.address")
			delete(gotSpan.Labels, "zone")
			delete(gotSpan.Labels, "g.co/agent")    // ignore OpenCensus lib versions
			delete(gotSpan.Labels, "response_size") // this could be slightly off, just ignore
			if foundSpan = reflect.DeepEqual(gotSpan.Labels, wantSpan.Labels); foundSpan {
				break
			}
		}
		if !foundSpan {
			log.Errorf("missing span from trace: got %v\nwant %v", got, want)
			return false
		}
	}

	return true
}

func gotRequestCountMetrics(wantClient, wantServer *monitoring.TimeSeries) bool {
	ts, err := sdInst.ListTimeSeries()
	if err != nil {
		log.Errorf("could not get list of time-series from stackdriver: %v", err)
		return false
	}

	var gotServer, gotClient bool
	for _, series := range ts {
		// Making resource nil, as test can run on various platforms.
		series.Resource = nil
		if proto.Equal(series, wantServer) {
			gotServer = true
		}
		if proto.Equal(series, wantClient) {
			gotClient = true
		}
	}

	if !gotServer {
		log.Errorf("incorrect metric: got %v\n want client %v\n", ts, wantServer)
	}
	if !gotClient {
		log.Errorf("incorrect metric: got %v\n want client %v\n", ts, wantClient)
	}
	return gotServer && gotClient
}

func gotLogEntry(want *loggingpb.LogEntry) bool {
	entries, err := sdInst.ListLogEntries(stackdriver.ServerAccessLog)
	if err != nil {
		log.Errorf("failed to get list of log entries from stackdriver: %v", err)
		return false
	}
	for _, l := range entries {
		l.Trace = ""
		l.SpanId = ""
		if proto.Equal(l, want) {
			return true
		}
		log.Errorf("incorrect log: got %v\nwant %v", l, want)
	}
	return false
}

func gotTrafficAssertion(want *edgespb.TrafficAssertion) bool {
	edges, err := sdInst.ListTrafficAssertions()
	if err != nil {
		log.Errorf("failed to get traffic assertions from stackdriver: %v", err)
		return false
	}

	for _, ta := range edges {
		srcUID := ta.Source.Uid
		dstUID := ta.Destination.Uid

		ta.Source.Location = ""
		ta.Source.ClusterName = ""
		ta.Source.Uid = ""
		ta.Destination.Uid = ""

		if diff := cmp.Diff(ta, want, protocmp.Transform()); diff != "" {
			log.Errorf("different edge found: %v", diff)
			continue
		}

		if strings.HasPrefix(dstUID, "//compute.googleapis.com/projects/test-project/zones/us-west1-c/instances/server-v1-") &&
			strings.HasPrefix(srcUID, "kubernetes://client-v1-") {
			return true
		}
	}

	return false
}

func gotTrace(want *cloudtrace.Trace) bool {
	traces, err := sdInst.ListTraces()
	if err != nil {
		log.Errorf("failed to retrieve list of tracespans from stackdriver: %v", err)
		return false
	}

	for _, trace := range traces {
		if found := traceEqual(trace, want); found {
			return true
		}
	}
	return false
}
