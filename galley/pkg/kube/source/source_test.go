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
	"testing"

	"github.com/gogo/protobuf/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/converter"
	"istio.io/istio/galley/pkg/meshconfig"
	kube_meta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/mock"
)

var emptyInfo resource.Info

func init() {
	b := resource.NewSchemaBuilder()
	b.Register("empty", "type.googleapis.com/google.protobuf.Empty")
	schema := b.Build()
	emptyInfo, _ = schema.Lookup("empty")
}

func schemaWithSpecs(specs []kube.ResourceSpec) *kube.Schema {
	sb := kube.NewSchemaBuilder()
	for _, s := range specs {
		sb.Add(s)
	}
	return sb.Build()
}

func TestNewSource(t *testing.T) {
	k := &mock.Kube{}
	for i := 0; i < 100; i++ {
		cl := fake.NewSimpleDynamicClient(runtime.NewScheme())
		k.AddResponse(cl, nil)
	}

	cfg := converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
	p, err := New(k, 0, kube_meta.Types, &cfg)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if p == nil {
		t.Fatal("expected non-nil source")
	}
}

func TestNewSource_Error(t *testing.T) {
	k := &mock.Kube{}
	k.AddResponse(nil, errors.New("newDynamicClient error"))

	cfg := converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
	_, err := New(k, 0, kube_meta.Types, &cfg)
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Expected error not found: %v", err)
	}
}

func TestSource_BasicEvents(t *testing.T) {
	k := &mock.Kube{}
	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())

	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "List",
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{},
		},
	}

	cl.PrependReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	w := mock.NewWatch()
	cl.PrependWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	schema := schemaWithSpecs([]kube.ResourceSpec{
		{
			Kind:      "List",
			Singular:  "List",
			Plural:    "foos",
			Target:    emptyInfo,
			Converter: converter.Get("identity"),
		},
	})

	cfg := converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
	s, err := New(k, 0, schema, &cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 2)
	expected := strings.TrimSpace(`
[Event](Added: [VKey](empty:ns/f1 @rv1))
[Event](FullSync)
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	w.Send(watch.Event{Type: watch.Deleted, Object: &i1})
	log = logChannelOutput(ch, 1)
	expected = strings.TrimSpace(`
[Event](Deleted: [VKey](empty:ns/f1 @rv1))
		`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_BasicEvents_NoConversion(t *testing.T) {

	k := &mock.Kube{}
	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())

	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "List",
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{},
		},
	}

	cl.PrependReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.PrependWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	schema := schemaWithSpecs([]kube.ResourceSpec{
		{
			Kind:      "List",
			Singular:  "List",
			Plural:    "foos",
			Target:    emptyInfo,
			Converter: converter.Get("nil"),
		},
	})

	cfg := converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
	s, err := New(k, 0, schema, &cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
[Event](FullSync)
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_ProtoConversionError(t *testing.T) {
	k := &mock.Kube{}
	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())
	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{
				"zoo": "zar",
			},
		},
	}

	cl.PrependReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.PrependWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	schema := schemaWithSpecs([]kube.ResourceSpec{
		{
			Kind:     "foo",
			Singular: "foo",
			Plural:   "foos",
			Target:   emptyInfo,
			Converter: func(_ *converter.Config, info resource.Info, name resource.FullName, kind string, u *unstructured.Unstructured) ([]converter.Entry, error) {
				return nil, fmt.Errorf("cant convert")
			},
		},
	})

	cfg := converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
	s, err := New(k, 0, schema, &cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// The add event should not appear.
	log := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
[Event](FullSync)
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_MangledNames(t *testing.T) {
	k := &mock.Kube{}
	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())
	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{
				"zoo": "zar",
			},
		},
	}

	cl.PrependReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.PrependWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	schema := schemaWithSpecs([]kube.ResourceSpec{
		{
			Kind:     "foo",
			Singular: "foo",
			Plural:   "foos",
			Target:   emptyInfo,
			Converter: func(_ *converter.Config, info resource.Info, name resource.FullName, kind string, u *unstructured.Unstructured) ([]converter.Entry, error) {
				e := converter.Entry{
					Key:      resource.FullNameFromNamespaceAndName("foo", name.String()),
					Resource: &types.Struct{},
				}

				return []converter.Entry{e}, nil
			},
		},
	})

	cfg := converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
	s, err := New(k, 0, schema, &cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// The mangled name foo/ns/f1 should appear.
	log := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
[Event](Added: [VKey](empty:foo/ns/f1 @rv1))
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func logChannelOutput(ch chan resource.Event, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		item := <-ch
		result += fmt.Sprintf("%v\n", item)
	}

	result = strings.TrimSpace(result)

	return result
}
