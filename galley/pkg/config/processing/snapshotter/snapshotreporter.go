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

package snapshotter

import (
	"time"

	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
)

// SnapshotReporter is an event reporter for Snapshot events
type SnapshotReporter interface {
	RecordStrategyOnChange()

	RecordOnMaxTimer()
	RecordOnQuiesceTimer()
	RecordTimerReset()

	RecordProcessorSnapshotPublished(events int64, snapshotSpan time.Duration)
	RecordProcessorEventProcessed(eventSpan time.Duration)
	RecordStateTypeCount(collection string, count int)
}

// Ensure that the snapshot reporter can be used by strategies. Otherwise, they will need to be
// plumbed in separately.
var _ strategy.Reporter = SnapshotReporter(nil)
var _ strategy.DebounceReporter = SnapshotReporter(nil)

// NoopReporter does not perform any reporting.
type NoopReporter struct {
}

var _ SnapshotReporter = &NoopReporter{}

// RecordStrategyOnChange implements SnapshotReporter
func (n *NoopReporter) RecordStrategyOnChange() {}

// RecordOnMaxTimer implements SnapshotReporter
func (n *NoopReporter) RecordOnMaxTimer() {}

// RecordOnQuiesceTimer implements SnapshotReporter
func (n *NoopReporter) RecordOnQuiesceTimer() {}

// RecordTimerReset implements SnapshotReporter
func (n *NoopReporter) RecordTimerReset() {}

// RecordStateTypeCount implements SnapshotReporter
func (n *NoopReporter) RecordStateTypeCount(_ string, _ int) {}

// RecordProcessorEventProcessed implements SnapshotReporter
func (n *NoopReporter) RecordProcessorEventProcessed(eventSpan time.Duration) {}

// RecordProcessorSnapshotPublished implements SnapshotReporter
func (n *NoopReporter) RecordProcessorSnapshotPublished(_ int64, _ time.Duration) {}
