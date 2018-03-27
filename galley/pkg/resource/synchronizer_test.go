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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"istio.io/istio/galley/pkg/testing/dynamic/mock"
	wmock "istio.io/istio/galley/pkg/testing/mock"
)

func TestSynchronizer_NewClientError(t *testing.T) {
	newDynamicClient = func(cfg *rest.Config) (dynamic.Interface, error) {
		return nil, errors.New("newDynamicClient error")
	}

	sgv := schema.GroupVersion{Group: "g1", Version: "v1"}
	dgv := schema.GroupVersion{Group: "g2", Version: "v2"}
	_, err := NewSynchronizer(&rest.Config{}, 0, "foo", sgv, dgv, "kind", "listkind")
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestSynchronizer_NewClientError2(t *testing.T) {
	callid := 0
	m := mock.NewClient()
	newDynamicClient = func(cfg *rest.Config) (dynamic.Interface, error) {
		if callid == 0 {
			callid++
			return m, nil
		}
		return nil, errors.New("newDynamicClient error")
	}

	sgv := schema.GroupVersion{Group: "g1", Version: "v1"}
	dgv := schema.GroupVersion{Group: "g1", Version: "v2"}
	_, err := NewSynchronizer(&rest.Config{}, 0, "foo", sgv, dgv, "kind", "listkind")
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

type testState struct {
	m1           *mock.Client
	m2           *mock.Client
	w1           *wmock.Watch
	w2           *wmock.Watch
	synchronizer *Synchronizer
	eventWG      sync.WaitGroup
}

func newTestState(t *testing.T, initial1, initial2 []unstructured.Unstructured) *testState {
	st := &testState{}

	callid := 0
	st.m1 = mock.NewClient()
	st.w1 = wmock.NewWatch()
	st.m1.MockResource.ListResult = &unstructured.UnstructuredList{Items: initial1}
	st.m1.MockResource.WatchResult = st.w1

	st.m2 = mock.NewClient()
	st.w2 = wmock.NewWatch()
	st.m2.MockResource.ListResult = &unstructured.UnstructuredList{Items: initial2}
	st.m2.MockResource.WatchResult = st.w2

	newDynamicClient = func(cfg *rest.Config) (dynamic.Interface, error) {
		if callid == 0 {
			callid++
			return st.m1, nil
		}
		return st.m2, nil
	}

	hookFn := func(_ interface{}) {
		st.eventWG.Done()
	}

	sgv := schema.GroupVersion{Group: "g1", Version: "v1"}
	dgv := schema.GroupVersion{Group: "g2", Version: "v2"}
	s, err := newSynchronizer(
		&rest.Config{}, 0, "foo", sgv, dgv, "kind", "listkind", hookFn)
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
List
Watch`
	// Both accessors should do the same list/watch operations.
	check(t, st.m1.String(), expected)
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_DoubleStart(t *testing.T) {
	st := newTestState(t, nil, nil)

	// Start again
	st.synchronizer.Start()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	// Both accessors should do the same list/watch operations.
	check(t, st.m1.String(), expected)
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_DoubleStop(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.synchronizer.Stop()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	// Both accessors should do the same list/watch operations.
	check(t, st.m1.String(), expected)
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_SourceAddEvent(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch
Create foo/g2/v2`
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_SourceAddEvent_CreateError(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.m2.MockResource.ErrorResult = errors.New("some create error")

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch
Create foo/g2/v2`
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_DestinationAddEvent(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.eventWG.Add(1)
	st.w2.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch
Delete foo`
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_DestinationAddEvent_DeleteError(t *testing.T) {
	st := newTestState(t, nil, nil)

	st.m2.MockResource.ErrorResult = errors.New("some delete error")

	st.eventWG.Add(1)
	st.w2.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch
Delete foo`
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_InitiallySynced(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})

	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch`
	check(t, st.m2.String(), expected)
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
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch`
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_Update(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})

	t3 := t1.DeepCopy()
	t3.SetResourceVersion("rv3")
	t3.SetLabels(map[string]string{"aa": "bb"})

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Modified, Object: t3})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch
Update foo/g2/v2`
	check(t, st.m2.String(), expected)
}

func TestSynchronizer_Update_Error(t *testing.T) {
	t1 := template.DeepCopy()
	t2 := rewrite(t1, "g2/v2")
	st := newTestState(t, []unstructured.Unstructured{*t1}, []unstructured.Unstructured{*t2})

	t3 := t1.DeepCopy()
	t3.SetResourceVersion("rv3")
	t3.SetLabels(map[string]string{"aa": "bb"})

	st.m2.MockResource.ErrorResult = errors.New("some update error")

	st.eventWG.Add(1)
	st.w1.Send(watch.Event{Type: watch.Modified, Object: t3})
	st.eventWG.Wait()

	st.synchronizer.Stop()

	expected := `
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch
Update foo/g2/v2`
	check(t, st.m2.String(), expected)
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
List
Watch`
	check(t, st.m1.String(), expected)

	expected = `
List
Watch`
	check(t, st.m2.String(), expected)
}
