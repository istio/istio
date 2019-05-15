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

package dynamic_test

import (
	"fmt"
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/meshconfig"
	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/dynamic"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/events"
	"istio.io/istio/galley/pkg/testing/mock"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	k8sDynamic "k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	k8sTesting "k8s.io/client-go/testing"
)

var (
	emptyInfo resource.Info

	cfg = converter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
)

func init() {
	b := resource.NewSchemaBuilder()
	b.Register("empty", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()
	emptyInfo, _ = s.Lookup("empty")
}

func TestNewSource(t *testing.T) {
	k := &mock.Kube{}
	for i := 0; i < 100; i++ {
		_ = fakeClient(k)
	}

	spec := kubeMeta.Types.All()[0]

	client, _ := k.DynamicInterface()
	_ = newOrFail(t, client, spec)
}

func TestStartTwiceShouldError(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create the source
	w, client := createMocks()
	defer w.Stop()
	spec := schema.ResourceSpec{
		Kind:      "List",
		Singular:  "List",
		Plural:    "foos",
		Target:    emptyInfo,
		Converter: converter.Get("identity"),
	}
	s := newOrFail(t, client, spec)

	// Start it once.
	ch := startOrFail(t, s)
	defer s.Stop()

	// Start again should fail
	err := s.Start(events.ChannelHandler(ch))
	g.Expect(err).ToNot(BeNil())
}

func TestStopTwiceShouldSucceed(t *testing.T) {
	// Create the source
	w, client := createMocks()
	defer w.Stop()
	spec := schema.ResourceSpec{
		Kind:      "List",
		Singular:  "List",
		Plural:    "foos",
		Target:    emptyInfo,
		Converter: converter.Get("identity"),
	}
	s := newOrFail(t, client, spec)

	// Start it once.
	_ = startOrFail(t, s)

	s.Stop()
	s.Stop()
}

func TestEvents(t *testing.T) {
	g := NewGomegaWithT(t)

	w, client := createMocks()
	defer w.Stop()

	spec := schema.ResourceSpec{
		Kind:      "List",
		Singular:  "List",
		Plural:    "foos",
		Target:    emptyInfo,
		Converter: converter.Get("identity"),
	}

	// Create and start the source
	s := newOrFail(t, client, spec)
	ch := startOrFail(t, s)
	defer s.Stop()

	obj := &unstructured.Unstructured{
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

	t.Run("Add", func(t *testing.T) {
		obj = obj.DeepCopy()
		expected := resource.Event{
			Kind:  resource.Added,
			Entry: toEntry(obj),
		}
		w.Send(watch.Event{Type: watch.Added, Object: obj})
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// The full sync should come immediately after the first event.
	expectFullSync(t, ch)

	t.Run("Update", func(t *testing.T) {
		obj = obj.DeepCopy()
		obj.SetResourceVersion("rv2")

		expected := resource.Event{
			Kind:  resource.Updated,
			Entry: toEntry(obj),
		}
		w.Send(watch.Event{Type: watch.Modified, Object: obj})
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	t.Run("UpdateSameVersion", func(t *testing.T) {
		// Make a copy so we can change it without affecting the original.
		objCopy := obj.DeepCopy()
		objCopy.SetResourceVersion("rv2")

		w.Send(watch.Event{Type: watch.Modified, Object: objCopy})
		events.ExpectNone(t, ch)
	})

	t.Run("Delete", func(t *testing.T) {
		expected := resource.Event{
			Kind: resource.Deleted,
			Entry: resource.Entry{
				ID: getID(obj),
			},
		}
		w.Send(watch.Event{Type: watch.Deleted, Object: obj})
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestSource_BasicEvents_NoConversion(t *testing.T) {
	w, client := createMocks()
	defer w.Stop()

	spec := schema.ResourceSpec{
		Kind:      "List",
		Singular:  "List",
		Plural:    "foos",
		Target:    emptyInfo,
		Converter: converter.Get("nil"),
	}

	// Create and start the source
	s := newOrFail(t, client, spec)
	ch := startOrFail(t, s)
	defer s.Stop()

	obj := unstructured.Unstructured{
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

	// Trigger an Added event.
	w.Send(watch.Event{Type: watch.Added, Object: &obj})

	// Expect only the full sync event.
	expectFullSync(t, ch)
}

func TestSource_ProtoConversionError(t *testing.T) {
	w, client := createMocks()
	defer w.Stop()

	spec := schema.ResourceSpec{
		Kind:     "foo",
		Singular: "foo",
		Plural:   "foos",
		Target:   emptyInfo,
		Converter: func(_ *converter.Config, info resource.Info, name resource.FullName, kind string, u *unstructured.Unstructured) ([]converter.Entry, error) {
			return nil, fmt.Errorf("cant convert")
		},
	}

	// Create and start the source
	s := newOrFail(t, client, spec)
	ch := startOrFail(t, s)
	defer s.Stop()

	obj := unstructured.Unstructured{
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

	// Trigger an Added event.
	w.Send(watch.Event{Type: watch.Added, Object: &obj})

	// The add event should not appear.
	expectFullSync(t, ch)
}

func TestSource_MangledNames(t *testing.T) {
	g := NewGomegaWithT(t)

	w, client := createMocks()
	defer w.Stop()

	spec := schema.ResourceSpec{
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
	}

	// Create and start the source.
	s := newOrFail(t, client, spec)
	ch := startOrFail(t, s)
	defer s.Stop()

	obj := unstructured.Unstructured{
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

	// Expect an Added event for the resource.
	expected := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Key: resource.Key{
					Collection: emptyInfo.Collection,
					FullName:   resource.FullNameFromNamespaceAndName("foo/"+obj.GetNamespace(), obj.GetName()),
				},
				Version: resource.Version(obj.GetResourceVersion()),
			},
			Metadata: resource.Metadata{
				Labels:      obj.GetLabels(),
				Annotations: obj.GetAnnotations(),
			},
			Item: &types.Struct{},
		},
	}

	// Trigger an Added event.
	w.Send(watch.Event{Type: watch.Added, Object: &obj})

	// The mangled name foo/ns/f1 should appear.
	actual := events.Expect(t, ch)
	g.Expect(actual).To(Equal(expected))

	// The full sync should come immediately after the first event.
	expectFullSync(t, ch)
}

func newOrFail(t *testing.T, dynClient k8sDynamic.Interface, spec schema.ResourceSpec) runtime.Source {
	t.Helper()
	s, err := dynamic.New(dynClient, 0, spec, &cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non nil source")
	}
	return s
}

func startOrFail(t *testing.T, s runtime.Source) chan resource.Event {
	t.Helper()
	g := NewGomegaWithT(t)

	ch := make(chan resource.Event, 1024)
	err := s.Start(events.ChannelHandler(ch))
	g.Expect(err).To(BeNil())
	return ch
}

func createMocks() (*mock.Watch, k8sDynamic.Interface) {
	k := &mock.Kube{}
	cl := fakeClient(k)
	w := mockWatch(cl)
	client, _ := k.DynamicInterface()
	return w, client
}

func fakeClient(k *mock.Kube) *fake.FakeDynamicClient {
	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())
	k.AddResponse(cl, nil)
	return cl
}

func mockWatch(cl *fake.FakeDynamicClient) *mock.Watch {
	w := mock.NewWatch()
	cl.PrependWatchReactor("foos", func(_ k8sTesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})
	return w
}

func expectFullSync(t *testing.T, ch chan resource.Event) {
	t.Helper()
	g := NewGomegaWithT(t)
	actual := events.ExpectOne(t, ch)
	g.Expect(actual).To(Equal(resource.FullSyncEvent))
}

func getID(obj *unstructured.Unstructured) resource.VersionedKey {
	return resource.VersionedKey{
		Key: resource.Key{
			Collection: emptyInfo.Collection,
			FullName:   resource.FullNameFromNamespaceAndName(obj.GetNamespace(), obj.GetName()),
		},
		Version: resource.Version(obj.GetResourceVersion()),
	}
}

func toEntry(obj *unstructured.Unstructured) resource.Entry {
	return resource.Entry{
		ID: getID(obj),
		Metadata: resource.Metadata{
			Labels:      obj.GetLabels(),
			Annotations: obj.GetAnnotations(),
		},
		Item: &types.Empty{},
	}
}
