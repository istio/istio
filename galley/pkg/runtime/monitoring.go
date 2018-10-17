// Copyright 2018 Istio Authors
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

package runtime

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/stats/view"
	"context"
	"time"
)

const typeURL = "typeURL"

// TypeURLTag holds the type URL for the context.
var TypeURLTag tag.Key

var (
	strategyOnChangeTotal = stats.Int64(
		"/galley/runtime/strategy/on_change_total",
		"The number of times the strategy's onChange has been called",
		stats.UnitDimensionless)
	strategyOnTimerMaxTimeReachedTotal = stats.Int64(
		"/galley/runtime/strategy/timer_max_time_reached_total",
		"The number of times the max time has been reached",
		stats.UnitDimensionless)
	strategyOnTimerQuiesceReachedTotal = stats.Int64(
		"/galley/runtime/strategy/timer_quiesce_reached_total",
		"The number of times a quiesce has been reached",
		stats.UnitDimensionless)
	strategyOnTimerResetTotal = stats.Int64(
		"/galley/runtime/strategy/timer_resets_total",
		"The number of times the timer has been reset",
		stats.UnitDimensionless)
	processorEventSpans = stats.Int64(
		"/galley/runtime/processor/event_span_duration_seconds",
		"The duration between each incoming event",
		stats.UnitDimensionless)
	processorEventsProcessed = stats.Int64(
		"/galley/runtime/processor/events_processed_total",
		"The number of events that have been processed",
		stats.UnitDimensionless)
	processorSnapshotsPublished = stats.Int64(
		"/galley/runtime/processor/snapshots_published_total",
		"The number of snapshots that have been published",
		stats.UnitDimensionless)
	processorEventsPerSnapshot = stats.Int64(
		"/galley/runtime/processor/snapshot_events_total",
		"The number of events per snapshot",
		stats.UnitDimensionless)
	processorSnapshotLifetimes = stats.Int64(
		"/galley/runtime/processor/snapshot_lifetime_duration_seconds",
		"The duration of each snapshot",
		stats.UnitDimensionless)
	stateTypeInstancesTotal = stats.Int64(
		"/galley/runtime/state/type_instances_total",
		"The number of type instances per type URL",
		stats.UnitDimensionless)

	durationDistribution =
	 	view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8193, 16384, 32768, 65536)
)

func recordStrategyOnChange() {
	stats.Record(context.Background(), strategyOnChangeTotal.M(1))
}

func recordOnTimer(maxTimeReached, quiesceTimeReached, timerReset bool) {
	if maxTimeReached {
		stats.Record(context.Background(), strategyOnTimerMaxTimeReachedTotal.M(1))
	}
	if quiesceTimeReached {
		stats.Record(context.Background(), strategyOnTimerQuiesceReachedTotal.M(1))
	}
	if timerReset {
		stats.Record(context.Background(), strategyOnTimerResetTotal.M(1))
	}
}

func recordProcessorEventProcessed(eventSpan time.Duration) {
	stats.Record(context.Background(), processorEventsProcessed.M(1),
		processorEventSpans.M(int64(eventSpan.Seconds())))
}

func recordProcessorSnapshotPublished(events int, snapshotSpan time.Duration) {
	stats.Record(context.Background(), processorSnapshotsPublished.M(1))
	stats.Record(context.Background(), processorEventsPerSnapshot.M(int64(events)),
		processorSnapshotLifetimes.M(int64(snapshotSpan.Seconds())))
}

func recordStateTypeCount(typeURL string, count int) {
	ctx, err := tag.New(context.Background(), tag.Insert(TypeURLTag, typeURL))
	if err != nil {
		scope.Errorf("Error creating monitoring context for counting state: %v", err)
	}
	stats.Record(ctx, stateTypeInstancesTotal.M(int64(count)))
}

func newView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) *view.View {
	return &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure,
		TagKeys:     keys,
		Aggregation: aggregation,
	}
}

func init() {
	var err error
	if TypeURLTag, err = tag.NewKey(typeURL); err != nil {
		panic(err)
	}

	var noKeys []tag.Key
	typeURLKeys := []tag.Key{TypeURLTag}

	err = view.Register(
		newView(strategyOnTimerResetTotal, noKeys, view.Count()),
		newView(strategyOnChangeTotal, noKeys, view.Count()),
		newView(strategyOnTimerMaxTimeReachedTotal, noKeys, view.Count()),
		newView(strategyOnTimerQuiesceReachedTotal, noKeys, view.Count()),
		newView(
			processorEventSpans,
			noKeys,
			durationDistribution),
		newView(processorEventsProcessed, noKeys, view.Count()),
		newView(processorSnapshotsPublished, noKeys, view.Count()),
		newView(processorEventsPerSnapshot, noKeys, view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256)),
		newView(stateTypeInstancesTotal, typeURLKeys, view.LastValue()),
		newView(processorSnapshotLifetimes, noKeys, durationDistribution),
	)

	if err != nil {
		panic(err)
	}
}