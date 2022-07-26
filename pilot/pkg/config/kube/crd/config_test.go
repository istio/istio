// Copyright Istio Authors
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

	"istio.io/istio/pilot/pkg/config/kube/crd"
)

func TestKind(t *testing.T) {
	obj := crd.IstioKind{}

	spec := map[string]any{"a": "b"}
	obj.Spec = spec
	if got := obj.GetSpec(); !reflect.DeepEqual(spec, got) {
		t.Errorf("GetSpec() => got %v, want %v", got, spec)
	}

	status := map[string]any{"yo": "lit"}
	obj.Status = status
	if got := obj.GetStatus(); !reflect.DeepEqual(status, got) {
		t.Errorf("GetStatus() => got %v, want %v", got, status)
	}

	meta := meta_v1.ObjectMeta{Name: "test"}
	obj.ObjectMeta = meta
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
}
