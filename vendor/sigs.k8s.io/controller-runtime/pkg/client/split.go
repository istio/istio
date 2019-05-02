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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// DelegatingClient forms an interface Client by composing separate
// reader, writer and statusclient interfaces.  This way, you can have an Client that
// reads from a cache and writes to the API server.
type DelegatingClient struct {
	Reader
	Writer
	StatusClient
}

// DelegatingReader forms a interface Reader that will cause Get and List
// requests for unstructured types to use the ClientReader while
// requests for any other type of object with use the CacheReader.
type DelegatingReader struct {
	CacheReader  Reader
	ClientReader Reader
}

// Get retrieves an obj for a given object key from the Kubernetes Cluster.
func (d *DelegatingReader) Get(ctx context.Context, key ObjectKey, obj runtime.Object) error {
	_, isUnstructured := obj.(*unstructured.Unstructured)
	if isUnstructured {
		return d.ClientReader.Get(ctx, key, obj)
	}
	return d.CacheReader.Get(ctx, key, obj)
}

// List retrieves list of objects for a given namespace and list options.
func (d *DelegatingReader) List(ctx context.Context, opts *ListOptions, list runtime.Object) error {
	_, isUnstructured := list.(*unstructured.UnstructuredList)
	if isUnstructured {
		return d.ClientReader.List(ctx, opts, list)
	}
	return d.CacheReader.List(ctx, opts, list)
}
