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

package snapshotter

import (
	"fmt"
	"time"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/runtime/monitoring"
)

// Snapshotter is a processor that handles input events and creates snapshotImpl collections.
type Snapshotter struct {
	accumulators   map[collection.Name]*accumulator
	selector       event.Router
	xforms         []event.Transformer
	settings       []SnapshotOptions
	snapshotGroups []*snapshotGroup

	// lastEventTime records the last time an event was received.
	lastEventTime time.Time

	// pendingEvents counts the number of events awaiting publishing.
	pendingEvents int64

	// lastSnapshotTime records the last time a snapshotImpl was published.
	lastSnapshotTime time.Time
}

var _ event.Processor = &Snapshotter{}

// HandlerFn handles generated snapshots
type HandlerFn func(*collection.Set)

type accumulator struct {
	reqSyncCount   int
	syncCount      int
	collection     *collection.Instance
	snapshotGroups []*snapshotGroup
}

// snapshotGroup represents a group of collections that all need to be synced before any of them publish events,
// and also share a set of strategies.
// Members of a snapshot group are defined via the per-collection accumulators that point to a group.
type snapshotGroup struct {
	// How many collections in the current group still need to receive a FullSync before we start publishing.
	remaining int
	// Set of collections that have already received a FullSync. If we get a duplicate, it will be ignored.
	synced map[*collection.Instance]bool
	// Strategy to execute on handled events only after all collections in the group have been synced.
	strategy strategy.Instance
}

// Handle implements event.Handler
func (a *accumulator) Handle(e event.Event) {
	switch e.Kind {
	case event.Added, event.Updated:
		a.collection.Set(e.Entry)
		monitoring.RecordStateTypeCount(e.Source.String(), a.collection.Size())
	case event.Deleted:
		a.collection.Remove(e.Entry.Metadata.Name)
		monitoring.RecordStateTypeCount(e.Source.String(), a.collection.Size())
	case event.FullSync:
		a.syncCount++
	default:
		panic(fmt.Errorf("accumulator.Handle: unhandled event type: %v", e.Kind))
	}

	// Update the group sync counter if we received all required FullSync events for a collection
	for _, sg := range a.snapshotGroups {
		if a.syncCount >= a.reqSyncCount {
			sg.onSync(a.collection)
		}
	}
}

func (a *accumulator) reset() {
	a.syncCount = 0
	a.collection.Clear()
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
		for _, i := range xform.Inputs() {
			s.selector = event.AddToRouter(s.selector, i, xform)
		}

		for _, o := range xform.Outputs() {
			a, found := s.accumulators[o]
			if !found {
				a = &accumulator{
					collection: collection.New(o),
				}
				s.accumulators[o] = a
			}
			a.reqSyncCount++
			xform.DispatchFor(o, a)
		}
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

func (sg *snapshotGroup) onSync(c *collection.Instance) {
	if !sg.synced[c] {
		sg.remaining--
		sg.synced[c] = true
	}

	// proceed with triggering the strategy OnChange only after we've full synced every collection in a group.
	if sg.remaining == 0 {
		sg.strategy.OnChange()
	}
}

func (sg *snapshotGroup) reset(size int) {
	sg.remaining = size
	sg.synced = make(map[*collection.Instance]bool)
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
	var collections []*collection.Instance

	for _, n := range o.Collections {
		col := s.accumulators[n].collection.Clone()
		collections = append(collections, col)
	}

	set := collection.NewSetFromCollections(collections)
	sn := &Snapshot{set: set}

	now := time.Now()
	monitoring.RecordProcessorSnapshotPublished(s.pendingEvents, now.Sub(s.lastSnapshotTime))
	s.lastSnapshotTime = now
	s.pendingEvents = 0
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
	s.pendingEvents++
	s.selector.Handle(e)
}
