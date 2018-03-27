//  Copyright 2018 Istio Authors
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

package crd

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/galley/pkg/testing/mock"

	"k8s.io/client-go/rest"
)

func TestSynchronizer_ClientSetError(t *testing.T) {
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return nil, errors.New("newForConfig error")
	}

	_, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err == nil || err.Error() != "newForConfig error" {
		t.Fatalf("Expected error not found: %v", err)
	}
}

func TestSynchronizer_DoubleStart(t *testing.T) {
	iface := mock.NewInterface()
	defer iface.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return iface, nil
	}

	iface.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	w := mock.NewWatch()
	iface.AddWatchResponse(w, nil)

	synchronizer, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = synchronizer.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = synchronizer.Start()
	if err.Error() != "crd.Synchronizer: already started" {
		t.Fatalf("Unexpected error: %v", err)
	}

	synchronizer.Stop()
}

func TestSynchronizer_DoubleStop(t *testing.T) {
	iface := mock.NewInterface()
	defer iface.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return iface, nil
	}

	iface.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	w := mock.NewWatch()
	iface.AddWatchResponse(w, nil)

	synchronizer, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = synchronizer.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	synchronizer.Stop()
	synchronizer.Stop()
}

func TestSynchronizer_BasicCreate(t *testing.T) {
	st := newTestState(t)
	defer st.Close()

	st.iface.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			*i1Template.DeepCopy(),
		},
	}, nil)
	st.eventSync.Inc()

	st.iface.AddWatchResponse(st.watch, nil)
	st.iface.AddCreateResponse(nil, nil)

	st.Start(t)
	st.eventSync.wait()

	expected := `
LIST
WATCH
CREATE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)
}

func TestSynchronizer_BasicUpdate(t *testing.T) {
	st := newTestState(t)
	defer st.Close()

	i2 := i2Template.DeepCopy() // The already-existing, but not matching target copy.
	i2.Labels = map[string]string{"foo": "bar"}

	st.iface.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			*i1Template.DeepCopy(),
			*i2,
		},
	}, nil)
	st.eventSync.Inc() // for i1 add
	st.eventSync.Inc() // for i2 add

	st.iface.AddWatchResponse(st.watch, nil)
	st.iface.AddUpdateResponse(nil, nil)
	st.iface.AddUpdateResponse(nil, nil)

	st.Start(t)
	st.eventSync.wait()

	// The second update is due to handling two different add events, one for each existing crd.
	expected := `
LIST
WATCH
UPDATE: foos.g2
UPDATE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)
}

func TestSynchronizer_BasicDelete(t *testing.T) {
	st := newTestState(t)
	defer st.Close()

	st.iface.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			// Instance at destination with no source.
			*i2Template.DeepCopy(),
		},
	}, nil)
	st.eventSync.Inc() // for i2 add

	st.iface.AddWatchResponse(st.watch, nil)
	st.iface.AddDeleteResponse(nil)

	st.Start(t)
	st.eventSync.wait()

	expected := `
LIST
WATCH
DELETE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)
}

func setInitialDataAndStart(t *testing.T, st *testState) {
	st.iface.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			*i1Template.DeepCopy(),
			*i2Template.DeepCopy(),
		},
	}, nil)
	st.eventSync.Inc()
	st.eventSync.Inc()

	st.iface.AddWatchResponse(st.watch, nil)
	st.Start(t)
	st.eventSync.wait()
}

func TestSynchronizer_BasicAlreadySynced(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicCreateEvent(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	// Now, send an update event
	i3 := i1Template.DeepCopy()
	i3.Name = "i3.g1"
	i3.ResourceVersion = "rv3"
	i3.Labels = map[string]string{"foo": "bar"}

	st.iface.AddCreateResponse(nil, nil)

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Added, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
CREATE: i3.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
ADD: i3.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicCreateEvent_Error(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	// Now, send an update event
	i3 := i1Template.DeepCopy()
	i3.Name = "i3.g1"
	i3.ResourceVersion = "rv3"
	i3.Labels = map[string]string{"foo": "bar"}

	st.iface.AddCreateResponse(nil, errors.New("some create error"))

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Added, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
CREATE: i3.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicCreateEvent_Unrelated(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	// Now, send an update event
	i3 := rewrite(i1Template.DeepCopy(), "someunrelatedgroup", "v1")

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Added, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicUpdateEvent(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	i3 := i1Template.DeepCopy()
	i3.ResourceVersion = "rv2"
	i3.Labels = map[string]string{"foo": "bar"}

	st.iface.AddUpdateResponse(nil, nil)

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Modified, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
UPDATE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicUpdateEvent_Error(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	i3 := i1Template.DeepCopy()
	i3.ResourceVersion = "rv2"
	i3.Labels = map[string]string{"foo": "bar"}

	st.iface.AddUpdateResponse(nil, errors.New("some update error"))

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Modified, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
UPDATE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicUpdateEvent_SameVersion(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	i3 := i1Template.DeepCopy()
	// No change

	st.watch.Send(watch.Event{Type: watch.Modified, Object: i3})

	// We expect the system to not do anything in an asynchronous. This is bad, but there is
	// really no good way to exercise some paths of code without doing invasive test hooks.
	time.Sleep(time.Second)

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicDeleteEvent(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	i3 := i1Template.DeepCopy()
	i3.ResourceVersion = "rv2"
	i3.Labels = map[string]string{"foo": "bar"}

	st.iface.AddDeleteResponse(nil)

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Deleted, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
DELETE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
REMOVE: foos.g1
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_BasicDeleteEvent_Error(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	i3 := i1Template.DeepCopy()
	i3.ResourceVersion = "rv2"
	i3.Labels = map[string]string{"foo": "bar"}

	st.iface.AddDeleteResponse(errors.New("some delete error"))

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Deleted, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
DELETE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_TombstoneEvent(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	tombstone := cache.DeletedFinalStateUnknown{
		Obj: i1Template.DeepCopy(),
	}

	st.eventSync.Inc()
	st.synchronizer.handleEvent(change.Delete, tombstone)
	st.eventSync.wait()

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
REMOVE: foos.g1
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_TombstoneEvent_Error(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	tombstone := cache.DeletedFinalStateUnknown{
		Obj: struct{}{},
	}

	st.synchronizer.handleEvent(change.Delete, tombstone)

	if st.synchronizer.queue.Len() != 0 {
		t.Fatalf("Unexpected event detected in the queue.")
	}

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_HandleEvent_Error(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	st.synchronizer.handleEvent(change.Delete, struct{}{})

	if st.synchronizer.queue.Len() != 0 {
		t.Fatalf("Unexpected event detected in the queue.")
	}

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_PanicInProcessorQueue(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	i3 := i1Template.DeepCopy()
	i3.ResourceVersion = "rv2"
	i3.Labels = map[string]string{"foo": "bar"}

	// First prime the interface to response with panic for the next call.
	st.iface.AddPanicResponse("purple tigers!")

	st.eventSync.Inc()
	st.watch.Send(watch.Event{Type: watch.Deleted, Object: i3})
	st.eventSync.wait()

	expected := `
