/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// client is a client.Client that reads and writes directly from/to an API server.  It lazily initializes
// new clients at the time they are used, and caches the client.
type unstructuredClient struct {
	client     dynamic.Interface
	restMapper meta.RESTMapper
}

// Create implements client.Client
func (uc *unstructuredClient) Create(_ context.Context, obj runtime.Object) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unstructured client did not understand object: %T", obj)
	}
	r, err := uc.getResourceInterface(u.GroupVersionKind(), u.GetNamespace())
	if err != nil {
		return err
	}
	i, err := r.Create(u, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	u.Object = i.Object
	return nil
}

// Update implements client.Client
func (uc *unstructuredClient) Update(_ context.Context, obj runtime.Object) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unstructured client did not understand object: %T", obj)
	}
	r, err := uc.getResourceInterface(u.GroupVersionKind(), u.GetNamespace())
	if err != nil {
		return err
	}
	i, err := r.Update(u, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	u.Object = i.Object
	return nil
}

// Delete implements client.Client
func (uc *unstructuredClient) Delete(_ context.Context, obj runtime.Object, opts ...DeleteOptionFunc) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unstructured client did not understand object: %T", obj)
	}
	r, err := uc.getResourceInterface(u.GroupVersionKind(), u.GetNamespace())
	if err != nil {
		return err
	}
	deleteOpts := DeleteOptions{}
	err = r.Delete(u.GetName(), deleteOpts.ApplyOptions(opts).AsDeleteOptions())
	return err
}

// Get implements client.Client
func (uc *unstructuredClient) Get(_ context.Context, key ObjectKey, obj runtime.Object) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unstructured client did not understand object: %T", obj)
	}
	r, err := uc.getResourceInterface(u.GroupVersionKind(), key.Namespace)
	if err != nil {
		return err
	}
	i, err := r.Get(key.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	u.Object = i.Object
	return nil
}

// List implements client.Client
func (uc *unstructuredClient) List(_ context.Context, opts *ListOptions, obj runtime.Object) error {
	u, ok := obj.(*unstructured.UnstructuredList)
	if !ok {
		return fmt.Errorf("unstructured client did not understand object: %T", obj)
	}
	gvk := u.GroupVersionKind()
	if strings.HasSuffix(gvk.Kind, "List") {
		gvk.Kind = gvk.Kind[:len(gvk.Kind)-4]
	}
	namespace := ""
	if opts != nil {
		namespace = opts.Namespace
	}
	r, err := uc.getResourceInterface(gvk, namespace)
	if err != nil {
		return err
	}

	i, err := r.List(*opts.AsListOptions())
	if err != nil {
		return err
	}
	u.Items = i.Items
	u.Object = i.Object
	return nil
}

func (uc *unstructuredClient) UpdateStatus(_ context.Context, obj runtime.Object) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unstructured client did not understand object: %T", obj)
	}
	r, err := uc.getResourceInterface(u.GroupVersionKind(), u.GetNamespace())
	if err != nil {
		return err
	}
	i, err := r.UpdateStatus(u, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	u.Object = i.Object
	return nil
}

func (uc *unstructuredClient) getResourceInterface(gvk schema.GroupVersionKind, ns string) (dynamic.ResourceInterface, error) {
	mapping, err := uc.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		return uc.client.Resource(mapping.Resource), nil
	}
	return uc.client.Resource(mapping.Resource).Namespace(ns), nil
}
