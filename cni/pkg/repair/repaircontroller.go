// Copyright 2020 Istio Authors
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

package repair

import (
	"fmt"
	"strings"
	"time"

	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type Controller struct {
	clientset     client.Interface
	workQueue     workqueue.RateLimitingInterface
	podController cache.Controller

	reconciler BrokenPodReconciler
}

func NewRepairController(reconciler BrokenPodReconciler) (*Controller, error) {
	c := &Controller{
		clientset:  reconciler.client,
		workQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		reconciler: reconciler,
	}

	podListWatch := cache.NewFilteredListWatchFromClient(
		c.clientset.CoreV1().RESTClient(),
		"pods",
		metav1.NamespaceAll,
		func(options *metav1.ListOptions) {
			var (
				labelSelectors []string
				fieldSelectors []string
			)

			for _, ls := range []string{options.LabelSelector, reconciler.Filters.LabelSelectors} {
				if ls != "" {
					labelSelectors = append(labelSelectors, ls)
				}
			}
			for _, fs := range []string{options.FieldSelector, reconciler.Filters.FieldSelectors} {
				if fs != "" {
					fieldSelectors = append(fieldSelectors, fs)
				}
			}
			options.LabelSelector = strings.Join(labelSelectors, ",")
			options.FieldSelector = strings.Join(fieldSelectors, ",")
		},
	)

	_, c.podController = cache.NewInformer(podListWatch, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			c.workQueue.AddRateLimited(newObj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.workQueue.AddRateLimited(newObj)
		},
	})

	return c, nil
}

func (rc *Controller) Run(stopCh <-chan struct{}) {
	go rc.podController.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, rc.podController.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	// This will run the func every 1 second until stopCh is sent
	go wait.Until(
		func() {
			// Runs processNextItem in a loop, if it returns false it will
			// be restarted by wait.Until unless stopCh is sent.
			for rc.processNextItem() {
			}
		},
		time.Second,
		stopCh,
	)

	<-stopCh
	log.Infof("Stopping repair controller.")
}

// Process the next available item in the work queue.
// Return false if exiting permanently, else return true
// so the loop keeps processing.
func (rc *Controller) processNextItem() bool {
	obj, quit := rc.workQueue.Get()
	if quit {
		// Exit permanently
		return false
	}
	defer rc.workQueue.Done(obj)

	pod, ok := obj.(*v1.Pod)
	if !ok {
		log.Errorf("Error decoding object, invalid type. Dropping.")
		rc.workQueue.Forget(obj)
		// Short-circuit on this item, but return true to keep
		// processing.
		return true
	}

	err := rc.reconciler.ReconcilePod(*pod)

	if err == nil {
		log.Debugf("Removing %s/%s from work queue", pod.Namespace, pod.Name)
		rc.workQueue.Forget(obj)
	} else if rc.workQueue.NumRequeues(obj) < 50 {
		if strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version and try again") {
			log.Debugf("Object '%s/%s' modified, requeue for retry", pod.Namespace, pod.Name)
			log.Infof("Re-adding %s/%s to work queue", pod.Namespace, pod.Name)
			rc.workQueue.AddRateLimited(obj)
		} else if strings.Contains(err.Error(), "not found") {
			log.Debugf("Object '%s/%s' removed, dequeue", pod.Namespace, pod.Name)
			rc.workQueue.Forget(obj)
		} else {
			log.Errorf("Error: %s", err)
			log.Infof("Re-adding %s/%s to work queue", pod.Namespace, pod.Name)
			rc.workQueue.AddRateLimited(obj)
		}
	} else {
		log.Infof("Requeue limit reached, removing %s/%s", pod.Namespace, pod.Name)
		rc.workQueue.Forget(obj)
		runtime.HandleError(err)
	}

	// Return true to let the loop process the next item
	return true
}
