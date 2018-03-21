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

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

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

func TestSynchronizer_BasicCreate(t *testing.T) {
	i := mock.NewInterface()
	defer i.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	i1 := apiext.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{Name: "foos.g1", ResourceVersion: "v1"},
		Spec: apiext.CustomResourceDefinitionSpec{
			Group:   "g1",
			Version: "v1",
		},
	}

	i.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{i1},
	}, nil)

	w := mock.NewWatch()
	i.AddWatchResponse(w, nil)

	i.AddCreateResponse(nil, nil)

	s, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	i.WaitForResponseDelivery()
	s.waitForEventQueueDrain()

	expected := `
LIST
WATCH
CREATE: foos.g2
`

	if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
		t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
	}

	// Make sure this doesn't block or panic
	s.Stop()
}

func TestSynchronizer_BasicUpdate(t *testing.T) {
	i := mock.NewInterface()
	defer i.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	i1 := apiext.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{Name: "foos.g1", ResourceVersion: "v1"},
		Spec: apiext.CustomResourceDefinitionSpec{
			Group:   "g1",
			Version: "v1",
		},
	}
	// The already-existing target copy.
	i2 := apiext.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{Name: "foos.g2", ResourceVersion: "v2"},
		Spec: apiext.CustomResourceDefinitionSpec{
			Group:   "g2",
			Version: "v2",
		},
	}

	i.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{i1, i2},
	}, nil)

	w := mock.NewWatch()
	i.AddWatchResponse(w, nil)

	i.AddUpdateResponse(nil, nil)
	i.AddUpdateResponse(nil, nil)

	s, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	i.WaitForResponseDelivery()
	s.waitForEventQueueDrain()

	// The second update is due to handling two different add events, one for each existing crd.
	expected := `
LIST
WATCH
UPDATE: foos.g2
UPDATE: foos.g2
`

	if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
		t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
	}

	// Make sure this doesn't block or panic
	s.Stop()
}

func TestSynchronizer_BasicDelete(t *testing.T) {
	i := mock.NewInterface()
	defer i.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	// Instance at destination with no source.
	i1 := apiext.CustomResourceDefinition{
		ObjectMeta: v1.ObjectMeta{Name: "foos.g2", ResourceVersion: "v2"},
		Spec: apiext.CustomResourceDefinitionSpec{
			Group:   "g2",
			Version: "v2",
		},
	}

	i.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{i1},
	}, nil)

	w := mock.NewWatch()
	i.AddWatchResponse(w, nil)

	i.AddDeleteResponse(nil)

	s, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	i.WaitForResponseDelivery()
	s.waitForEventQueueDrain()

	expected := `
LIST
WATCH
DELETE: foos.g2
`

	if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
		t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
	}

	s.Stop()
}

func runEventTest(t *testing.T, listener SyncListener, logicFn func(*mock.Interface, *mock.Watch, *Synchronizer, apiext.CustomResourceDefinition)) {
	i := mock.NewInterface()
	defer i.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	i1 := apiext.CustomResourceDefinition{
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
	i2 := rewrite(&i1, "g2", "v2")

	i.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{i1, *i2},
	}, nil)

	w := mock.NewWatch()
	i.AddWatchResponse(w, nil)

	s, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, listener)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	i.WaitForResponseDelivery()
	s.waitForEventQueueDrain()

	logicFn(i, w, s, i1)

	s.Stop()
}

