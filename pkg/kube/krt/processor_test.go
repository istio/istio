package krt

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestProcessor(t *testing.T) {
	t.Run("initial sync without initial events", func(t *testing.T) {
		hs := newHandlerSet[Named]()
		tracker := assert.NewTracker[string](t)
		handler := BatchedTrackerHandler[Named](tracker)
		stop := test.NewStop(t)
		reg := hs.Insert(handler, alwaysSynced{}, nil, stop)
		assert.Equal(t, reg.HasSynced(), true)
		tracker.Empty()
	})
	t.Run("initial un-sync without initial events", func(t *testing.T) {
		ready := make(chan struct{})
		sync := channelSyncer{synced: ready}
		hs := newHandlerSet[Named]()
		tracker := assert.NewTracker[string](t)
		handler := BatchedTrackerHandler[Named](tracker)
		stop := test.NewStop(t)
		reg := hs.Insert(handler, sync, nil, stop)
		assert.Equal(t, reg.HasSynced(), false)
		close(ready)
		assert.EventuallyEqual(t, reg.HasSynced, true)
		tracker.Empty()
	})
	t.Run("initial un-sync without initial events then more events", func(t *testing.T) {
		ready := make(chan struct{})
		sync := channelSyncer{synced: ready}
		hs := newHandlerSet[Named]()
		tracker := assert.NewTracker[string](t)
		allowEvent := make(chan struct{})
		handler := BlockingBatchedTrackerHandler[Named](allowEvent, tracker)
		stop := test.NewStop(t)
		reg := hs.Insert(handler, sync, nil, stop)
		assert.Equal(t, reg.HasSynced(), false)

		// Send some events. They are blocked
		hs.Distribute([]Event[Named]{{New: &Named{Name: "a"}}}, true)
		hs.Distribute([]Event[Named]{{New: &Named{Name: "b"}}}, true)
		// Tracker should be empty since they are blocked
		tracker.Empty()

		// Parent ready; we are still not ready!
		close(ready)
		assert.Equal(t, reg.HasSynced(), false)

		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//a")
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//b")
		assert.EventuallyEqual(t, reg.HasSynced, true)
	})
	t.Run("initial un-sync with initial events then more events", func(t *testing.T) {
		ready := make(chan struct{})
		sync := channelSyncer{synced: ready}
		hs := newHandlerSet[Named]()
		tracker := assert.NewTracker[string](t)
		allowEvent := make(chan struct{})
		handler := BlockingBatchedTrackerHandler[Named](allowEvent, tracker)
		stop := test.NewStop(t)
		reg := hs.Insert(handler, sync, []Event[Named]{{New: &Named{Name: "init"}}}, stop)
		assert.Equal(t, reg.HasSynced(), false)

		// Send some events. They are blocked
		hs.Distribute([]Event[Named]{{New: &Named{Name: "a"}}}, true)
		hs.Distribute([]Event[Named]{{New: &Named{Name: "b"}}}, true)
		// Tracker should be empty since they are blocked
		tracker.Empty()

		// Parent ready; we are still not ready!
		close(ready)
		assert.Equal(t, reg.HasSynced(), false)

		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//init")
		assert.Equal(t, reg.HasSynced(), false)
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//a")
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//b")
		assert.EventuallyEqual(t, reg.HasSynced, true)
	})
	t.Run("initial un-sync with initial events then continually more events", func(t *testing.T) {
		ready := make(chan struct{})
		sync := channelSyncer{synced: ready}
		hs := newHandlerSet[Named]()
		tracker := assert.NewTracker[string](t)
		allowEvent := make(chan struct{})
		handler := BlockingBatchedTrackerHandler[Named](allowEvent, tracker)
		stop := test.NewStop(t)
		reg := hs.Insert(handler, sync, []Event[Named]{{New: &Named{Name: "init"}}}, stop)
		assert.Equal(t, reg.HasSynced(), false)

		// Send some events. They are blocked
		hs.Distribute([]Event[Named]{{New: &Named{Name: "a"}}}, true)
		hs.Distribute([]Event[Named]{{New: &Named{Name: "b"}}}, true)
		// Tracker should be empty since they are blocked
		tracker.Empty()

		// Parent ready; we are still not ready!
		close(ready)
		assert.Equal(t, reg.HasSynced(), false)
		hs.Distribute([]Event[Named]{{New: &Named{Name: "after"}}}, false)

		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//init")
		assert.Equal(t, reg.HasSynced(), false)
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//a")
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//b")
		// We should be marked synced now, event though we haven't processed 'after'
		assert.EventuallyEqual(t, reg.HasSynced, true)
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//after")
	})
	t.Run("initial sync with initial events then more events", func(t *testing.T) {
		hs := newHandlerSet[Named]()
		tracker := assert.NewTracker[string](t)
		allowEvent := make(chan struct{})
		handler := BlockingBatchedTrackerHandler[Named](allowEvent, tracker)
		stop := test.NewStop(t)
		reg := hs.Insert(handler, alwaysSynced{}, []Event[Named]{{New: &Named{Name: "init"}}}, stop)
		assert.Equal(t, reg.HasSynced(), false)

		// Send some events. They are blocked
		hs.Distribute([]Event[Named]{{New: &Named{Name: "a"}}}, false)
		hs.Distribute([]Event[Named]{{New: &Named{Name: "b"}}}, false)
		// Tracker should be empty since they are blocked
		tracker.Empty()
		// We are not ready even though parent is synced since we haven't handled
		assert.Equal(t, reg.HasSynced(), false)

		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//init")
		assert.EventuallyEqual(t, reg.HasSynced, true)
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//a")
		allowEvent <- struct{}{}
		tracker.WaitOrdered("add//b")
		assert.EventuallyEqual(t, reg.HasSynced, true)
	})
}

func BlockingBatchedTrackerHandler[T any](allowEvents chan struct{}, tracker *assert.Tracker[string]) func([]Event[T]) {
	return func(o []Event[T]) {
		<-allowEvents
		tracker.Record(slices.Join(",", slices.Map(o, func(o Event[T]) string {
			return fmt.Sprintf("%v/%v", o.Event, GetKey(o.Latest()))
		})...))
	}
}

func BatchedTrackerHandler[T any](tracker *assert.Tracker[string]) func([]Event[T]) {
	return func(o []Event[T]) {
		tracker.Record(slices.Join(",", slices.Map(o, func(o Event[T]) string {
			return fmt.Sprintf("%v/%v", o.Event, GetKey(o.Latest()))
		})...))
	}
}
