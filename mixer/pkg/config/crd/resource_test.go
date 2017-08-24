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

package crd

import (
	"encoding/json"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResource(t *testing.T) {
	const jsonSpec = `
	{"foo": 1, "bar": {"x": false}, "bazz":[{"y": "foo", "h2": 1.4}, {"y": "bar"}]}
	`
	spec := map[string]interface{}{}
	if err := json.Unmarshal([]byte(jsonSpec), &spec); err != nil {
		t.Fatal(err)
	}
	in := &resource{
		Kind:       "Test",
		APIVersion: apiGroupVersion,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "ns",
		},
		Spec: spec,
	}
	okind := in.GetObjectKind()
	wantKind := schema.GroupVersionKind{Group: apiGroup, Version: apiVersion, Kind: "Test"}
	if gvk := okind.GroupVersionKind(); !reflect.DeepEqual(gvk, wantKind) {
		t.Errorf("Got %+v, Want %+v", gvk, wantKind)
	}
	out := in.DeepCopyObject()
	if !reflect.DeepEqual(out, in) {
		t.Errorf("Got %+v, Want %+v", out, in)
	}
}

func TestResourceListDeepCopy(t *testing.T) {
	const jsonSpec1 = `
	{"foo": 1, "bar": {"x": false}, "bazz":[{"y": "foo", "h2": 1.4}, {"y": "bar"}]}
	`
	const jsonSpec2 = `{"foo": null}`
	spec1 := map[string]interface{}{}
	if err := json.Unmarshal([]byte(jsonSpec1), &spec1); err != nil {
		t.Fatal(err)
	}
	spec2 := map[string]interface{}{}
	if err := json.Unmarshal([]byte(jsonSpec2), &spec2); err != nil {
		t.Fatal(err)
	}
	in := &resourceList{
		Kind: "TestList",
		ListMeta: metav1.ListMeta{
			ResourceVersion: "42",
		},
		Items: []*resource{
			{
				Kind:       "Test",
				APIVersion: apiVersion,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name1",
					Namespace: "ns",
				},
				Spec: spec1,
			},
			{
				Kind:       "Test",
				APIVersion: apiVersion,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name2",
					Namespace: "ns",
				},
				Spec: spec2,
			},
		},
	}
	okind := in.GetObjectKind()
	wantKind := schema.GroupVersionKind{Group: apiGroup, Version: apiVersion, Kind: "TestList"}
	if gvk := okind.GroupVersionKind(); !reflect.DeepEqual(gvk, wantKind) {
		t.Errorf("Got %+v, Want %+v", gvk, wantKind)
	}
	out := in.DeepCopyObject()
	if !reflect.DeepEqual(out, in) {
		t.Errorf("Got %+v, Want %+v", out, in)
	}

}
