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
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
)

type crdWatcher struct {
	crds      Informer[*metav1.PartialObjectMetadata]
	queue     controllers.Queue
	mutex     sync.RWMutex
	callbacks map[string][]func()

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
		running:   make(chan struct{}),
		callbacks: map[string][]func(){},
	}

	c.queue = controllers.NewQueue("crd watcher",
		controllers.WithReconciler(c.Reconcile))
	c.crds = NewMetadata(client, gvr.CustomResourceDefinition, Filter{})
	c.crds.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))
	return c
}

// HasSynced returns whether the underlying cache has synced and the callback has been called at least once.
func (c *crdWatcher) HasSynced() bool {
	return c.queue.HasSynced()
}

// Run starts the controller. This must be called.
func (c *crdWatcher) Run(stop <-chan struct{}) {
	c.mutex.Lock()
	if c.stop != nil {
		// Run already called. Because we call this from client.RunAndWait this isn't uncommon
		c.mutex.Unlock()
		return
	}
	c.stop = stop
	c.mutex.Unlock()
	kube.WaitForCacheSync("crd watcher", stop, c.crds.HasSynced)
	c.queue.Run(stop)
	c.crds.ShutdownHandlers()
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
	// If we are already synced, return immediately if the CRD is present.
	if c.crds.HasSynced() && c.known(s) {
		// Already known, return early
		return true
	}
	name := fmt.Sprintf("%s.%s", s.Resource, s.Group)
	c.callbacks[name] = append(c.callbacks[name], func() {
		if features.EnableUnsafeAssertions && c.stop == nil {
			log.Fatalf("CRD Watcher callback called without stop set")
		}
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

func (c *crdWatcher) Reconcile(key types.NamespacedName) error {
	c.mutex.Lock()
	callbacks, f := c.callbacks[key.Name]
	if !f {
		c.mutex.Unlock()
		return nil
	}
	// Delete them so we do not run again
	delete(c.callbacks, key.Name)
	c.mutex.Unlock()
	for _, cb := range callbacks {
		cb()
	}
	return nil
}
