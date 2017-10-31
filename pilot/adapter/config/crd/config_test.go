// Copyright 2017 Istio Authors
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

package crd_test

import (
	"reflect"
	"testing"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/adapter/config/crd"
)

func TestKind(t *testing.T) {
	obj := crd.IstioKind{}

	spec := map[string]interface{}{"a": "b"}
	obj.SetSpec(spec)
	if got := obj.GetSpec(); !reflect.DeepEqual(spec, got) {
		t.Errorf("GetSpec() => got %v, want %v", got, spec)
	}

	meta := meta_v1.ObjectMeta{Name: "test"}
	obj.SetObjectMeta(meta)
	if got := obj.GetObjectMeta(); !reflect.DeepEqual(meta, got) {
		t.Errorf("GetObjectMeta() => got %v, want %v", got, meta)
	}

	if got := obj.DeepCopy(); !reflect.DeepEqual(*got, obj) {
		t.Errorf("DeepCopy() => got %v, want %v", got, obj)
	}

	if got := obj.DeepCopyObject(); !reflect.DeepEqual(got, &obj) {
		t.Errorf("DeepCopyObject() => got %v, want %v", got, obj)
	}

	var empty *crd.IstioKind
	if empty.DeepCopy() != nil {
		t.Error("DeepCopy of nil should return nil")
	}

	if empty.DeepCopyObject() != nil {
		t.Error("DeepCopyObject of nil should return nil")
	}

	obj2 := crd.IstioKind{}
	spec2 := map[string]interface{}{"a": "b"}
	obj2.SetSpec(spec2)

	list := crd.IstioKindList{Items: []crd.IstioKind{obj, obj2}}
	if got := list.GetItems(); len(got) != len(list.Items) ||
		!reflect.DeepEqual(got[0], &obj) ||
		!reflect.DeepEqual(got[1], &obj2) {
		t.Errorf("GetItems() => got %#v, want %#v", got, list.Items)
	}

	if got := list.DeepCopy(); !reflect.DeepEqual(*got, list) {
		t.Errorf("DeepCopy() => got %v, want %v", got, list)
	}

	if got := list.DeepCopyObject(); !reflect.DeepEqual(got, &list) {
		t.Errorf("DeepCopyObject() => got %v, want %v", got, list)
	}

	var emptylist *crd.IstioKindList
	if emptylist.DeepCopy() != nil {
		t.Error("DeepCopy of nil should return nil")
	}

	if emptylist.DeepCopyObject() != nil {
		t.Error("DeepCopyObject of nil should return nil")
	}
}
