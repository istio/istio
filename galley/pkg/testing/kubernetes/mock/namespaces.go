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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/galley/pkg/testing/common"
)

type namespaces struct {
	e           *common.MockLog
	ListResult  *v1.NamespaceList
	ErrorResult error
}

var _ corev1.NamespaceInterface = &namespaces{}

// List interface method implementation
func (n *namespaces) List(opts metav1.ListOptions) (*v1.NamespaceList, error) {
	return n.ListResult, n.ErrorResult
}

// Create interface method implementation
func (n *namespaces) Create(*v1.Namespace) (*v1.Namespace, error) {
	panic("Not implemented")
}

// Update interface method implementation
func (n *namespaces) Update(*v1.Namespace) (*v1.Namespace, error) {
	panic("Not implemented")
}

// UpdateStatus interface method implementation
func (n *namespaces) UpdateStatus(*v1.Namespace) (*v1.Namespace, error) {
	panic("Not implemented")
}

// Delete interface method implementation
func (n *namespaces) Delete(name string, options *metav1.DeleteOptions) error {
	panic("Not implemented")
}

// DeleteCollection interface method implementation
func (n *namespaces) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("Not implemented")
}

// Get interface method implementation
func (n *namespaces) Get(name string, options metav1.GetOptions) (*v1.Namespace, error) {
	panic("Not implemented")
}

// Watch interface method implementation
func (n *namespaces) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	panic("Not implemented")
}

// Patch interface method implementation
func (n *namespaces) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.Namespace, err error) {
	panic("Not implemented")
}

// Finalize interface method implementation
func (n *namespaces) Finalize(item *v1.Namespace) (*v1.Namespace, error) {
	panic("Not implemented")
}
