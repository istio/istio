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

package crd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/galley/pkg/testing/machinery/mock"
)

func TestGetAll_Simple(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	items := []apiext.CustomResourceDefinition{
		{ObjectMeta: v1.ObjectMeta{Name: "foo"}},
		{ObjectMeta: v1.ObjectMeta{Name: "bar"}},
	}

	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: items,
	}, nil)

	crds, err := GetAll(i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(crds, items) {
		t.Fatalf("result mismatch: got:%v, wanted:%v", crds, items)
	}
}

func TestGetAll_Empty(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{}, nil)

	crds, err := GetAll(i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(crds) != 0 {
		t.Fatalf("unexpected items in result: %v", crds)
	}
}

func TestGetAll_ListError(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	i.MockCRDI.AddListResponse(nil, errors.New("some list error"))

	_, err := GetAll(i)
	if err == nil || err.Error() != "some list error" {
		t.Fatalf("error mismatch: %v", err)
	}
}

func TestGetAll_Continuation(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	items1 := []apiext.CustomResourceDefinition{
		{ObjectMeta: v1.ObjectMeta{Name: "foo"}},
	}

	items2 := []apiext.CustomResourceDefinition{
		{ObjectMeta: v1.ObjectMeta{Name: "bar"}},
	}

	expected := []apiext.CustomResourceDefinition{items1[0], items2[0]}

	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items:    items1,
		ListMeta: v1.ListMeta{Continue: "continue"},
	}, nil)
	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: items2,
	}, nil)

	crds, err := GetAll(i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(crds, expected) {
		t.Fatalf("result mismatch: got:%v, wanted:%v", crds, expected)
	}
}

func TestPurge_ListError(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	i.MockCRDI.AddListResponse(nil, errors.New("some list error"))

	err := Purge(i, getMappingForOperationsTests())
	if err == nil || err.Error() != "some list error" {
		t.Fatalf("expected error not found:: %v", err)
	}
}

func TestPurge(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	items := []apiext.CustomResourceDefinition{
		{ObjectMeta: v1.ObjectMeta{Name: "foo.g2"},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group: "g2",
			}},
		{ObjectMeta: v1.ObjectMeta{Name: "bar.g2"},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group: "g2",
			}},
	}

	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: items,
	}, nil)
	i.MockCRDI.AddDeleteResponse(nil)
	i.MockCRDI.AddDeleteResponse(nil)

	err := Purge(i, getMappingForOperationsTests())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `
LIST
DELETE: foo.g2
DELETE: bar.g2`
	if strings.TrimSpace(expected) != strings.TrimSpace(i.MockCRDI.String()) {
		t.Fatalf("Expected operation mismatch: got:\n%v\nwanted:\n%v\n", i.MockCRDI.String(), expected)
	}
}

func TestPurge_DeleteError(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	items := []apiext.CustomResourceDefinition{
		{ObjectMeta: v1.ObjectMeta{Name: "foo.g2"},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group: "g2",
			}},
		{ObjectMeta: v1.ObjectMeta{Name: "bar.g2"},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group: "g2",
			}},
	}

	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: items,
	}, nil)
	i.MockCRDI.AddDeleteResponse(errors.New("some delete error"))

	err := Purge(i, getMappingForOperationsTests())
	if err == nil || err.Error() != "some delete error" {
		t.Fatalf("expected error not found:: %v", err)
	}
}

func TestPurge_Originals(t *testing.T) {
	i := mock.NewInterface()
	defer i.MockCRDI.Close()

	items := []apiext.CustomResourceDefinition{
		{ObjectMeta: v1.ObjectMeta{Name: "foo.g1"},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group: "g1",
			}},
		{ObjectMeta: v1.ObjectMeta{Name: "bar.g2"},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group: "g2",
			}},
	}

	i.MockCRDI.AddListResponse(&apiext.CustomResourceDefinitionList{
		Items: items,
	}, nil)
	i.MockCRDI.AddDeleteResponse(nil)

	err := Purge(i, getMappingForOperationsTests())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only original is deleted.
	expected := `
LIST
DELETE: bar.g2`
	if strings.TrimSpace(expected) != strings.TrimSpace(i.MockCRDI.String()) {
		t.Fatalf("Expected operation mismatch: got:\n%v\nwanted:\n%v\n", i.MockCRDI.String(), expected)
	}
}

func getMappingForOperationsTests() Mapping {
	if m, err := NewMapping(map[schema.GroupVersion]schema.GroupVersion{
		{
			Group:   "g1",
			Version: "v1",
		}: {
			Group:   "g2",
			Version: "v2",
		},
	}); err != nil {
		panic(err)
	} else {
		return m
	}
}
