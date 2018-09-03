/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controller

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"reflect"

	"istio.io/istio/pkg/api/rpccontroller.istio.io/v1"
	"istio.io/istio/pkg/log"
	clientset "istio.io/istio/pkg/rpccontroller/clientset/versioned"
	watcherscheme "istio.io/istio/pkg/rpccontroller/clientset/versioned/scheme"
	informers "istio.io/istio/pkg/rpccontroller/informers/externalversions/rpccontroller.istio.io/v1"
	listers "istio.io/istio/pkg/rpccontroller/listers/rpccontroller.istio.io/v1"
)

const controllerAgentName = "rpc-controller"

// Controller is the controller implementation for Foo resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// controllerclientset is a clientset for our own API group
	controllerclientset clientset.Interface

	rpcServiceLister listers.RpcServiceLister
	rpcServiceSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	rpcWatcher *rpcWatcher

	stopCh <-chan struct{}
}

// NewController for create controller struct
func NewController(
	kubeclientset kubernetes.Interface,
	controllerclientset clientset.Interface,
	rsInformer informers.RpcServiceInformer,
	config *Config,
	stopCh <-chan struct{}) *Controller {
	// Create event broadcaster
	// Add rpc-controller types to the default Kubernetes Scheme so Events can be
  err := watcherscheme.AddToScheme(scheme.Scheme); if err != nil {
    panic(err)
  }
	log.Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:       kubeclientset,
		controllerclientset: controllerclientset,
		rpcServiceLister:    rsInformer.Lister(),
		rpcServiceSynced:    rsInformer.Informer().HasSynced,
		workqueue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "rpc controller"),
		recorder:            recorder,
		stopCh:              stopCh,
	}

	log.Info("Setting up event handlers")
	// Set up an event handler for when Foo resources change
	rsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueue,
		UpdateFunc: func(old, new interface{}) {
			log.Debugf("old: %v, new: %v", old, new)
			if !reflect.DeepEqual(old, new) {
				controller.enqueue(new)
			}
		},
		DeleteFunc: controller.deleteRPCService,
	})

	controller.rpcWatcher = newRPCWatcher(controller.rpcServiceLister, controller.kubeclientset, config, stopCh)

	return controller
}

func (c *Controller) deleteRPCService(obj interface{}) {
	rs, ok := obj.(*v1.RpcService)
	if !ok {
		return
	}

	c.rpcWatcher.Delete(rs)
}

func (c *Controller) enqueue(obj interface{}) {
	c.workqueue.AddRateLimited(obj)
}

// Run is controller's main routine
func (c *Controller) Run(threadiness int) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting rpc controller")

	c.rpcWatcher.Run(c.stopCh)

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(c.stopCh, c.rpcServiceSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log.Info("Starting workers")
	// Launch two workers to process Foo resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, c.stopCh)
	}

	log.Info("Started workers")
	<-c.stopCh
	log.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)

		var rs *v1.RpcService
		var ok bool
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if rs, ok = obj.(*v1.RpcService); !ok {
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(rs); err != nil {
			return fmt.Errorf("error syncing '%v': %s", obj, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Foo resource
// with the current status of the resource.
func (c *Controller) syncHandler(rs *v1.RpcService) error {
	c.rpcWatcher.Sync(rs)

	return nil
}
