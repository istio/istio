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

package resource

import (
	"errors"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/testing/mock"
	wmock "istio.io/istio/galley/pkg/testing/mock"
)

func TestSynchronizer_NewClientError(t *testing.T) {
	k := mock.NewKube()
	k.AddResponse(nil, errors.New("newDynamicClient error"))

	sgv := schema.GroupVersion{Group: "g1", Version: "v1"}
	dgv := schema.GroupVersion{Group: "g2", Version: "v2"}
	_, err := NewSynchronizer(k, 0, sgv, dgv, "foo", "kind", "listkind")
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestSynchronizer_NewClientError2(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)
	k.AddResponse(nil, errors.New("newDynamicClient error"))

	sgv := schema.GroupVersion{Group: "g1", Version: "v1"}
	dgv := schema.GroupVersion{Group: "g1", Version: "v2"}
	_, err := NewSynchronizer(k, 0, sgv, dgv, "foo", "kind", "listkind")
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

type testState struct {
	fc1          *fake.FakeClient
	fc2          *fake.FakeClient
	w1           *wmock.Watch
	w2           *wmock.Watch
	synchronizer *Synchronizer
	eventWG      sync.WaitGroup
}

func newTestState(t *testing.T, initial1, initial2 []unstructured.Unstructured) *testState {
	st := &testState{}

	k := mock.NewKube()

	w1 := mock.NewWatch()
	cl1 := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	cl1.AddReactor("list", "foo", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: initial1}, nil
	})
	cl1.AddWatchReactor("foo", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w1, nil
	})

	w2 := mock.NewWatch()
	cl2 := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	cl2.AddReactor("list", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{Items: initial2}, nil
	})
	cl2.AddWatchReactor("foo", func(action dtesting.Action) (bool, watch.Interface, error) {
		return true, w2, nil
	})

	st.fc1 = cl1
	st.w1 = w1

	st.fc2 = cl2
	st.w2 = w2

	k.AddResponse(cl1, nil)
	k.AddResponse(cl2, nil)

	hookFn := func(_ interface{}) {
		st.eventWG.Done()
	}

	sgv := schema.GroupVersion{Group: "g1", Version: "v1"}
	dgv := schema.GroupVersion{Group: "g2", Version: "v2"}
	s, err := newSynchronizer(k, 0, sgv, dgv, "foo", "kind", "listkind", hookFn)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	st.synchronizer = s

	st.eventWG.Add(len(initial1) + len(initial2))
	s.Start()
	st.eventWG.Wait()

	return st
}

func TestSynchronizer_Basic(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	// Both accessors should do the same list/watch operations.
	check(t, writeActions(st.fc1.Fake.Actions()), expected)
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_DoubleStart(t *testing.T) {
	st := newTestState(t, nil, nil)

	// Start again
	st.synchronizer.Start()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	// Both accessors should do the same list/watch operations.
	check(t, writeActions(st.fc1.Fake.Actions()), expected)
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_DoubleStop(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.synchronizer.Stop()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	// Both accessors should do the same list/watch operations.
	check(t, writeActions(st.fc1.Fake.Actions()), expected)
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_SourceAddEvent(t *testing.T) {
	st := newTestState(t, nil, nil)
	st.fc2.AddReactor("create", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, nil
	})
	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
create foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_SourceAddEvent_CreateError(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.fc2.AddReactor("create", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("some create error")
	})

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
create foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_DestinationAddEvent(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.fc2.AddReactor("delete", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil
	})

	st.eventWG.Add(1)
	st.w2.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
delete foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_DestinationAddEvent_DeleteError(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.fc2.AddReactor("delete", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("some delete error")
	})

	st.eventWG.Add(1)
	st.w2.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
delete foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_InitiallySynced(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})

	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_NoSemanticChange(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})

	t3 := t1.DeepCopy()
	t3.SetResourceVersion("rv3")

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Modified, Object: t3})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_Update(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})
	st.fc2.AddReactor("update", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, nil
	})

	t3 := t1.DeepCopy()
	t3.SetResourceVersion("rv3")
	t3.SetLabels(map[string]string{"aa": "bb"})

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Modified, Object: t3})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
update foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_Update_Error(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})
	st.fc2.AddReactor("update", "foo", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("some update error")
	})

	t3 := t1.DeepCopy()
	t3.SetResourceVersion("rv3")
	t3.SetLabels(map[string]string{"aa": "bb"})

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Modified, Object: t3})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
update foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}

func TestSynchronizer_NonChangeEvent(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})

	st.eventWG.Add(1)
	st.synchronizer.queue.Add(struct{}{})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
list foo
watch foo
`
	check(t, writeActions(st.fc1.Fake.Actions()), expected)

	expected = `
list foo
watch foo
`
	check(t, writeActions(st.fc2.Fake.Actions()), expected)
}
