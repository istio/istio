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
	"strings"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	dtesting "k8s.io/client-go/testing"
)

func TestDeleteAll_NewClientError(t *testing.T) {
	newDynamicClient = func(cfg *rest.Config) (dynamic.Interface, error) {
		return nil, errors.New("newDynamicClient error")
	}

	err := DeleteAll(&rest.Config{}, "foos", "foo", schema.GroupVersion{}, []string{"ns1"})
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteAll_Basic(t *testing.T) {
	m := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	newDynamicClient = func(cfg *rest.Config) (dynamic.Interface, error) {
		return m, nil
	}

	err := DeleteAll(&rest.Config{}, "foos", "foo", schema.GroupVersion{}, []string{"ns1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `
delete-collection foos
`
	check(t, writeActions(m.Actions()), expected)
}

func TestDeleteAll_Error(t *testing.T) {
	m := &fake.FakeClient{
		Fake: &dtesting.Fake{},
	}
	newDynamicClient = func(cfg *rest.Config) (dynamic.Interface, error) {
		return m, nil
	}
	m.AddReactor("delete-collection", "foos", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("some DeleteCollection error")
	})

	err := DeleteAll(&rest.Config{}, "foos", "foo", schema.GroupVersion{}, []string{"ns1"})
	if err == nil || err.Error() != "some DeleteCollection error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetNamespaces(t *testing.T) {
	l := &v1.NamespaceList{
		Items: []v1.Namespace{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "bar"},
			},
		},
	}
	m := kfake.NewSimpleClientset(l)

	newKubernetesClient = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return m, nil
	}

	ns, err := GetNamespaces(&rest.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ns) != 2 {
		t.Fatalf("unexpected ns len: %d", len(ns))
	}
}

func TestGetNamespaces_Empty(t *testing.T) {
	l := &v1.NamespaceList{
		Items: []v1.Namespace{},
	}
	m := kfake.NewSimpleClientset(l)

	newKubernetesClient = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return m, nil
	}

	ns, err := GetNamespaces(&rest.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ns) != 0 {
		t.Fatalf("unexpected ns len: %d", len(ns))
	}
}

func TestGetNamespaces_NewClientError(t *testing.T) {
	newKubernetesClient = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return nil, errors.New("newKubernetesClient error")
	}

	_, err := GetNamespaces(&rest.Config{})
	if err == nil || err.Error() != "newKubernetesClient error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetNamespaces_ListError(t *testing.T) {
	m := kfake.Clientset{}
	m.AddReactor("*", "namespaces", func(action dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("some list error")
	})

	newKubernetesClient = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return &m, nil
	}

	_, err := GetNamespaces(&rest.Config{})
	if err == nil || err.Error() != "some list error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func check(t *testing.T, actual string, expected string) {
	if strings.TrimSpace(actual) != strings.TrimSpace(expected) {
		t.Fatalf("mismatch.\nGot:\n%s\nWanted:\n%s\n", actual, expected)
	}
}