LIST
WATCH
DELETE: foos.g2
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

func TestSynchronizer_NonChangeItem(t *testing.T) {
	st := newTestState(t)
	defer st.Close()
	setInitialDataAndStart(t, st)

	st.eventSync.Inc()
	st.synchronizer.queue.Add(struct{}{}) // non-change.Info item
	st.eventSync.wait()

	expected := `
LIST
WATCH
`
	check(t, "Iface.Log", st.iface.String(), expected)

	expected = `
ADD: foos.g1, foo, foolist
`
	check(t, "Listener.Log", st.listener.String(), expected)
}

type testState struct {
	iface        *mock.Interface
	watch        *mock.Watch
	synchronizer *Synchronizer
	eventSync    *eventSynchronizer
	listener     *loggingListener
}

var i1Template = apiext.CustomResourceDefinition{
	TypeMeta:   v1.TypeMeta{Kind: "CustomResourceDefinitions"},
	ObjectMeta: v1.ObjectMeta{Name: "foos.g1", ResourceVersion: "v1"},
	Spec: apiext.CustomResourceDefinitionSpec{
		Group:   "g1",
		Version: "v1",
		Names: apiext.CustomResourceDefinitionNames{
			Kind:     "foo",
			ListKind: "foolist",
		},
	},
}

var i2Template = rewrite(&i1Template, "g2", "v2")

func newTestState(t *testing.T) *testState {
	i := mock.NewInterface()
	state := &testState{
		iface:     i,
		watch:     mock.NewWatch(),
		eventSync: &eventSynchronizer{},
		listener:  newLoggingListener(),
	}

	crdiFn := func(config *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	var err error
	if state.synchronizer, err = newSynchronizer(
		&rest.Config{}, getMappingForSynchronizerTests(), 0,
		state.listener.listener, state.eventSync.hookFn, crdiFn); err != nil {

		t.Fatalf("Unexpected error during newSynchronizer call: %v", err)
	}

	return state
}

func (s *testState) Start(t *testing.T) {
	if err := s.synchronizer.Start(); err != nil {
		t.Fatalf("Unexpected error during start: %v", err)
	}
}

func (s *testState) Close() {
	s.synchronizer.Stop()
	s.iface.Close()
}

func getMappingForSynchronizerTests() Mapping {
	if m, err := NewMapping(map[schema.GroupVersion]schema.GroupVersion{
		{
			Group:   "g1",
			Version: "v1",
		}: {
			Group:   "g2",
			Version: "v2",
		},
	}); err != nil {
		panic(err)
	} else {
		return m
	}
}

type loggingListener struct {
	log      string
	lock     sync.Mutex
	listener SyncListener
}

func newLoggingListener() *loggingListener {
	l := &loggingListener{}

	l.listener = SyncListener{
		OnDestinationAdded:   l.onAdd,
		OnDestinationRemoved: l.onRemove,
	}

	return l
}

func (l *loggingListener) onAdd(name string, kind string, listKind string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.log += fmt.Sprintf("ADD: %s, %s, %s\n", name, kind, listKind)
}

func (l *loggingListener) onRemove(name string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.log += fmt.Sprintf("REMOVE: %s\n", name)
}

func (l *loggingListener) String() string {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.log
}

type eventSynchronizer struct {
	wg sync.WaitGroup
}

func (e *eventSynchronizer) Inc() {
	e.wg.Add(1)
}

func (e *eventSynchronizer) hookFn(_ interface{}) {
	e.wg.Done()
}

func (e *eventSynchronizer) wait() {
	e.wg.Wait()
}

func check(t *testing.T, context string, actual string, expected string) {
	if strings.TrimSpace(actual) != strings.TrimSpace(expected) {
		t.Fatalf("Mismatch[%synchronizer]: got:\n%v\nwanted:\n%v\n", context, actual, expected)
	}
}
