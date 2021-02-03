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

package xds

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	gm "istio.io/istio/pilot/pkg/gcpmonitoring"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/monitoring"
)

var (
	successTestTag  = tag.MustNewKey("success")
	typeTestTag     = tag.MustNewKey("type")
	proxyVersionTag = tag.MustNewKey("proxy_version")
)

func TestGCPMonitoringPilotXDSMetrics(t *testing.T) {
	os.Setenv("ENABLE_STACKDRIVER_MONITORING", "true")
	defer os.Unsetenv("ENABLE_STACKDRIVER_MONITORING")
	exp := &gm.TestExporter{Rows: make(map[string][]*view.Row)}
	view.RegisterExporter(exp)
	view.SetReportingPeriod(1 * time.Millisecond)

	cases := []struct {
		name       string
		m          monitoring.Metric
		increment  bool
		recordVal  float64
		wantMetric string
		wantVal    *view.Row
	}{
		{"cdsPushes", pushes.With(typeTag.Value(v3.GetMetricType(resource.ClusterType))), true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "true"}, {Key: typeTestTag, Value: "CDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"edsPushes", pushes.With(typeTag.Value(v3.GetMetricType(resource.EndpointType))), true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "true"}, {Key: typeTestTag, Value: "EDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"ldsPushes", pushes.With(typeTag.Value(v3.GetMetricType(resource.ListenerType))), true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "true"}, {Key: typeTestTag, Value: "LDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"rdsPushes", pushes.With(typeTag.Value(v3.GetMetricType(resource.RouteType))), true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "true"}, {Key: typeTestTag, Value: "RDS"}}, Data: &view.SumData{Value: 1.0},
		}},

		{"cdsSendErrPushes", cdsSendErrPushes, true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "false"}, {Key: typeTestTag, Value: "CDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"edsSendErrPushes", edsSendErrPushes, true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "false"}, {Key: typeTestTag, Value: "EDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"ldsSendErrPushes", ldsSendErrPushes, true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "false"}, {Key: typeTestTag, Value: "LDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"rdsSendErrPushes", rdsSendErrPushes, true, 0, "config_push_count", &view.Row{
			Tags: []tag.Tag{{Key: successTestTag, Value: "false"}, {Key: typeTestTag, Value: "RDS"}}, Data: &view.SumData{Value: 1.0},
		}},

		{"cdsReject", cdsReject, true, 0, "rejected_config_count", &view.Row{
			Tags: []tag.Tag{{Key: typeTestTag, Value: "CDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"edsReject", edsReject, true, 0, "rejected_config_count", &view.Row{
			Tags: []tag.Tag{{Key: typeTestTag, Value: "EDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"ldsReject", ldsReject, true, 0, "rejected_config_count", &view.Row{
			Tags: []tag.Tag{{Key: typeTestTag, Value: "LDS"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"rdsReject", rdsReject, true, 0, "rejected_config_count", &view.Row{
			Tags: []tag.Tag{{Key: typeTestTag, Value: "RDS"}}, Data: &view.SumData{Value: 1.0},
		}},

		{"xdsClients", xdsClients.With(versionTag.Value("test-version")), false, 10, "proxy_clients", &view.Row{
			Tags: []tag.Tag{{Key: proxyVersionTag, Value: "test-version"}}, Data: &view.LastValueData{Value: 10.0},
		}},

		{"proxiesConvergeDelay", proxiesConvergeDelay, false, 0.4, "config_convergence_latencies", &view.Row{
			Tags: []tag.Tag{},
			Data: &view.DistributionData{
				CountPerBucket: []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0},
			},
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			exp.Lock()
			exp.Rows = make(map[string][]*view.Row)
			exp.Unlock()
			if tt.increment {
				tt.m.Increment()
			} else {
				tt.m.Record(tt.recordVal)
			}
			if err := retry.UntilSuccess(func() error {
				exp.Lock()
				defer exp.Unlock()
				if len(exp.Rows[tt.wantMetric]) < 1 {
					return fmt.Errorf("wanted metrics %v not received", tt.wantMetric)
				}
				for _, got := range exp.Rows[tt.wantMetric] {
					if len(got.Tags) != len(tt.wantVal.Tags) ||
						(len(tt.wantVal.Tags) != 0 && !reflect.DeepEqual(got.Tags, tt.wantVal.Tags)) {
						continue
					}
					switch v := tt.wantVal.Data.(type) {
					case *view.SumData:
						if int64(v.Value) == int64(got.Data.(*view.SumData).Value) {
							return nil
						}
					case *view.LastValueData:
						if int64(v.Value) == int64(got.Data.(*view.LastValueData).Value) {
							return nil
						}
					case *view.DistributionData:
						gotDist := got.Data.(*view.DistributionData)
						if reflect.DeepEqual(gotDist.CountPerBucket, v.CountPerBucket) {
							return nil
						}
					}
				}
				return fmt.Errorf("metrics %v does not have expected values, want %+v", tt.m.Name(), tt.wantVal)
			}); err != nil {
				t.Fatalf("failed to get expected metric: %v", err)
			}
		})
	}
}
