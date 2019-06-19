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
	"time"

	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
)

// Reporter is a config-system wide reporter that can be used by all libraries, as a common
// reporting interface.
type Reporter interface {
	RecordStrategyOnChange()

	RecordOnMaxTimer()
	RecordOnQuiesceTimer()
	RecordTimerReset()

	RecordProcessorSnapshotPublished(events int64, snapshotSpan time.Duration)
	RecordProcessorEventProcessed(eventSpan time.Duration)
	RecordStateTypeCount(collection string, count int)
}

var _ strategy.Reporter = Reporter(nil)
var _ strategy.DebounceReporter = Reporter(nil)
var _ snapshotter.SnapshotReporter = Reporter(nil)
