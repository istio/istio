// Copyright 2018 The Operator-SDK Authors
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

package test

import (
	goctx "context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	dynclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type frameworkClient struct {
	dynclient.Client
}

var _ FrameworkClient = &frameworkClient{}

type FrameworkClient interface {
	Get(gCtx goctx.Context, key dynclient.ObjectKey, obj runtime.Object) error
	List(gCtx goctx.Context, opts *dynclient.ListOptions, list runtime.Object) error
	Create(gCtx goctx.Context, obj runtime.Object, cleanupOptions *CleanupOptions) error
	Delete(gCtx goctx.Context, obj runtime.Object, opts ...dynclient.DeleteOptionFunc) error
	Update(gCtx goctx.Context, obj runtime.Object) error
}

// Create uses the dynamic client to create an object and then adds a
// cleanup function to delete it when Cleanup is called. In addition to
// the standard controller-runtime client options
func (f *frameworkClient) Create(gCtx goctx.Context, obj runtime.Object, cleanupOptions *CleanupOptions) error {
	objCopy := obj.DeepCopyObject()
	err := f.Client.Create(gCtx, obj)
	if err != nil {
		return err
	}
	// if no test context exists, cannot add finalizer function or print to testing log
	if cleanupOptions == nil || cleanupOptions.TestContext == nil {
		return nil
	}
	key, err := dynclient.ObjectKeyFromObject(objCopy)
	// this function fails silently if t is nil
	if cleanupOptions.TestContext.t != nil {
		cleanupOptions.TestContext.t.Logf("resource type %+v with namespace/name (%+v) created\n", objCopy.GetObjectKind().GroupVersionKind().Kind, key)
	}
	cleanupOptions.TestContext.AddCleanupFn(func() error {
		err = f.Client.Delete(gCtx, objCopy)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if cleanupOptions.Timeout == 0 && cleanupOptions.RetryInterval != 0 {
			return fmt.Errorf("retry interval is set but timeout is not; cannot poll for cleanup")
		} else if cleanupOptions.Timeout != 0 && cleanupOptions.RetryInterval == 0 {
			return fmt.Errorf("timeout is set but retry interval is not; cannot poll for cleanup")
		}
		if cleanupOptions.Timeout != 0 && cleanupOptions.RetryInterval != 0 {
			return wait.PollImmediate(cleanupOptions.RetryInterval, cleanupOptions.Timeout, func() (bool, error) {
				err = f.Client.Get(gCtx, key, objCopy)
				if err != nil {
					if apierrors.IsNotFound(err) {
						if cleanupOptions.TestContext.t != nil {
							cleanupOptions.TestContext.t.Logf("resource type %+v with namespace/name (%+v) successfully deleted\n", objCopy.GetObjectKind().GroupVersionKind().Kind, key)
						}
						return true, nil
					}
					return false, fmt.Errorf("error encountered during deletion of resource type %v with namespace/name (%+v): %v", objCopy.GetObjectKind().GroupVersionKind().Kind, key, err)
				}
				if cleanupOptions.TestContext.t != nil {
					cleanupOptions.TestContext.t.Logf("waiting for deletion of resource type %+v with namespace/name (%+v)\n", objCopy.GetObjectKind().GroupVersionKind().Kind, key)
				}
				return false, nil
			})
		}
		return nil
	})
	return nil
}

func (f *frameworkClient) Get(gCtx goctx.Context, key dynclient.ObjectKey, obj runtime.Object) error {
	return f.Client.Get(gCtx, key, obj)
}

func (f *frameworkClient) List(gCtx goctx.Context, opts *dynclient.ListOptions, list runtime.Object) error {
	return f.Client.List(gCtx, opts, list)
}

func (f *frameworkClient) Delete(gCtx goctx.Context, obj runtime.Object, opts ...dynclient.DeleteOptionFunc) error {
	return f.Client.Delete(gCtx, obj, opts...)
}

func (f *frameworkClient) Update(gCtx goctx.Context, obj runtime.Object) error {
	return f.Client.Update(gCtx, obj)
}
