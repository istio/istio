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

package runtime

import (
	"sync"

	sn "istio.io/istio/pkg/mcp/snapshot"
)

// Distributor interface allows processor to distribute snapshots of configuration.
type Distributor interface {
	SetSnapshot(name string, snapshot sn.Snapshot)

	ClearSnapshot(name string)
}

// InMemoryDistributor is an in-memory distributor implementation.
type InMemoryDistributor struct {
	snapshotsLock sync.Mutex
	snapshots     map[string]sn.Snapshot
	listenersLock sync.Mutex
	listeners     []*listenerEntry
}

type listenerEntry struct {
	// action that the listener want to take once it get notified
	action ListenerFn
	// channel used to receive snapshot
	snapshotChan chan sn.Snapshot
}

// ListenerFn is used by listeners for defining listen action
type ListenerFn func(s sn.Snapshot)

var _ Distributor = &InMemoryDistributor{}

// NewInMemoryDistributor returns a new instance of InMemoryDistributor
func NewInMemoryDistributor() *InMemoryDistributor {
	return &InMemoryDistributor{
		snapshots: make(map[string]sn.Snapshot),
	}
}

// SetSnapshot is an implementation of Distributor.SetSnapshot
func (d *InMemoryDistributor) SetSnapshot(name string, snapshot sn.Snapshot) {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()

	d.snapshots[name] = snapshot
	d.notifyListeners(snapshot)
}

// ClearSnapshot is an implementation of Distributor.ClearSnapshot
func (d *InMemoryDistributor) ClearSnapshot(name string) {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()

	delete(d.snapshots, name)
}

// GetSnapshot get the snapshot of the specified name
func (d *InMemoryDistributor) GetSnapshot(name string) sn.Snapshot {
	d.snapshotsLock.Lock()
	defer d.snapshotsLock.Unlock()
	if s, ok := d.snapshots[name]; ok {
		return s
	}
	return nil
}

// NumSnapshots returns the current number of snapshots.
func (d *InMemoryDistributor) NumSnapshots() int {
	return len(d.snapshots)
}

// ListenChanges registered listener and start listening snapshot changes in the distributor
func (d *InMemoryDistributor) ListenChanges(cancel chan bool, fn ListenerFn) {
	snapshotChan := make(chan sn.Snapshot, 1)
	l := &listenerEntry{fn, snapshotChan}
	d.listenersLock.Lock()
	d.listeners = append(d.listeners, l)
	d.listenersLock.Unlock()

	for {
		select {
		case <-cancel:
			d.deleteListener(l)
			close(snapshotChan)
			return
		case s := <-snapshotChan:
			fn(s)
		}
	}
}

// Called internally when a snapshot is set
func (d *InMemoryDistributor) notifyListeners(s sn.Snapshot) {
	d.listenersLock.Lock()
	defer d.listenersLock.Unlock()
	for _, l := range d.listeners {
		l.snapshotChan <- s
	}
}

// Called internally when a listener is stopped
func (d *InMemoryDistributor) deleteListener(l *listenerEntry) {
	d.listenersLock.Lock()
	defer d.listenersLock.Unlock()
	for i := len(d.listeners) - 1; i >= 0; i-- {
		if d.listeners[i] == l {
			d.listeners = append(d.listeners[:i], d.listeners[i+1:]...)
			l = nil
			return
		}
	}
}
