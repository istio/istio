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

package mock

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"istio.io/istio/galley/pkg/testing/common"
)

// ResourceInterface is a mock implementation of dynamic.ResourceInterface
type ResourceInterface struct {
	e            *common.MockLog
	ListResult   runtime.Object
	CreateResult *unstructured.Unstructured
	UpdateResult *unstructured.Unstructured
	WatchResult  watch.Interface
	ErrorResult  error
}

var _ dynamic.ResourceInterface = &ResourceInterface{}

// DeleteCollection deletes a collection of objects.
func (r *ResourceInterface) DeleteCollection(deleteOptions *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	r.e.Append("DeleteCollection")
	return r.ErrorResult
}

// List returns a list of objects for this resource.
func (r *ResourceInterface) List(opts metav1.ListOptions) (runtime.Object, error) {
	r.e.Append("List")
	return r.ListResult, r.ErrorResult
}

// Create creates the provided resource.
func (r *ResourceInterface) Create(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	r.e.Append("Create %s/%s", obj.GetName(), obj.GetAPIVersion())
	return r.CreateResult, r.ErrorResult
}

// Update updates the provided resource.
func (r *ResourceInterface) Update(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	r.e.Append("Update %s/%s", obj.GetName(), obj.GetAPIVersion())
	return r.UpdateResult, r.ErrorResult
}

// Delete deletes the resource with the specified name.
func (r *ResourceInterface) Delete(name string, opts *metav1.DeleteOptions) error {
	r.e.Append("Delete %s", name)
	return r.ErrorResult
}

// Get gets the resource with the specified name.
func (r *ResourceInterface) Get(name string, opts metav1.GetOptions) (*unstructured.Unstructured, error) {
	panic("Not Implemented: Get")

}

// Watch returns a watch.Interface that watches the resource.
func (r *ResourceInterface) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	r.e.Append("Watch")
	return r.WatchResult, r.ErrorResult
}

// Patch patches the provided resource.
func (r *ResourceInterface) Patch(name string, pt types.PatchType, data []byte) (*unstructured.Unstructured, error) {
	panic("Not Implemented: Patch")

}