func TestSynchronizer_BasicAlreadySynced(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		expected := `
LIST
WATCH
`
		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	e := `
ADD: foos.g1, foo, foolist
`

	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicCreateEvent(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.Name = "i3.g1"
		i3.ResourceVersion = "rv3"
		i3.Labels = map[string]string{"foo": "bar"}
		w.Send(watch.Event{Type: watch.Added, Object: i3})

		i.AddCreateResponse(nil, nil)
		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
CREATE: i3.g2
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	e := `
ADD: foos.g1, foo, foolist
ADD: i3.g1, foo, foolist`

	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicCreateEvent_Error(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.Name = "i3.g1"
		i3.ResourceVersion = "rv3"
		i3.Labels = map[string]string{"foo": "bar"}
		w.Send(watch.Event{Type: watch.Added, Object: i3})

		i.AddCreateResponse(nil, errors.New("some create error"))
		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
CREATE: i3.g2
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	// We should not receive an event for the erroneous create.
	e := `
ADD: foos.g1, foo, foolist
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicCreateEvent_Unrelated(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.Name = "i3.someunrelatedapigroup"
		i3.Spec.Group = "someunrelatedapigroup"
		i3.ResourceVersion = "rv3"
		w.Send(watch.Event{Type: watch.Added, Object: i3})

		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	// We should not receive an event for the unrelated create.
	e := `
ADD: foos.g1, foo, foolist
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicUpdateEvent(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.ResourceVersion = "rv2"
		i3.Labels = map[string]string{"foo": "bar"}
		w.Send(watch.Event{Type: watch.Modified, Object: i3})

		i.AddUpdateResponse(nil, nil)
		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
UPDATE: foos.g2
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	e := `
ADD: foos.g1, foo, foolist
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicUpdateEvent_Error(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.ResourceVersion = "rv2"
		i3.Labels = map[string]string{"foo": "bar"}
		w.Send(watch.Event{Type: watch.Modified, Object: i3})

		i.AddUpdateResponse(nil, errors.New("some update error"))
		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
UPDATE: foos.g2
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	e := `
ADD: foos.g1, foo, foolist
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicUpdateEvent_SameVersion(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		i3 := i1.DeepCopy()
		w.Send(watch.Event{Type: watch.Modified, Object: i3})

		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	e := `
ADD: foos.g1, foo, foolist
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicDeleteEvent(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.ResourceVersion = "rv2"
		i3.Labels = map[string]string{"foo": "bar"}
		w.Send(watch.Event{Type: watch.Deleted, Object: i3})

		i.AddDeleteResponse(nil)
		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
DELETE: foos.g2
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	e := `
ADD: foos.g1, foo, foolist
REMOVE: foos.g1
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_BasicDeleteEvent_Error(t *testing.T) {
	l := newLoggingListener()
	runEventTest(t, l.listener, func(i *mock.Interface, w *mock.Watch, s *Synchronizer, i1 apiext.CustomResourceDefinition) {
		// Now, send an update event
		// Now, send an update event
		i3 := i1.DeepCopy()
		i3.ResourceVersion = "rv2"
		i3.Labels = map[string]string{"foo": "bar"}
		w.Send(watch.Event{Type: watch.Deleted, Object: i3})

		i.AddDeleteResponse(errors.New("some delete error"))
		i.WaitForResponseDelivery() // wait for the event to be picked up
		s.waitForEventQueueDrain()

		expected := `
LIST
WATCH
DELETE: foos.g2
`

		if strings.TrimSpace(expected) != strings.TrimSpace(i.String()) {
			t.Fatalf("log mismatch: got:\n%v\nwanted:\n%v\n", i.String(), expected)
		}
	})

	// We should not be receiving an event for the erroneous delete event.
	e := `
ADD: foos.g1, foo, foolist
`
	if strings.TrimSpace(e) != strings.TrimSpace(l.String()) {
		t.Fatalf("listener log mismatch: got:\n%v\nwanted:\n%v\n", l.String(), e)
	}
}

func TestSynchronizer_DoubleStart(t *testing.T) {
	i := mock.NewInterface()
	defer i.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	i.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	w := mock.NewWatch()
	i.AddWatchResponse(w, nil)

	s, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err.Error() != "crd.Synchronizer: already started" {
		t.Fatalf("Unexpected error: %v", err)
	}

	s.Stop()
}

func TestSynchronizer_DoubleStop(t *testing.T) {
	i := mock.NewInterface()
	defer i.Close()
	getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
		return i, nil
	}

	i.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	w := mock.NewWatch()
	i.AddWatchResponse(w, nil)

	s, err := NewSynchronizer(&rest.Config{}, getMappingForSynchronizerTests(), 0, SyncListener{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	s.Stop()
	s.Stop()
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
