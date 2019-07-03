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

import (
	"sync/atomic"

	"istio.io/istio/galley/pkg/runtime/monitoring"
)

// Immediate is a snapshotting strategy for creating snapshots immediately, as events arrive.
type Immediate struct {
	handler atomic.Value
}

func sentinelOnSnapshot() {}

var _ Instance = &Immediate{}

// NewImmediate returns a new Immediate.
func NewImmediate() *Immediate {
	i := &Immediate{}
	i.handler.Store(OnSnapshotFn(sentinelOnSnapshot))

	return i
}

// Start implements processing.Debounce
func (i *Immediate) Start(handler OnSnapshotFn) {
	i.handler.Store(handler)
}

// Stop implements processing.Debounce
func (i *Immediate) Stop() {
	i.handler.Store(OnSnapshotFn(sentinelOnSnapshot))
}

// OnChange implements processing.Debounce
func (i *Immediate) OnChange() {
	scope.Debug("Immediate.OnChange")
	fn := i.handler.Load().(OnSnapshotFn)

	monitoring.RecordStrategyOnChange()
	fn()
}
