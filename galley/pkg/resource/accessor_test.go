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
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/testing/machinery/mock"

	"istio.io/istio/galley/pkg/change"
	"istio.io/istio/galley/pkg/testing/common"
	wmock "istio.io/istio/galley/pkg/testing/crd/mock"
)

func TestAccessor_NewClientError(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorFn := func(c *change.Info) {}

	i := mock.NewInterface()
	i.DynamicFn = func(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error) {
		return nil, errors.New("newDynamicClient error")
	}
	_, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAccessor_Basic(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	processorFn := func(c *change.Info) { processorLog.Append("%v", c) }

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()
	a.start()

	expected := `
List
Watch`
	check(t, i.MockDynamic.String(), expected)

	check(t, processorLog.String(), "")
}

func TestAccessor_DoubleStart(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	processorFn := func(c *change.Info) { processorLog.Append("%v", c) }

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()
	a.start()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)
	check(t, processorLog.String(), "")
}

func TestAccessor_DoubleStop(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	processorFn := func(c *change.Info) { processorLog.Append("%v", c) }

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.start()
	a.stop()
	a.stop()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)
	check(t, processorLog.String(), "")
}

func TestAccessor_AddEvent(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	w.Send(watch.Event{Type: watch.Added, Object: template.DeepCopy()})
	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]`

	check(t, processorLog.String(), expected)
}

func TestAccessor_UpdateEvent(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(2) // One for initial add, one for update
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*template.DeepCopy(),
	}}

	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	t2 := template.DeepCopy()
	t2.SetResourceVersion("rv2")
	w.Send(watch.Event{Type: watch.Modified, Object: t2})
	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]
Info[Type:Update, Name:foo, GroupVersion:group/version]`

	check(t, processorLog.String(), expected)
}

func TestAccessor_UpdateEvent_SameResourceVersion(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1) // One for initial add only
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*template.DeepCopy(),
	}}

	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	t2 := template.DeepCopy()
	w.Send(watch.Event{Type: watch.Modified, Object: t2})
	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]
`
	check(t, processorLog.String(), expected)
}

func TestAccessor_DeleteEvent(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(2) // One for initial add, one for delete
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*template.DeepCopy(),
	}}

	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	t2 := template.DeepCopy()
	t2.SetResourceVersion("rv2")
	w.Send(watch.Event{Type: watch.Deleted, Object: t2})
	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]
Info[Type:Delete, Name:foo, GroupVersion:group/version]`
	check(t, processorLog.String(), expected)
}

func TestAccessor_Tombstone(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(2) // One for initial add, one for delete
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*template.DeepCopy(),
	}}

	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	t2 := template.DeepCopy()
	item := cache.DeletedFinalStateUnknown{Key: "foo", Obj: t2}
	a.handleEvent(change.Delete, item)

	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]
Info[Type:Delete, Name:foo, GroupVersion:group/version]`
	check(t, processorLog.String(), expected)
}

func TestAccessor_TombstoneDecodeError(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1) // One for initial add only
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*template.DeepCopy(),
	}}

	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	a.handleEvent(change.Delete, struct{}{})

	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]
`
	check(t, processorLog.String(), expected)
}

func TestAccessor_Tombstone_ObjDecodeError(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	processorLog := &common.MockLog{}
	wg := &sync.WaitGroup{}
	wg.Add(1) // One for initial add only
	processorFn := func(c *change.Info) {
		processorLog.Append("%v", c)
		wg.Done()
	}

	i := mock.NewInterface()
	w := wmock.NewWatch()
	i.MockDynamic.MockResource.ListResult = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*template.DeepCopy(),
	}}

	i.MockDynamic.MockResource.WatchResult = w
	a, err := newAccessor(i, 0, "foo", gv, "kind", "listkind", processorFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer a.stop()

	a.start()

	item := cache.DeletedFinalStateUnknown{Key: "foo", Obj: struct{}{}}
	a.handleEvent(change.Delete, item)

	wg.Wait()

	expected := `
List
Watch`

	check(t, i.MockDynamic.String(), expected)

	expected = `
Info[Type:Add, Name:foo, GroupVersion:group/version]
`
	check(t, processorLog.String(), expected)
}

var template = &unstructured.Unstructured{
	Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":            "foo",
			"resourceVersion": "rv",
		},
	},
}
