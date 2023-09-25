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

package kclient

import (
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/slices"
)

type crdWatcher struct {
	crds      Informer[*metav1.PartialObjectMetadata]
	mutex     sync.RWMutex
	callbacks []func(name string)

	running chan struct{}
	stop    <-chan struct{}
}

func init() {
	// Unfortunate hack needed to avoid circular imports
	kube.NewCrdWatcher = newCrdWatcher
}

// newCrdWatcher returns a new CRD watcher controller.
func newCrdWatcher(client kube.Client) kubetypes.CrdWatcher {
	c := &crdWatcher{
		running: make(chan struct{}),
	}

	c.crds = NewMetadata(client, gvr.CustomResourceDefinition, Filter{})
	c.crds.AddEventHandler(controllers.EventHandler[*metav1.PartialObjectMetadata]{
		AddFunc: func(crd *metav1.PartialObjectMetadata) {
			c.mutex.RLock()
			handlers := slices.Clone(c.callbacks)
			c.mutex.RUnlock()
			for _, handler := range handlers {
				handler(crd.Name)
			}
		},
	})
	return c
}

// HasSynced returns whether the underlying cache has synced and the callback has been called at least once.
func (c *crdWatcher) HasSynced() bool {
	return c.crds.HasSynced()
}

// Run starts the controller. This must be called.
func (c *crdWatcher) Run(stop <-chan struct{}) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	select {
	case <-c.running:
		// already started, ignore
	default:
		c.stop = stop
		close(c.running)
	}
}

// WaitForCRD waits until the request CRD exists, and returns true on success. A false return value
// indicates the CRD does not exist but the wait failed or was canceled.
// This is useful to conditionally enable controllers based on CRDs being created.
func (c *crdWatcher) WaitForCRD(s schema.GroupVersionResource, stop <-chan struct{}) bool {
	done := make(chan struct{})
	if c.KnownOrCallback(s, func(stop <-chan struct{}) {
		close(done)
	}) {
		// Already known
		return true
	}
	select {
	case <-stop:
		return false
	case <-done:
		return true
	}
}

// KnownOrCallback returns `true` immediately if the resource is known.
// If it is not known, `false` is returned. If the resource is later added, the callback will be triggered.
func (c *crdWatcher) KnownOrCallback(s schema.GroupVersionResource, f func(stop <-chan struct{})) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	// We use HasStoreSyncedIgnoringHandlers here to avoid a subtle race condition.
	// If we wait for the handlers to be called, we may have cloned the current callbacks in the handler, but not yet finished yet.
	// In this case, we will add a callback, which will never be triggered later.
	if c.crds.HasStoreSyncedIgnoringHandlers() && c.known(s) {
		// Already known, return early
		return true
	}
	want := fmt.Sprintf("%s.%s", s.Resource, s.Group)
	ran := false
	c.callbacks = append(c.callbacks, func(name string) {
		if want != name {
			// event was for different resource
			return
		}
		if ran {
			// Make sure we only run this once
			return
		}
		ran = true
		// Wait until we are running, so c.stop is set
		<-c.running
		// Call the callback
		f(c.stop)
	})
	return false
}

func (c *crdWatcher) known(s schema.GroupVersionResource) bool {
	// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
	name := fmt.Sprintf("%s.%s", s.Resource, s.Group)
	return c.crds.Get(name, "") != nil
}
