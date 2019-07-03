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
	"io"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"istio.io/pkg/log"
)

const collection = "collection"

// collectionTag holds the type URL for the context.
var (
	collectionTag = func() tag.Key {
		t, err := tag.NewKey(collection)
		if err != nil {
			panic(err)
		}
		return t
	}()

	// durationDistributionMs = view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8193, 16384, 32768, 65536,
	// 	131072, 262144, 524288, 1048576, 2097152, 4194304, 8388608)
	//
	// eventDistribution = view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256)
)

type reporter struct {
	strategyOnChangeTotal              *stats.Int64Measure
	strategyOnTimerMaxTimeReachedTotal *stats.Int64Measure
	strategyOnTimerQuiesceReachedTotal *stats.Int64Measure
	strategyOnTimerResetTotal          *stats.Int64Measure
	processorEventSpansMs              *stats.Int64Measure
	processorEventsProcessed           *stats.Int64Measure
	processorSnapshotsPublished        *stats.Int64Measure
	processorEventsPerSnapshot         *stats.Int64Measure
	processorSnapshotLifetimesMs       *stats.Int64Measure
	stateTypeInstancesTotal            *stats.Int64Measure

	views []*view.View
}

var _ io.Closer = &reporter{}
var _ Reporter = &reporter{}

func createOpenCensusReporter() (*reporter, error) { // nolint:unparam
	r := &reporter{
		strategyOnChangeTotal: stats.Int64(
			"galley/runtime/strategy/on_change_total",
			"The number of times the strategy's onChange has been called",
			stats.UnitDimensionless),

		strategyOnTimerMaxTimeReachedTotal: stats.Int64(
			"galley/runtime/strategy/timer_max_time_reached_total",
			"The number of times the max time has been reached",
			stats.UnitDimensionless),

		strategyOnTimerQuiesceReachedTotal: stats.Int64(
			"galley/runtime/strategy/timer_quiesce_reached_total",
			"The number of times a quiesce has been reached",
			stats.UnitDimensionless),

		strategyOnTimerResetTotal: stats.Int64(
			"galley/runtime/strategy/timer_resets_total",
			"The number of times the timer has been reset",
			stats.UnitDimensionless),

		processorEventSpansMs: stats.Int64(
			"galley/runtime/processor/event_span_duration_milliseconds",
			"The duration between each incoming event",
			stats.UnitMilliseconds),

		processorEventsProcessed: stats.Int64(
			"galley/runtime/processor/events_processed_total",
			"The number of events that have been processed",
			stats.UnitDimensionless),

		processorSnapshotsPublished: stats.Int64(
			"galley/runtime/processor/snapshots_published_total",
			"The number of snapshots that have been published",
			stats.UnitDimensionless),

		processorEventsPerSnapshot: stats.Int64(
			"galley/runtime/processor/snapshot_events_total",
			"The number of events per snapshot",
			stats.UnitDimensionless),

		processorSnapshotLifetimesMs: stats.Int64(
			"galley/runtime/processor/snapshot_lifetime_duration_milliseconds",
			"The duration of each snapshot",
			stats.UnitMilliseconds),

		stateTypeInstancesTotal: stats.Int64(
			"galley/runtime/state/type_instances_total",
			"The number of type instances per type URL",
			stats.UnitDimensionless),
	}

	// TODO: These collide with the originals
	// var noKeys []tag.Key
	// collectionKeys := []tag.Key{collectionTag}
	//
	// r.views = []*view.View{
	// 	newView(r.strategyOnTimerResetTotal, noKeys, view.Count()),
	// 	newView(r.strategyOnChangeTotal, noKeys, view.Count()),
	// 	newView(r.strategyOnTimerMaxTimeReachedTotal, noKeys, view.Count()),
	// 	newView(r.strategyOnTimerQuiesceReachedTotal, noKeys, view.Count()),
	// 	newView(r.processorEventSpansMs, noKeys, durationDistributionMs),
	// 	newView(r.processorEventsProcessed, noKeys, view.Count()),
	// 	newView(r.processorSnapshotsPublished, noKeys, view.Count()),
	// 	newView(r.processorEventsPerSnapshot, noKeys, eventDistribution),
	// 	newView(r.stateTypeInstancesTotal, collectionKeys, view.LastValue()),
	// 	newView(r.processorSnapshotLifetimesMs, noKeys, durationDistributionMs),
	// }
	//
	// if err := view.Register(r.views...); err != nil {
	// 	return nil, err
	// }

	return r, nil
}

// RecordStrategyOnChange implements Reporter
func (r *reporter) RecordStrategyOnChange() {
	stats.Record(context.Background(), r.strategyOnChangeTotal.M(1))
}

// RecordOnMaxTimer implements Reporter
func (r *reporter) RecordOnMaxTimer() {
	stats.Record(context.Background(), r.strategyOnTimerMaxTimeReachedTotal.M(1))
}

// RecordOnQuiesceTimer implements Reporter
func (r *reporter) RecordOnQuiesceTimer() {
	stats.Record(context.Background(), r.strategyOnTimerQuiesceReachedTotal.M(1))
}

// RecordTimerReset implements Reporter
func (r *reporter) RecordTimerReset() {
	stats.Record(context.Background(), r.strategyOnTimerResetTotal.M(1))
}

// RecordProcessorEventProcessed
func (r *reporter) RecordProcessorEventProcessed(eventSpan time.Duration) {
	stats.Record(context.Background(), r.processorEventsProcessed.M(1),
		r.processorEventSpansMs.M(eventSpan.Nanoseconds()/1e6))
}

// RecordProcessorSnapshotPublished
func (r *reporter) RecordProcessorSnapshotPublished(events int64, snapshotSpan time.Duration) {
	stats.Record(context.Background(), r.processorSnapshotsPublished.M(1))
	stats.Record(context.Background(), r.processorEventsPerSnapshot.M(events),
		r.processorSnapshotLifetimesMs.M(snapshotSpan.Nanoseconds()/1e6))
}

// RecordStateTypeCount
func (r *reporter) RecordStateTypeCount(collection string, count int) {
	ctx, err := tag.New(context.Background(), tag.Insert(collectionTag, collection))
	if err != nil {
		log.Errorf("Error creating monitoring context for counting state: %v", err)
	} else {
		r.RecordStateTypeCountWithContext(ctx, count)
	}
}

// RecordStateTypeCountWithContext
func (r *reporter) RecordStateTypeCountWithContext(ctx context.Context, count int) {
	if ctx != nil {
		stats.Record(ctx, r.stateTypeInstancesTotal.M(int64(count)))
	}
}

// Close implements io.Closer
func (r *reporter) Close() error {
	for _, v := range r.views {
		view.Unregister(v)
	}
	return nil
}
