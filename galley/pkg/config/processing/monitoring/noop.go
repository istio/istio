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
	"context"
	"time"
)

// NoopReporter is a noop implementation of Reporter.
type NoopReporter struct {
}

var _ Reporter = &NoopReporter{}

// RecordStrategyOnChange implements Reporter
func (r *NoopReporter) RecordStrategyOnChange() {
}

// RecordOnMaxTimer implements Reporter
func (r *NoopReporter) RecordOnMaxTimer() {
}

// RecordOnQuiesceTimer implements Reporter
func (r *NoopReporter) RecordOnQuiesceTimer() {
}

// RecordTimerReset implements Reporter
func (r *NoopReporter) RecordTimerReset() {
}

// RecordProcessorEventProcessed implements Reporter
func (r *NoopReporter) RecordProcessorEventProcessed(_ time.Duration) {
}

// RecordProcessorSnapshotPublished implements Reporter
func (r *NoopReporter) RecordProcessorSnapshotPublished(_ int64, _ time.Duration) {
}

// RecordStateTypeCount implements Reporter
func (r *NoopReporter) RecordStateTypeCount(_ string, _ int) {
}

// RecordStateTypeCountWithContext implements Reporter
func (r *NoopReporter) RecordStateTypeCountWithContext(_ context.Context, _ int) {
}
