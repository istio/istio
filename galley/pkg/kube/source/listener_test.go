// Copyright 2018 Istio Authors
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

package source

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/log"

	"istio.io/istio/galley/pkg/kube"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/common"
	"istio.io/istio/galley/pkg/testing/mock"
)

var info = kube.ResourceSpec{
	Kind:     "kind",
	ListKind: "listkind",
	Group:    "group",
	Version:  "version",
	Singular: "singular",
	Plural:   "plural",
}

var template = &unstructured.Unstructured{
	Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":            "foo",
			"resourceVersion": "rv",
		},
	},
}

func TestListener_NewClientError(t *testing.T) {
	k := &mock.Kube{}
	k.AddResponse(nil, errors.New("newDynamicClient error"))

	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {}

	_, err := newListener(k, 0, info, processorFn)
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListener_NewClient_Debug(t *testing.T) {
	k := &mock.Kube{}
	k.AddResponse(nil, errors.New("newDynamicClient error"))

	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {}

	old := scope.GetOutputLevel()
	defer scope.SetOutputLevel(old)
	scope.SetOutputLevel(log.DebugLevel)
	_, _ = newListener(k, 0, info, processorFn)
	// should not crash
}

func TestListener_Basic(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v %v", eventKind, key)
	}

	cl.AddReactor("*", "foo", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}, nil
	})
	cl.AddWatchReactor("foo", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()
	a.start()
	a.waitForCacheSync()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)
	check(t, processorLog.String(), "")
}

func TestListener_DoubleStart(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v %v", eventKind, key)
	}

	cl.AddReactor("*", "foo", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}, nil
	})
	cl.AddWatchReactor("foo", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.start()
	a.waitForCacheSync()
	a.start()
	a.waitForCacheSync()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)
	check(t, processorLog.String(), "")
}

func TestListener_DoubleStop(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v %v", eventKind, key)
	}

	cl.AddReactor("*", "foo", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}, nil
	})
	cl.AddWatchReactor("foo", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.start()
	a.waitForCacheSync()
	a.stop()
	a.stop()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)
	check(t, processorLog.String(), "")
}

func TestListener_AddEvent(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "foo", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.waitForCacheSync()

	w.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo`

	check(t, processorLog.String(), expected)
}

func TestListener_UpdateEvent(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(2) // One for initial add, one for update
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "plural", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*template.DeepCopy()}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.waitForCacheSync()

	t2 := template.DeepCopy()
	t2.SetResourceVersion("rv2")
	w.Send(watch.Event{Type: watch.Modified, Object: t2})
	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo
Updated key=foo`

	check(t, processorLog.String(), expected)
}

func TestListener_UpdateEvent_SameResourceVersion(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1) // One for initial add only
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "plural", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*template.DeepCopy()}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.waitForCacheSync()

	t2 := template.DeepCopy()
	w.Send(watch.Event{Type: watch.Modified, Object: t2})
	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo
`
	check(t, processorLog.String(), expected)
}

func TestListener_DeleteEvent(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(2) // One for initial add, one for delete
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "plural", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*template.DeepCopy()}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.waitForCacheSync()

	t2 := template.DeepCopy()
	t2.SetResourceVersion("rv2")
	w.Send(watch.Event{Type: watch.Deleted, Object: t2})
	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo
Deleted key=foo`

	check(t, processorLog.String(), expected)
}

func TestListener_Tombstone(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(2) // One for initial add, one for delete
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "plural", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*template.DeepCopy()}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.waitForCacheSync()

	t2 := template.DeepCopy()
	item := cache.DeletedFinalStateUnknown{Key: "foo", Obj: t2}
	a.handleEvent(resource.Deleted, item)

	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo
Deleted key=foo`

	check(t, processorLog.String(), expected)
}

func TestListener_TombstoneDecodeError(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1) // One for initial add only
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "plural", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*template.DeepCopy()}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.waitForCacheSync()

	a.handleEvent(resource.Deleted, struct{}{})

	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo
`
	check(t, processorLog.String(), expected)
}

func TestListener_Tombstone_ObjDecodeError(t *testing.T) {
	k := mock.NewKube()
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1) // One for initial add only
	processorFn := func(l *listener, eventKind resource.EventKind, key, version string, u *unstructured.Unstructured) {
		processorLog.Append("%v key=%v", eventKind, key)
		wg.Done()
	}

	cl.AddReactor("*", "plural", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*template.DeepCopy()}}, nil
	})
	w := mock.NewWatch()
	cl.AddWatchReactor("plural", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	a, err := newListener(k, 0, info, processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	old := scope.GetOutputLevel()
	defer scope.SetOutputLevel(old)
	scope.SetOutputLevel(log.DebugLevel)

	item := cache.DeletedFinalStateUnknown{Key: "foo", Obj: struct{}{}}
	a.handleEvent(resource.Deleted, item)

	wg.Wait()

	expected := `
list plural
watch plural
`
	check(t, writeActions(cl.Fake.Actions()), expected)

	expected = `
Added key=foo
`
	check(t, processorLog.String(), expected)
}

func writeActions(actions []dtesting.Action) string {
	result := ""
	for _, a := range actions {
		result += fmt.Sprintf("%s %s\n", a.GetVerb(), a.GetResource().Resource)
	}
	return result
}

func check(t *testing.T, actual string, expected string) {
	t.Helper()
	if strings.TrimSpace(actual) != strings.TrimSpace(expected) {
		t.Fatalf("mismatch.\nGot:\n%s\nWanted:\n%s\n", actual, expected)
	}
}
