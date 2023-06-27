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

package controllers_test

import (
	"fmt"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/retry"
)

// Controller represents a simple example controller show best practices for correctly writing a controller
// in Istio.
// In this example, we simply print all pods.
type Controller struct {
	pods   kclient.Client[*corev1.Pod]
	queue  controllers.Queue
	events *atomic.Int32
}

// NewController creates a controller instance. A controller should typically take in a kube.Client,
// and optionally whatever other configuration is needed. When a large number of options are needed,
// prefer a struct as input.
func NewController(cl kube.Client) *Controller {
	c := &Controller{events: atomic.NewInt32(0)}
	// For each thing we watch, build a typed `client`. This encapsulates a variety of complex logic
	// that would otherwise need to be dealt with manually with Informers and Listers.
	// In general, you should *not* ever do a direct List or Get call to the api-server.
	// Each kube.Client (which, there should be one per cluster) has a shared set of watches to the API server,
	// so two controllers watching Pods will only open a single watch to the API server.
	c.pods = kclient.New[*corev1.Pod](cl)
	// Some other examples:
	// filteredPods := client.NewFiltered[*corev1.Pod](c, kclient.Filter{ObjectFilter: options.GetFilter()})
	// singlePod := client.NewFiltered[*corev1.Pod](c, kclient.Filter{FieldSelector: "metadata.name=my-pod})

	// Establish a queue. This ensures that:
	// * our event handlers are fast (simply enqueuing an item)
	// * if we have multiple resources watched, they are not handled in parallel
	// * errors can be retried
	c.queue = controllers.NewQueue("pods",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))

	// Register a handler for pods. For each pod event, we will add it to the queue.
	c.pods.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))
	return c
}

// Reconcile is the main work function for our controller. As input, we get a name of an object; with
// this we should drive the rest of the state of the world.
func (c *Controller) Reconcile(key types.NamespacedName) error {
	// Its often useful to shadow the log one with additional K/V context
	log := log.WithLabels("resource", key)
	pod := c.pods.Get(key.Name, key.Namespace)
	if pod == nil {
		log.Infof("pod deleted")
	} else {
		c.events.Inc()
		fmt.Println("pod has IP", pod.Status.PodIP) // Just for our test, normally use log.Info
		log.Infof("pod has IP %v", pod.Status.PodIP)
	}
	// We never have an error for this controller.
	// If we did, it would be retried (with backoff), based on our controllers.WithMaxAttempts argument.
	return nil
}

// Run is called after New() but before HasSynced(). This separation allows building up the controllers
// without yet running them.
// Run should block.
func (c *Controller) Run(stop <-chan struct{}) {
	// The details here are subtle but important to get right. We want to mark our controller ready
	// only after it has processed the state of the world when we started. That is, we must have
	// Reconciled() each Pod. The queueing introduces some indirection, though - we want to make sure
	// we have fully processed each item, not simply added it to the Queue. The queue exposes a
	// HasSynced method that returns true once all items added before queue.Run() was called are complete.
	// This means we must populate the initial state into the queue *before* we run it.

	// First, wait for pods to sync. Once this is complete, we know the event handler for Pods will have
	// ran for each item and enqueued everything.
	kube.WaitForCacheSync("pod controller", stop, c.pods.HasSynced)

	// Now we can run the queue. This will block until `stop` is closed.
	c.queue.Run(stop)
	// Unregister our handler. This ensures it does not continue to run if the informer is still running
	// after the controller exits.
	// This is typically only needed for leader election controllers, as otherwise the controller lifecycle
	// is typically the same as the informer.
	c.pods.ShutdownHandlers()
}

// HasSynced asserts we have "synced", meaning we have processed the initial state.
func (c *Controller) HasSynced() bool {
	// We could check `c.pods` as well, but it is redundant due to the Run() implementation.
	// Instead, just check `c.queue`.
	return c.queue.HasSynced()
}

// nolint: gocritic
func Example() {
	// Setup our fake client. This can be pre-populated with items.
	c := kube.NewFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test1"},
		Status:     corev1.PodStatus{PodIP: "127.0.0.1"},
	})
	// When the test is done, terminate the client. This ensures all informers are closed.
	// This usually doesn't matter, but can be useful when mutating global state with test.SetForTest, etc.
	defer c.Shutdown() // Normally: t.Cleanup(c.Shutdown)

	// Build our controller
	controller := NewController(c)
	// Ensure our queue finished processing events
	defer func() {
		// Normally: assert.NoError(t, ...)
		controller.queue.WaitForClose(time.Second)
	}()

	stop := make(chan struct{}) // Normally: test.NewStop(t), in tests
	defer func() { close(stop) }()

	// Note: the order of the defer/t.Cleanup matters
	// We should close the stop (which will start the queue and informers shutdown process)
	// then wait for the queue and informers to shutdown

	// *After* we build the controller, we need to start all informers. The order here matters, as NewController
	// if the thing that registers them.
	c.RunAndWait(stop)

	// Now run our controller (in goroutine)
	go controller.Run(stop)
	// Wait for it to be ready
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	// In a test, we can also use a wrapped client that calls t.Fatal on errors
	// pods := clienttest.Wrap(t, controller.pods)
	_, _ = controller.pods.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test2"},
		Status:     corev1.PodStatus{PodIP: "127.0.0.2"},
	})
	// There is no guarantee that test2 would be processed before the controller exits, so wait for events insert
	_ = retry.Until(func() bool {
		return controller.events.Load() == 2
	})
	// In a typical test, using helpers like assert.EventuallyEqual or retry.UntilSuccessOrFail are preferred
	// assert.EventuallyEqual(t, controller.events.Load, 2)

	// Output:
	// pod has IP 127.0.0.1
	// pod has IP 127.0.0.2
}
