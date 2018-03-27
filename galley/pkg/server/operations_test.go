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

package server

import (
	"errors"
	"fmt"
	"testing"

	"k8s.io/api/core/v1"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	dtesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/testing/mock"
)

func TestPurge_CRDI_Error(t *testing.T) {
	k := mock.NewKube()
	kube := kfake.NewSimpleClientset()
	k.AddResponse(nil, errors.New("some CRDI error"))
	k.AddResponse(kube, nil)

	m := Mapping()
	err := Purge(k, m)
	if err == nil || err.Error() != "some CRDI error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_Kubernetes_Error(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	k.AddResponse(crdi, nil)
	k.AddResponse(nil, errors.New("some Kube error"))

	m := Mapping()
	err := Purge(k, m)
	if err == nil || err.Error() != "some Kube error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_GetCRDs_Error(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	kube := kfake.NewSimpleClientset()
	k.AddResponse(crdi, nil)
	k.AddResponse(kube, nil)

	crdi.AddListResponse(nil, errors.New("some crd list error"))

	m := Mapping()
	err := Purge(k, m)
	if err == nil || err.Error() != "some crd list error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_GetNamespaces_Error(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	kube := kfake.NewSimpleClientset()
	k.AddResponse(crdi, nil)
	k.AddResponse(kube, nil)

	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)

	kube.PrependReactor("list", "namespaces", func(a dtesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("some namespace list error")
	})

	m := Mapping()
	err := Purge(k, m)
	if err == nil || err.Error() != "some namespace list error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_Empty(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	kube := kfake.NewSimpleClientset()
	k.AddResponse(crdi, nil)
	k.AddResponse(kube, nil)

	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)

	kube.PrependReactor("list", "namespaces", func(a dtesting.Action) (bool, runtime.Object, error) {
		return true, &v1.NamespaceList{}, nil
	})

	// For the list operation performed by crd.Purge
	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)

	m := Mapping()
	err := Purge(k, m)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_UnrelatedCRD(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	kube := kfake.NewSimpleClientset()
	k.AddResponse(crdi, nil)
	k.AddResponse(kube, nil)

	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			{
				Spec: apiext.CustomResourceDefinitionSpec{
					Group:   "some other group",
					Version: " some other version",
					Names: apiext.CustomResourceDefinitionNames{
						Singular: "foo",
						Plural:   "foos",
						ListKind: "foolist",
						Kind:     "foo",
					},
				},
			},
		},
	}, nil)

	kube.PrependReactor("list", "namespaces", func(a dtesting.Action) (bool, runtime.Object, error) {
		return true, &v1.NamespaceList{}, nil
	})

	// For the list operation performed by crd.Purge
	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)

	m := Mapping()
	err := Purge(k, m)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_Basic(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	kube := kfake.NewSimpleClientset()
	dyn := fake.FakeClient{
		Fake: &dtesting.Fake{},
	}

	k.AddResponse(crdi, nil)
	k.AddResponse(kube, nil)
	k.AddResponse(&dyn, nil)

	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			{
				Spec: apiext.CustomResourceDefinitionSpec{
					Group:   "internal.istio.io",
					Version: "v1alpha2",
					Names: apiext.CustomResourceDefinitionNames{
						Singular: "foo",
						Plural:   "foos",
						ListKind: "foolist",
						Kind:     "foo",
					},
				},
			},
		},
	}, nil)

	kube.PrependReactor("list", "namespaces", func(a dtesting.Action) (bool, runtime.Object, error) {
		return true, &v1.NamespaceList{}, nil
	})

	// For the list operation performed by crd.Purge
	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)

	m := Mapping()
	err := Purge(k, m)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPurge_DeleteError(t *testing.T) {
	k := mock.NewKube()
	crdi := mock.NewInterface()
	defer crdi.Close()
	kube := kfake.NewSimpleClientset()
	k.AddResponse(crdi, nil)
	k.AddResponse(kube, nil)

	// Triggers error during deletion
	k.AddResponse(nil, errors.New("some delete error"))

	crdi.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: []apiext.CustomResourceDefinition{
			{
				Spec: apiext.CustomResourceDefinitionSpec{
					Group:   "internal.istio.io",
					Version: "v1alpha2",
					Names: apiext.CustomResourceDefinitionNames{
						Singular: "foo",
						Plural:   "foos",
						ListKind: "foolist",
						Kind:     "foo",
					},
				},
			},
		},
	}, nil)

	kube.PrependReactor("list", "namespaces", func(a dtesting.Action) (bool, runtime.Object, error) {
		return true, &v1.NamespaceList{}, nil
	})

	m := Mapping()
	err := Purge(k, m)
	if err == nil || err.Error() != "purge error: some delete error" {
		t.Fatalf("Unexpected error: %v", err)
	}
}
