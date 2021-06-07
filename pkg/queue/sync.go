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

package queue

import "go.uber.org/atomic"

// SyncInstance is a specialized queue.Instance that tracks whether the queue has synced yet
type SyncInstance interface {
	Instance
	// MarkSynced indicates that the queue should be synced. The queue will not be marked synced immediately.
	// Instead, it will enqueue a task to mark it as synced. This ensures that we complete all pending tasks prior
	// to marking ourselves as synced.
	// A boolean indicating whether we have synced is returned as well.
	MarkSynced() bool
}

type syncQueue struct {
	Instance
	syncState *atomic.Uint32
}

func (s *syncQueue) MarkSynced() bool {
	state := s.syncState.Load()
	switch state {
	case Synced:
		return true
	case Unsynced:
		s.syncState.Store(Enqueued)
		s.Push(func() error {
			s.syncState.Store(Synced)
			return nil
		})
		return false
	default:
		return false
	}
}

type SyncState = uint32

const (
	Unsynced SyncState = 0
	Enqueued SyncState = 1
	Synced   SyncState = 2
)

func NewSync(q Instance) SyncInstance {
	return &syncQueue{
		Instance:  q,
		syncState: atomic.NewUint32(Unsynced),
	}
}
