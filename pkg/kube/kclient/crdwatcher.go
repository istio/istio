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

	"github.com/Masterminds/semver/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api/pkg/consts"

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
	c.crds = NewMetadata(client, gvr.CustomResourceDefinition, Filter{
		ObjectFilter: kubetypes.NewStaticObjectFilter(minimumVersionFilter),
	})
	c.crds.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))
	return c
}

var minimumCRDVersions = map[string]*semver.Version{
	"grpcroutes.gateway.networking.k8s.io": semver.New(1, 1, 0, "", ""),
}

// minimumVersionFilter filters CRDs that do not meet a minimum "version".
// Currently, we use this only for Gateway API CRD's, so we hardcode their versioning scheme.
// The problem we are trying to solve is:
// * User installs CRDs with Foo v1alpha1
// * Istio vNext starts watching Foo at v1
// * user upgrades to Istio vNext. It sees Foo exists, and tries to watch v1. This fails.
// The user may have opted into using an experimental CRD, but not to experimental usage *in Istio* so this isn't acceptable.
func minimumVersionFilter(t any) bool {
	// Setup a filter
	crd := t.(*metav1.PartialObjectMetadata)
	mv, f := minimumCRDVersions[crd.Name]
	if !f {
		return true
	}
	bv, f := crd.Annotations[consts.BundleVersionAnnotation]
	if !f {
		log.Errorf("CRD %v expected to have a %v annotation, but none found; ignoring", crd.Name, consts.BundleVersion)
		return false
	}
	fv, err := semver.NewVersion(bv)
	if err != nil {
		log.Errorf("CRD %v version %v invalid; ignoring: %v", crd.Name, bv, err)
		return false
	}
	// Ignore RC tags, etc. We 'round up' those.
	nv, err := fv.SetPrerelease("")
	if err != nil {
		log.Errorf("CRD %v version %v invalid; ignoring: %v", crd.Name, bv, err)
		return false
	}
	fv = &nv
	if fv.LessThan(mv) {
		log.Infof("CRD %v version %v is below minimum version %v, ignoring", crd.Name, fv, mv)
		return false
	}
	return true
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
	log.Info("Stopping CRD watcher")
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
			log.Fatal("CRD Watcher callback called without stop set")
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
