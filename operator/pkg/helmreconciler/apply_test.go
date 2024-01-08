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
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha12 "istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/object"
)

var interceptorFunc = interceptor.Funcs{Patch: func(
	ctx context.Context,
	clnt client.WithWatch,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	// Apply patches are supposed to upsert, but fake client fails if the object doesn't exist,
	// if an apply patch occurs for an object that doesn't yet exist, create it.
	if patch.Type() != types.ApplyPatchType {
		return clnt.Patch(ctx, obj, patch, opts...)
	}
	check, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return errors.New("could not check for object in fake client")
	}
	if err := clnt.Get(ctx, client.ObjectKeyFromObject(obj), check); kerrors.IsNotFound(err) {
		if err := clnt.Create(ctx, check); err != nil {
			return fmt.Errorf("could not inject object creation for fake: %w", err)
		}
	} else if err != nil {
		return err
	}
	obj.SetResourceVersion(check.GetResourceVersion())
	return clnt.Update(ctx, obj)
}}

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
		// Test IstioOperator field removals
		{
			name:  "creates if not present",
			input: "testdata/iop-test-gw-1.yaml",
			want:  "testdata/iop-test-gw-1.yaml",
		},
		{
			name:         "updates if present",
			currentState: "testdata/iop-test-gw-1.yaml",
			input:        "testdata/iop-changed.yaml",
			want:         "testdata/iop-changed.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := loadData(t, tt.input)
			var k8sClient client.Client
			if tt.currentState != "" {
				k8sClient = fake.NewClientBuilder().
					WithRuntimeObjects(loadData(t, tt.currentState).
						UnstructuredObject()).WithInterceptorFuncs(interceptorFunc).Build()
			} else {
				// no current state provided, initialize fake client without runtime object
				k8sClient = fake.NewClientBuilder().WithInterceptorFuncs(interceptorFunc).Build()
			}

			cl := k8sClient
			h := &HelmReconciler{
				client: cl,
				opts:   &Options{},
				iop: &v1alpha1.IstioOperator{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-operator",
						Namespace: "istio-operator-test",
					},
					Spec: &v1alpha12.IstioOperatorSpec{},
				},
				countLock:     &sync.Mutex{},
				prunedKindSet: map[schema.GroupKind]struct{}{},
			}
			if err := h.ApplyObject(obj.UnstructuredObject()); (err != nil) != tt.wantErr {
				t.Errorf("HelmReconciler.ApplyObject() error = %v, wantErr %v", err, tt.wantErr)
			}

			manifest := loadData(t, tt.want)
			key := client.ObjectKeyFromObject(manifest.UnstructuredObject())
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
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := object.ParseYAMLToK8sObject(contents)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}
