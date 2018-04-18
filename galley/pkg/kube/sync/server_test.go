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

package sync

import (
	"errors"
	"sync"
	"testing"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/testing/mock"
)

func TestServer_CRDI_Error(t *testing.T) {
	k := mock.NewKube()
	k.AddResponse(nil, errors.New("some crdi error"))

	s := New(k, Mapping(), 0)

	if err := s.Start(); err == nil || err.Error() != "some crdi error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoubleStart(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	crdi.EagerPanic = true
	defer crdi.Close()
	k.AddResponse(crdi, nil)

	w := mock.NewWatch()
	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	crdi.AddWatchResponse(w, nil)

	s := New(k, Mapping(), 0)

	err := s.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = s.Start()
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestDoubleStop(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	crdi.EagerPanic = true
	defer crdi.Close()
	k.AddResponse(crdi, nil)

	w := mock.NewWatch()
	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	crdi.AddWatchResponse(w, nil)

	s := New(k, Mapping(), 0)

	err := s.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s.Stop()
	s.Stop()
}

var sourceCRD = &apiext.CustomResourceDefinition{
	TypeMeta:   metav1.TypeMeta{Kind: "CustomResourceDefinitions"},
	ObjectMeta: metav1.ObjectMeta{Name: "foos.config.istio.io", ResourceVersion: "v1alpha2"},
	Spec: apiext.CustomResourceDefinitionSpec{
		Names: apiext.CustomResourceDefinitionNames{
			Kind:     "foo",
			ListKind: "foolist",
			Singular: "foo",
			Plural:   "foos",
		},
		Group:   "config.istio.io",
		Version: "v1alpha2",
	},
}

var sourceCR = &unstructured.Unstructured{
	Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":            "f1",
			"kind":            "foo",
			"resourceVersion": "config.istio.io/v1alpha2",
		},
	},
}

func TestBasic(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	crdi.EagerPanic = true
	defer crdi.Close()
	k.AddResponse(crdi, nil)

	w := mock.NewWatch()
	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)
	crdi.AddWatchResponse(w, nil)

	wg := sync.WaitGroup{}
	wg.Add(1)
	hookFn := func(e interface{}) {
		wg.Done()
	}

	s := newServer(k, Mapping(), 0, hookFn)

	err := s.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect a create crdi operation
	crdi.AddCreateResponse(&apiext.CustomResourceDefinition{}, nil)

	// Expect two calls for dynamic interface creation, one for source, one for destination.
	src := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(src, nil)

	dst := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	k.AddResponse(dst, nil)

	// Respond to the read request for initial scan.
	src.AddReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}, nil
	})

	// Respond to the watch for source
	wsrc := mock.NewWatch()
	src.AddWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, wsrc, nil
	})

	// Respond to the read request for initial scan.
	dst.AddReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{}, nil
	})

	// Respond to the watch for source
	wdst := mock.NewWatch()
	dst.AddWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, wdst, nil
	})

	// Expect a "list" call for the destination resource.
	dst.AddReactor("list", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{}, nil
	})

	// Send the event to trigger creation of resource synchronizer.
	w.Send(watch.Event{Type: watch.Added, Object: sourceCRD.DeepCopy()})

	wg.Wait()

	// Now try delete
	wg.Add(1)
	// Send the event to trigger creation of resource synchronizer.
	w.Send(watch.Event{Type: watch.Deleted, Object: sourceCRD.DeepCopy()})
	wg.Wait()

	s.Stop()
	// no crash expected
}
