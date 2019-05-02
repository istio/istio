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
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Options are creation options for a Client
type Options struct {
	// Scheme, if provided, will be used to map go structs to GroupVersionKinds
	Scheme *runtime.Scheme

	// Mapper, if provided, will be used to map GroupVersionKinds to Resources
	Mapper meta.RESTMapper
}

// New returns a new Client using the provided config and Options.
func New(config *rest.Config, options Options) (Client, error) {
	if config == nil {
		return nil, fmt.Errorf("must provide non-nil rest.Config to client.New")
	}

	// Init a scheme if none provided
	if options.Scheme == nil {
		options.Scheme = scheme.Scheme
	}

	// Init a Mapper if none provided
	if options.Mapper == nil {
		var err error
		options.Mapper, err = apiutil.NewDiscoveryRESTMapper(config)
		if err != nil {
			return nil, err
		}
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	c := &client{
		typedClient: typedClient{
			cache: clientCache{
				config:         config,
				scheme:         options.Scheme,
				mapper:         options.Mapper,
				codecs:         serializer.NewCodecFactory(options.Scheme),
				resourceByType: make(map[reflect.Type]*resourceMeta),
			},
			paramCodec: runtime.NewParameterCodec(options.Scheme),
		},
		unstructuredClient: unstructuredClient{
			client:     dynamicClient,
			restMapper: options.Mapper,
		},
	}

	return c, nil
}

var _ Client = &client{}

// client is a client.Client that reads and writes directly from/to an API server.  It lazily initializes
// new clients at the time they are used, and caches the client.
type client struct {
	typedClient        typedClient
	unstructuredClient unstructuredClient
}

// Create implements client.Client
func (c *client) Create(ctx context.Context, obj runtime.Object) error {
	_, ok := obj.(*unstructured.Unstructured)
	if ok {
		return c.unstructuredClient.Create(ctx, obj)
	}
	return c.typedClient.Create(ctx, obj)
}

// Update implements client.Client
func (c *client) Update(ctx context.Context, obj runtime.Object) error {
	_, ok := obj.(*unstructured.Unstructured)
	if ok {
		return c.unstructuredClient.Update(ctx, obj)
	}
	return c.typedClient.Update(ctx, obj)
}

// Delete implements client.Client
func (c *client) Delete(ctx context.Context, obj runtime.Object, opts ...DeleteOptionFunc) error {
	_, ok := obj.(*unstructured.Unstructured)
	if ok {
		return c.unstructuredClient.Delete(ctx, obj, opts...)
	}
	return c.typedClient.Delete(ctx, obj, opts...)
}

// Get implements client.Client
func (c *client) Get(ctx context.Context, key ObjectKey, obj runtime.Object) error {
	_, ok := obj.(*unstructured.Unstructured)
	if ok {
		return c.unstructuredClient.Get(ctx, key, obj)
	}
	return c.typedClient.Get(ctx, key, obj)
}

// List implements client.Client
func (c *client) List(ctx context.Context, opts *ListOptions, obj runtime.Object) error {
	_, ok := obj.(*unstructured.UnstructuredList)
	if ok {
		return c.unstructuredClient.List(ctx, opts, obj)
	}
	return c.typedClient.List(ctx, opts, obj)
}

// Status implements client.StatusClient
func (c *client) Status() StatusWriter {
	return &statusWriter{client: c}
}

// statusWriter is client.StatusWriter that writes status subresource
type statusWriter struct {
	client *client
}

// ensure statusWriter implements client.StatusWriter
var _ StatusWriter = &statusWriter{}

// Update implements client.StatusWriter
func (sw *statusWriter) Update(ctx context.Context, obj runtime.Object) error {
	_, ok := obj.(*unstructured.Unstructured)
	if ok {
		return sw.client.unstructuredClient.UpdateStatus(ctx, obj)
	}
	return sw.client.typedClient.UpdateStatus(ctx, obj)
}
