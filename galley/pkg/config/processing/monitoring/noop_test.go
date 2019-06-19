// Copyright 2019 Istio Authors
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

package monitoring

import (
	"testing"
	"time"
)

func TestNoopReporter(t *testing.T) {
	// Make sure these don't crash
	n := &NoopReporter{}
	n.RecordOnMaxTimer()
	n.RecordOnQuiesceTimer()
	n.RecordStrategyOnChange()
	n.RecordProcessorEventProcessed(time.Second)
	n.RecordProcessorSnapshotPublished(0, time.Second)
	n.RecordStateTypeCount("", 0)
	n.RecordStateTypeCountWithContext(nil, 0)
	n.RecordTimerReset()
}
