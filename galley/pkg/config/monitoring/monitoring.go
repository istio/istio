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

package monitoring

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"istio.io/istio/galley/pkg/config/scope"
)

const (
	collection = "collection"
	namespace  = "namespace"
	name       = "name"
	version    = "version"
)

var (
	// CollectionTag holds the type URL for the context.
	CollectionTag tag.Key
	// NamespaceTag holds namespace of the resource for the context.
	NamespaceTag tag.Key
	// NameTag holds name of the resource for the context.
	NameTag tag.Key
	// VersionTag holds version of the resource for the context.
	VersionTag tag.Key
	// StateTypeConfigKeys holds key tags for runtime state metrics.
	StateTypeConfigKeys []tag.Key
)

var (
	strategyOnChangeTotal = stats.Int64(
		"galley/runtime/strategy/on_change_total",
		"The number of times the strategy's onChange has been called",
		stats.UnitDimensionless)
	strategyOnTimerMaxTimeReachedTotal = stats.Int64(
		"galley/runtime/strategy/timer_max_time_reached_total",
		"The number of times the max time has been reached",
		stats.UnitDimensionless)
	strategyOnTimerQuiesceReachedTotal = stats.Int64(
		"galley/runtime/strategy/timer_quiesce_reached_total",
		"The number of times a quiesce has been reached",
		stats.UnitDimensionless)
	strategyOnTimerResetTotal = stats.Int64(
		"galley/runtime/strategy/timer_resets_total",
		"The number of times the timer has been reset",
		stats.UnitDimensionless)
	processorEventSpansMs = stats.Int64(
		"galley/runtime/processor/event_span_duration_milliseconds",
		"The duration between each incoming event",
		stats.UnitMilliseconds)
	processorEventsProcessed = stats.Int64(
		"galley/runtime/processor/events_processed_total",
		"The number of events that have been processed",
		stats.UnitDimensionless)
	processorSnapshotsPublished = stats.Int64(
		"galley/runtime/processor/snapshots_published_total",
		"The number of snapshots that have been published",
		stats.UnitDimensionless)
	processorEventsPerSnapshot = stats.Int64(
		"galley/runtime/processor/snapshot_events_total",
		"The number of events per snapshot",
		stats.UnitDimensionless)
	processorSnapshotLifetimesMs = stats.Int64(
		"galley/runtime/processor/snapshot_lifetime_duration_milliseconds",
		"The duration of each snapshot",
		stats.UnitMilliseconds)
	stateTypeInstancesTotal = stats.Int64(
		"galley/runtime/state/type_instances_total",
		"The number of type instances per type URL",
		stats.UnitDimensionless)

	durationDistributionMs = view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8193, 16384, 32768, 65536,
		131072, 262144, 524288, 1048576, 2097152, 4194304, 8388608)

	stateTypeConfigTotal     map[string]*stats.Int64Measure
	stateTypeCollectionMutex sync.RWMutex
)

// RecordStrategyOnChange event
func RecordStrategyOnChange() {
	stats.Record(context.Background(), strategyOnChangeTotal.M(1))
}

