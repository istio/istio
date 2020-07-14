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

package helmreconciler

import (
	"context"
	"io/ioutil"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"istio.io/istio/operator/pkg/object"
)

func TestHelmReconciler_ApplyObject(t *testing.T) {
	tests := []struct {
		name         string
		currentState string
		input        string
		want         string
		wantErr      bool
	}{
		{
			name:  "creates if not present",
			input: "testdata/configmap.yaml",
			want:  "testdata/configmap.yaml",
		},
		{
			name:         "updates if present",
			currentState: "testdata/configmap.yaml",
			input:        "testdata/configmap-changed.yaml",
			want:         "testdata/configmap-changed.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewFakeClient(loadData(t, tt.input).UnstructuredObject())
			obj := loadData(t, tt.input)

			h := &HelmReconciler{client: cl, opts: &Options{}}
			if err := h.ApplyObject(obj.UnstructuredObject()); (err != nil) != tt.wantErr {
				t.Errorf("HelmReconciler.ApplyObject() error = %v, wantErr %v", err, tt.wantErr)
			}

			manifest := loadData(t, tt.want)
			key, _ := client.ObjectKeyFromObject(manifest.UnstructuredObject())
			got, want := obj.UnstructuredObject(), manifest.UnstructuredObject()

			if err := cl.Get(context.Background(), key, got); err != nil {
				t.Errorf("error validating manifest %v: %v", manifest.Hash(), err)
			}
			// remove resource version and annotations (last applied config) when we compare as we don't care
			unstructured.RemoveNestedField(got.Object, "metadata", "resourceVersion")
			unstructured.RemoveNestedField(got.Object, "metadata", "annotations")

			if !reflect.DeepEqual(want, got) {
				t.Errorf("wanted:\n%v\ngot:\n%v",
					object.NewK8sObject(want, nil, nil).YAMLDebugString(),
					object.NewK8sObject(got, nil, nil).YAMLDebugString(),
				)
			}

		})
	}
}

func loadData(t *testing.T, file string) *object.K8sObject {
	contents, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := object.ParseYAMLToK8sObject(contents)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}
