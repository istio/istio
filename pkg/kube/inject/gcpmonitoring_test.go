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

package inject

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	gm "istio.io/istio/pilot/pkg/gcpmonitoring"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/monitoring"
)

func TestGCPMonitoringSidecarInjection(t *testing.T) {
	os.Setenv("ENABLE_STACKDRIVER_MONITORING", "true")
	defer os.Unsetenv("ENABLE_STACKDRIVER_MONITORING")
	exp := &gm.TestExporter{Rows: make(map[string][]*view.Row)}
	view.RegisterExporter(exp)
	view.SetReportingPeriod(1 * time.Millisecond)

	cases := []struct {
		name    string
		m       monitoring.Metric
		wantVal *view.Row
	}{
		{"totalSuccessfulInjections", totalSuccessfulInjections, &view.Row{
			Tags: []tag.Tag{{Key: tag.MustNewKey("success"), Value: "true"}}, Data: &view.SumData{Value: 1.0},
		}},
		{"totalFailedInjections", totalFailedInjections, &view.Row{
			Tags: []tag.Tag{{Key: tag.MustNewKey("success"), Value: "false"}}, Data: &view.SumData{Value: 1.0},
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			exp.Lock()
			exp.Rows = make(map[string][]*view.Row)
			exp.Unlock()
			tt.m.Increment()
			wantMetric := "sidecar_injection_count"
			if err := retry.UntilSuccess(func() error {
				exp.Lock()
				defer exp.Unlock()
				if len(exp.Rows[wantMetric]) < 1 {
					return fmt.Errorf("wanted metrics %v not received", wantMetric)
				}
				for _, got := range exp.Rows[wantMetric] {
					if !reflect.DeepEqual(got.Tags, tt.wantVal.Tags) {
						continue
					}
					if int64(tt.wantVal.Data.(*view.SumData).Value) == int64(got.Data.(*view.SumData).Value) {
						return nil
					}
				}
				return fmt.Errorf("metrics %v does not have expected values, want %+v", tt.m.Name(), tt.wantVal)
			}); err != nil {
				t.Fatalf("failed to get expected metric: %v", err)
			}
		})
	}
}
