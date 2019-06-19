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

package strategy

// Reporter is the base metric reporter interface for strategies.
type Reporter interface {
	RecordStrategyOnChange()
}

// DebounceReporter is a metrics reporter for Debounce strategy
type DebounceReporter interface {
	Reporter

	RecordOnMaxTimer()
	RecordOnQuiesceTimer()
	RecordTimerReset()
}

// NoopReporter does not perform any reporting.
type NoopReporter struct {
}

var _ Reporter = &NoopReporter{}
var _ DebounceReporter = &NoopReporter{}

// RecordStrategyOnChange implements SnapshotReporter
func (n *NoopReporter) RecordStrategyOnChange() {}

// RecordOnMaxTimer implements SnapshotReporter
func (n *NoopReporter) RecordOnMaxTimer() {}

// RecordOnQuiesceTimer implements SnapshotReporter
func (n *NoopReporter) RecordOnQuiesceTimer() {}

// RecordTimerReset implements SnapshotReporter
func (n *NoopReporter) RecordTimerReset() {}
