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

package source

import (
	errs "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/kube"

	"istio.io/istio/galley/pkg/kube/converter"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/mock"
)

var emptyInfo resource.Info

func init() {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	schema := b.Build()
	emptyInfo, _ = schema.Lookup("type.googleapis.com/google.protobuf.Empty")
}

func TestNewSource(t *testing.T) {
	k := &mock.Kube{}
	for i := 0; i < 100; i++ {
		cl := &fake.FakeClient{
			Fake: &dtesting.Fake{},
		}
		k.AddResponse(cl, nil)
	}

	p, err := New(k, 0)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if p == nil {
		t.Fatal("expected non-nil source")
	}
}

func TestNewSource_Error(t *testing.T) {
	k := &mock.Kube{}
	k.AddResponse(nil, errs.New("newDynamicClient error"))

	_, err := New(k, 0)
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Expected error not found: %v", err)
	}
}

func TestSource_BasicEvents(t *testing.T) {
	k := &mock.Kube{}
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"kind":            "foo",
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{},
		},
	}

	cl.AddReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.AddWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	entries := []kube.ResourceSpec{
		{
			Kind:      "foo",
			Singular:  "foo",
			Plural:    "foos",
			Target:    emptyInfo,
			Converter: converter.Get("identity"),
		},
	}

	s, err := newSource(k, 0, entries)
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
[Event](Added: [VKey](type.googleapis.com/google.protobuf.Empty:ns/f1 @rv1))
[Event](FullSync: [VKey](: @))
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_ProtoConversionError(t *testing.T) {
	k := &mock.Kube{}
	cl := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"kind":            "foo",
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{
				"zoo": "zar",
			},
		},
	}

	cl.AddReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.AddWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	entries := []kube.ResourceSpec{
		{
			Kind:     "foo",
			Singular: "foo",
			Plural:   "foos",
			Target:   emptyInfo,
			Converter: func(info resource.Info, name string, u *unstructured.Unstructured) (string, time.Time, proto.Message, error) {
				return "", time.Time{}, nil, fmt.Errorf("cant convert")
			},
		},
	}

	s, err := newSource(k, 0, entries)
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
[Event](FullSync: [VKey](: @))
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
