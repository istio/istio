package krt

import (
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/slices"
	"k8s.io/client-go/tools/cache"
)

type nestedjoin2[T any] struct {
	*mergejoin2[T]
	collections internalCollection[Collection[T]]
}

var (
	_ internalCollection[any] = &nestedjoin2[any]{}
)

func (j *nestedjoin2[T]) Register(f func(e Event[T])) HandlerRegistration {
	return registerHandlerAsBatched(j, f)
}

func (j *nestedjoin2[T]) RegisterBatch(f func(e []Event[T]), runExistingState bool) HandlerRegistration {
	if !runExistingState {
		// If we don't to run the initial state this is simple, we just register the handler.
		j.mu.Lock()
		defer j.mu.Unlock()
		return j.eventHandlers.Insert(f, j, nil, j.stop)
	}

	// We need to run the initial state, but we don't want to get duplicate events.
	// We should get "ADD initialObject1, ADD initialObjectN, UPDATE someLaterUpdate" without mixing the initial ADDs
	// Create ADDs for the current state of the merge cache
	j.mu.RLock()
	defer j.mu.RUnlock()

	events := make([]Event[T], 0, len(j.outputs))
	for _, o := range j.outputs {
		events = append(events, Event[T]{
			New:   &o,
			Event: controllers.EventAdd,
		})
	}

	// Send out all the initial objects to the handler. We will then unlock the new events so it gets the future updates.
	return j.eventHandlers.Insert(f, j, events, j.stop)
}

// nolint: unused // (not true, its to implement an interface)
func (j *nestedjoin2[T]) dump() CollectionDump {
	innerCols := j.collections.List()
	dumpsByCollectionUID := make(map[string]InputDump, len(innerCols))
	for _, c := range innerCols {
		if c == nil {
			continue
		}
		ic := c.(internalCollection[T])
		icDump := ic.dump()
		dumpsByCollectionUID[GetKey(ic)] = InputDump{
			Outputs:      maps.Keys(icDump.Outputs),
			Dependencies: append(maps.Keys(icDump.Inputs), icDump.InputCollection),
		}
	}
	return CollectionDump{
		Outputs: eraseMap(slices.GroupUnique(j.List(), getTypedKey)),
		Synced:  j.HasSynced(),
		Inputs:  dumpsByCollectionUID,
	}
}

func (j *nestedjoin2[T]) getCollections() []Collection[T] {
	// This is used by the collection lister to get the collections for this join
	// so it can be used in a nested join.
	return j.collections.List()
}

func NewNestedJoinCollection2[T any](collections Collection[Collection[T]], merge func(ts []T) *T, opts ...CollectionOption) Collection[T] {
	o := buildCollectionOptions(opts...)
	if o.name == "" {
		o.name = fmt.Sprintf("NestedJoin[%v]", ptr.TypeName[T]())
	}

	ics := collections.(internalCollection[Collection[T]])
	synced := make(chan struct{})

	j := &nestedjoin2[T]{
		mergejoin2: &mergejoin2[T]{
			id:             nextUID(),
			collectionName: o.name,
			log:            log.WithLabels("owner", o.name),
			outputs:        make(map[Key[T]]T),
			indexes:        make(map[string]joinCollectionIndex2[T]),
			eventHandlers:  newHandlerSet[T](),
			merge:          merge,
			synced:         synced,
			stop:           o.stop,
		},
		collections: ics,
	}

	j.mergejoin2.collections = j
	j.syncer = channelSyncer{
		name:   j.collectionName,
		synced: j.synced,
	}

	maybeRegisterCollectionForDebugging(j, o.debugger)

	// Create our queue. When it syncs (that is, all items that were present when Run() was called), we mark ourselves as synced.
	j.queue = queue.NewWithSync(func() {
		close(j.synced)
		j.log.Infof("%v synced (uid %v)", j.name(), j.uid())
	}, j.collectionName)

	// Subscribe to when collections are added or removed.
	// Don't run existing because we want to ensure the first set
	// of collections passed to us are synced before we mark
	// ourselves as synced. Do this before returning so collections
	// aren't added between now and when runQueue is called.
	subscriptionFunc := func(events []Event[T]) {
		j.queue.Push(func() error {
			j.onSubCollectionEventHandler(events)
			return nil
		})
	}
	reg := j.collections.RegisterBatch(func(o []Event[Collection[T]]) {
		for _, e := range o {
			o := e.Latest()
			switch e.Event {
			case controllers.EventAdd:
				// When a collection is added, subscribe to its events
				o.RegisterBatch(subscriptionFunc, true)
			case controllers.EventUpdate:
				j.handleCollectionUpdate(e)
			case controllers.EventDelete:
				j.handleCollectionDelete(e)
			}
		}
	}, false)
	initialCollections := j.collections.List()
	// Finally, async wait for the primary to be synced. Once it has, we know it has enqueued the initial state.
	// After this, we can run our queue.
	// The queue will process the initial state and mark ourselves as synced (from the NewWithSync callback)
	go j.runQueue(initialCollections, subscriptionFunc, reg)

	return j
}

func (j *nestedjoin2[T]) runQueue(initialCollections []Collection[T], subscriptionFunc func([]Event[T]), reg HandlerRegistration) {
	// Wait for the container of collections to be synced before we start processing events.
	if !j.collections.WaitUntilSynced(j.stop) {
		return
	}

	// TODO: Figure out if we need special handles for nested joins.
	// If not, we don't need to keep track of the registrations.
	var regs []HandlerRegistration
	regs = append(regs, reg)
	// Now that we've subscribed, process the current set of collections.
	for _, c := range initialCollections {
		regs = append(regs, c.RegisterBatch(subscriptionFunc, true))
	}

	// Ensure each sub-collection is synced before we're marked as synced.
	syncers := slices.Map(regs, func(r HandlerRegistration) cache.InformerSynced {
		return r.HasSynced
	})
	if !kube.WaitForCacheSync(j.collectionName, j.stop, syncers...) {
		return
	}
	j.queue.Run(j.stop)
}

func (j *nestedjoin2[T]) handleCollectionUpdate(e Event[Collection[T]]) {

}

func (j *nestedjoin2[T]) handleCollectionDelete(e Event[Collection[T]]) {

}
