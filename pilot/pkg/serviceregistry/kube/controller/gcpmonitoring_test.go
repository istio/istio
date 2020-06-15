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

package controller

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	gm "istio.io/istio/pilot/pkg/gcpmonitoring"
	"istio.io/istio/pkg/test/util/retry"
)

func TestGCPMonitoringPilotK8sRegEvents(t *testing.T) {
	os.Setenv("ENABLE_STACKDRIVER_MONITORING", "true")
	defer os.Unsetenv("ENABLE_STACKDRIVER_MONITORING")
	exp := &gm.TestExporter{Rows: make(map[string][]*view.Row)}
	view.RegisterExporter(exp)
	view.SetReportingPeriod(1 * time.Millisecond)

	incrementEvent("foo", "bar")
	if err := retry.UntilSuccess(func() error {
		exp.Lock()
		defer exp.Unlock()

		if len(exp.Rows["config_event_count"]) < 1 {
			return errors.New("wanted metrics not received")
		}
		for _, r := range exp.Rows["config_event_count"] {
			got := float64(0)
			if findTagWithValue("operation", "bar", r.Tags) && findTagWithValue("type", "foo", r.Tags) {
				if sd, ok := r.Data.(*view.SumData); ok {
					got = sd.Value
				}
			}
			if got >= 1.0 {
				return nil
			}
		}
		return fmt.Errorf("cannot find config_event_count{operation:bar, type:foo}, want at least 1")
	}); err != nil {
		t.Fatalf("failed to get expected metric: %v", err)
	}
}

func findTagWithValue(key, value string, tags []tag.Tag) bool {
	for _, t := range tags {
		if t.Key.Name() == key && t.Value == value {
			return true
		}
	}
	return false
}
