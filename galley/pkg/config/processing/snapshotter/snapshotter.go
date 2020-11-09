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

package snapshotter

import (
	"fmt"
	"sync/atomic"
	"time"

	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/monitoring"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

// Snapshotter is a processor that handles input events and creates snapshotImpl collections.
type Snapshotter struct {
	// pendingEvents must be at start of struct to ensure 64bit alignment for atomics on
	// 32bit architectures. See also: https://golang.org/pkg/sync/atomic/#pkg-note-BUG

	// pendingEvents counts the number of events awaiting publishing.
	pendingEvents int64

	// lastSnapshotTime records the last time a snapshotImpl was published.
	lastSnapshotTime atomic.Value

	accumulators   map[collection.Name]*accumulator
	selector       event.Router
	xforms         []event.Transformer
	settings       []SnapshotOptions
	snapshotGroups []*snapshotGroup

	// lastEventTime records the last time an event was received.
	lastEventTime time.Time
}

var _ event.Processor = &Snapshotter{}

type accumulator struct {
	reqSyncCount   int32
	syncCount      int32
	collection     *coll.Instance
	snapshotGroups []*snapshotGroup
}

// snapshotGroup represents a group of collections that all need to be synced before any of them publish events,
// and also share a set of strategies.
// Members of a snapshot group are defined via the per-collection accumulators that point to a group.
type snapshotGroup struct {
	// How many collections in the current group still need to receive a FullSync before we start publishing.
	remaining int32
	// Set of collections that have already received a FullSync. If we get a duplicate, it will be ignored.
	synced atomic.Value
	// Strategy to execute on handled events only after all collections in the group have been synced.
	strategy strategy.Instance
}

// Handle implements event.Handler
func (a *accumulator) Handle(e event.Event) {
	switch e.Kind {
	case event.Added:
		a.collection.Set(e.Resource)
		monitoring.RecordStateTypeCount(e.Source.Name().String(), a.collection.Size())
		monitorEntry(e.Source, e.Resource.Metadata.FullName, true)

	case event.Updated:
		a.collection.Set(e.Resource)

	case event.Deleted:
		a.collection.Remove(e.Resource.Metadata.FullName)
		monitoring.RecordStateTypeCount(e.Source.Name().String(), a.collection.Size())
		monitorEntry(e.Source, e.Resource.Metadata.FullName, false)

	case event.FullSync:
		atomic.AddInt32(&a.syncCount, 1)

	default:
		panic(fmt.Errorf("accumulator.Handle: unhandled event type: %v", e.Kind))
	}

	// Update the group sync counter if we received all required FullSync events for a collection
	for _, sg := range a.snapshotGroups {
		if atomic.LoadInt32(&a.syncCount) >= a.reqSyncCount {
			sg.onSync(a.collection)
		}
	}
}

func (a *accumulator) reset() {
	atomic.StoreInt32(&a.syncCount, 0)
	a.collection.Clear()
}

func monitorEntry(col collection.Schema, resourceName resource.FullName, added bool) {
	value := 1
	if !added {
		value = 0
	}
	colName := collection.Name("")
	if col != nil {
		colName = col.Name()
	}
	monitoring.RecordDetailedStateType(string(resourceName.Namespace), string(resourceName.Name), colName, value)
}

// NewSnapshotter returns a new Snapshotter.
func NewSnapshotter(xforms []event.Transformer, settings []SnapshotOptions) (*Snapshotter, error) {
	s := &Snapshotter{
		accumulators:  make(map[collection.Name]*accumulator),
		selector:      event.NewRouter(),
		xforms:        xforms,
		settings:      settings,
		lastEventTime: time.Now(),
	}

	for _, xform := range xforms {
		xform.Inputs().ForEach(func(i collection.Schema) (done bool) {
			s.selector = event.AddToRouter(s.selector, i, xform)
			return
		})

		xform.Outputs().ForEach(func(o collection.Schema) (done bool) {
			a, found := s.accumulators[o.Name()]
			if !found {
				a = &accumulator{
					collection: coll.New(o),
				}
				s.accumulators[o.Name()] = a
			}
			a.reqSyncCount++
			xform.DispatchFor(o, a)
			return
		})
	}

	for _, o := range settings {
		sg := newSnapshotGroup(len(o.Collections), o.Strategy)
		s.snapshotGroups = append(s.snapshotGroups, sg)
		for _, c := range o.Collections {
			a := s.accumulators[c]
			if a == nil {
				return nil, fmt.Errorf("unrecognized collection in SnapshotOptions: %v (Group: %s)", c, o.Group)
			}

			a.snapshotGroups = append(a.snapshotGroups, sg)
		}
	}

	return s, nil
}

func newSnapshotGroup(size int, strategy strategy.Instance) *snapshotGroup {
	sg := &snapshotGroup{
		strategy: strategy,
	}
	sg.reset(size)
	return sg
}

func (sg *snapshotGroup) onSync(c *coll.Instance) {
	synced := sg.synced.Load().(map[*coll.Instance]bool)
	if !synced[c] {
		atomic.AddInt32(&sg.remaining, -1)
		synced[c] = true
		scope.Processing.Debugf("sg.onSync: %v fully synced, %d remaining", c.Name(), sg.remaining)
	}

	// proceed with triggering the strategy OnChange only after we've full synced every collection in a group.
	if atomic.LoadInt32(&sg.remaining) <= 0 {
		scope.Processing.Debugf("sg.onSync: all collections synced, proceeding with strategy.OnChange()")
		sg.strategy.OnChange()
	}
}

func (sg *snapshotGroup) reset(size int) {
	atomic.StoreInt32(&sg.remaining, int32(size))
	sg.synced.Store(make(map[*coll.Instance]bool))
}

// Start implements Processor
func (s *Snapshotter) Start() {
	for _, x := range s.xforms {
		x.Start()
	}

	for _, o := range s.settings {
		// Capture the iteration variable in a local
		opt := o
		o.Strategy.Start(func() {
			s.publish(opt)
		})
	}
}

func (s *Snapshotter) publish(o SnapshotOptions) {
	var collections []*coll.Instance

	for _, n := range o.Collections {
		col := s.accumulators[n].collection.Clone()
		collections = append(collections, col)
	}

	set := coll.NewSetFromCollections(collections)
	sn := &Snapshot{set: set}

	s.markSnapshotTime()
	atomic.StoreInt64(&s.pendingEvents, 0)
	scope.Processing.Infoa("Publishing snapshot for group: ", o.Group)
	scope.Processing.Debuga(sn)
	o.Distributor.Distribute(o.Group, sn)
}

// Stop implements Processor
func (s *Snapshotter) Stop() {
	for _, o := range s.settings {
		o.Strategy.Stop()
	}

	for _, x := range s.xforms {
		x.Stop()
	}

	for _, a := range s.accumulators {
		a.reset()
	}

	for i, o := range s.settings {
		s.snapshotGroups[i].reset(len(o.Collections))
	}
}

// Handle implements Processor
func (s *Snapshotter) Handle(e event.Event) {
	now := time.Now()
	monitoring.RecordProcessorEventProcessed(now.Sub(s.lastEventTime))
	s.lastEventTime = now
	atomic.AddInt64(&s.pendingEvents, 1)
	s.selector.Handle(e)
}

func (s *Snapshotter) markSnapshotTime() {
	now := time.Now()
	lst := s.lastSnapshotTime.Load()
	if lst == nil {
		lst = time.Time{}
	}
	lastSnapshotTime := lst.(time.Time)

	pe := atomic.SwapInt64(&s.pendingEvents, 0)

	monitoring.RecordProcessorSnapshotPublished(pe, now.Sub(lastSnapshotTime))
	s.lastSnapshotTime.Store(lastSnapshotTime)
}
