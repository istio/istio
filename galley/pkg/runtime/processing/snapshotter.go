//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import "istio.io/istio/pkg/mcp/snapshot"

// Snapshotter builds a snapshot
type Snapshotter interface {
	Snapshot() snapshot.Snapshot
}

// SnapshotterFromFn creates an Snapshotter based on the given function
func SnapshotterFromFn(fn func() snapshot.Snapshot) Snapshotter {
	return &fnSnapshotter{
		fn: fn,
	}
}

var _ Listener = &fnListener{}

type fnSnapshotter struct {
	fn func() snapshot.Snapshot
}

func (h *fnSnapshotter) Snapshot() snapshot.Snapshot {
	return h.fn()
}