// RecordOnTimer event
func RecordOnTimer(maxTimeReached, quiesceTimeReached, timerReset bool) {
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

// RecordProcessorEventProcessed event
func RecordProcessorEventProcessed(eventSpan time.Duration) {
	stats.Record(context.Background(), processorEventsProcessed.M(1),
		processorEventSpansMs.M(eventSpan.Nanoseconds()/1e6))
}

// RecordProcessorSnapshotPublished event
func RecordProcessorSnapshotPublished(events int64, snapshotSpan time.Duration) {
	stats.Record(context.Background(), processorSnapshotsPublished.M(1))
	stats.Record(context.Background(), processorEventsPerSnapshot.M(events),
		processorSnapshotLifetimesMs.M(snapshotSpan.Nanoseconds()/1e6))
}

// RecordStateTypeCount event
func RecordStateTypeCount(collection string, count int) {
	ctx, err := tag.New(context.Background(), tag.Insert(CollectionTag, collection))
	if err != nil {
		scope.Processing.Errorf("Error creating monitoring context for counting state: %v", err)
	} else {
		RecordStateTypeCountWithContext(ctx, count)
	}
}

// RecordStateTypeCountWithContext event
func RecordStateTypeCountWithContext(ctx context.Context, count int) {
	if ctx != nil {
		stats.Record(ctx, stateTypeInstancesTotal.M(int64(count)))
	}
}

// RecordDetailedStateType records name, namespace, version of the resource in Galley.
func RecordDetailedStateType(namespace, name string, collection fmt.Stringer, count int) {
	collectionStr := strings.Split(collection.String(), "/")
	// collection is of the format istio/<kind>/<version>/<name>
	if len(collectionStr) < 4 {
		scope.Processing.Errorf("length of collection is less than 4, does not match expectation. collection: %v",
			collectionStr)
		return
	}
	ctx, err := tag.New(context.Background(), tag.Insert(NamespaceTag, namespace),
		tag.Insert(NameTag, name), tag.Insert(VersionTag, collectionStr[2]))
	if err != nil {
		scope.Processing.Errorf("error creating monitoring context for counting state: %v", err)
		return
	}

	// We remove version from the collection name as it has been added as the VersionTag in the measurement.
	collectionName := strings.Replace(collection.String(),
		fmt.Sprintf("%s/", collectionStr[2]), "", 1)
	RecordDetailedStateTypeWithContext(ctx, collectionName, count)
}

// RecordDetailedStateTypeWithContext event
func RecordDetailedStateTypeWithContext(ctx context.Context, collection string, count int) {
	if ctx == nil {
		return
	}
	stateTypeCollectionMutex.Lock()
	defer stateTypeCollectionMutex.Unlock()
	if stateTypeConfigTotal[collection] == nil {
		err := registerNewStateTypeConfigView(collection)
		if err != nil {
			scope.Processing.Errorf("could not register collection %v for monitoring", err)
			return
		}
	}
	stats.Record(ctx, stateTypeConfigTotal[collection].M(int64(count)))
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

func getStateTypeConfigKeys() ([]tag.Key, error) {
	var err error
	if NamespaceTag, err = tag.NewKey(namespace); err != nil {
		return nil, err
	}
	if NameTag, err = tag.NewKey(name); err != nil {
		return nil, err
	}
	if VersionTag, err = tag.NewKey(version); err != nil {
		return nil, err
	}

	return []tag.Key{NamespaceTag, NameTag, VersionTag}, err
}

func registerNewStateTypeConfigView(collection string) error {
	stateTypeConfigTotal[collection] = stats.Int64(fmt.Sprintf("galley/%s", collection),
		fmt.Sprintf("The number of valid %v known to galley at a point in time", collection),
		stats.UnitDimensionless)
	err := view.Register(
		newView(stateTypeConfigTotal[collection], StateTypeConfigKeys, view.LastValue()),
	)
	return err
}

func init() {
	var err error
	if CollectionTag, err = tag.NewKey(collection); err != nil {
		panic(err)
	}

	var noKeys []tag.Key
	collectionKeys := []tag.Key{CollectionTag}

	err = view.Register(
		newView(strategyOnTimerResetTotal, noKeys, view.Count()),
		newView(strategyOnChangeTotal, noKeys, view.Count()),
		newView(strategyOnTimerMaxTimeReachedTotal, noKeys, view.Count()),
		newView(strategyOnTimerQuiesceReachedTotal, noKeys, view.Count()),
		newView(processorEventSpansMs, noKeys, durationDistributionMs),
		newView(processorEventsProcessed, noKeys, view.Count()),
		newView(processorSnapshotsPublished, noKeys, view.Count()),
		newView(processorEventsPerSnapshot, noKeys, view.Distribution(0, 1, 2, 4, 8, 16, 32, 64, 128, 256)),
		newView(processorSnapshotLifetimesMs, noKeys, durationDistributionMs),
		newView(stateTypeInstancesTotal, collectionKeys, view.LastValue()),
	)

	if err != nil {
		panic(err)
	}

	stateTypeConfigTotal = make(map[string]*stats.Int64Measure)
	StateTypeConfigKeys, err = getStateTypeConfigKeys()
	if err != nil {
		panic(err)
	}
}
