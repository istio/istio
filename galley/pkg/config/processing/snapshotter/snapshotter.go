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
	"time"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/runtime/monitoring"
)

// Snapshotter is a processor that handles input events and creates snapshotImpl collections.
type Snapshotter struct {
	accumulators map[collection.Name]*accumulator
	selector     *event.Selector
	xforms       []event.Transformer
	settings     []SnapshotOptions

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
	reqSyncCount int
	syncCount    int
	collection   *collection.Instance
	strategies   []strategy.Instance
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
		scope.Errorf("accumulator.Handle: unhandled event type: %v", e.Kind)
		return
	}

	if a.syncCount >= a.reqSyncCount {
		for _, s := range a.strategies {
			s.OnChange()
		}
	}
}

func (a *accumulator) reset() {
	a.syncCount = 0
	a.collection.Clear()
}

// NewSnapshotter returns a new Snapshotter.
func NewSnapshotter(xforms []event.Transformer, settings []SnapshotOptions) *Snapshotter {
	s := &Snapshotter{
		accumulators:  make(map[collection.Name]*accumulator),
		selector:      event.NewSelector(),
		xforms:        xforms,
		settings:      settings,
		lastEventTime: time.Now(),
	}

	for _, xform := range xforms {
		for _, i := range xform.Inputs() {
			s.selector.Select(i, xform)
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
			xform.Select(o, a)
		}
	}

	for _, o := range settings {
		for _, c := range o.Collections {
			a := s.accumulators[c]
			if a == nil {
				scope.Errorf("NewSnapshotter: Unrecognized collection in SnapshotOptions: %v (Group: %s)", c, o.Group)
				continue
			}

			a.strategies = append(a.strategies, o.Strategy)
		}
	}

	return s
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
	sn := &snapshotImpl{set: set}

	now := time.Now()
	monitoring.RecordProcessorSnapshotPublished(s.pendingEvents, now.Sub(s.lastSnapshotTime))
	s.lastSnapshotTime = now
	s.pendingEvents = 0
	o.Distributor.SetSnapshot(o.Group, sn)
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
}

// Handle implements Processor
func (s *Snapshotter) Handle(e event.Event) {
	now := time.Now()
	monitoring.RecordProcessorEventProcessed(now.Sub(s.lastEventTime))
	s.lastEventTime = now
	s.pendingEvents++
	s.selector.Handle(e)
}
