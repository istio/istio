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

package repair

import (
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/kube"
)

type Controller struct {
	clientset     client.Interface
	workQueue     workqueue.RateLimitingInterface
	podController cache.Controller

	reconciler brokenPodReconciler
}

func NewRepairController(reconciler brokenPodReconciler) (*Controller, error) {
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

			for _, ls := range []string{options.LabelSelector, reconciler.cfg.LabelSelectors} {
				if ls != "" {
					labelSelectors = append(labelSelectors, ls)
				}
			}
			for _, fs := range []string{options.FieldSelector, reconciler.cfg.FieldSelectors} {
				if fs != "" {
					fieldSelectors = append(fieldSelectors, fs)
				}
			}
			// filter out pod events from different nodes
			fieldSelectors = append(fieldSelectors, fmt.Sprintf("spec.nodeName=%v", reconciler.cfg.NodeName))
			options.LabelSelector = strings.Join(labelSelectors, ",")
			options.FieldSelector = strings.Join(fieldSelectors, ",")
		},
	)

	_, c.podController = cache.NewInformer(podListWatch, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj any) {
			c.mayAddToWorkQueue(newObj)
		},
		UpdateFunc: func(_, newObj any) {
			c.mayAddToWorkQueue(newObj)
		},
	})

	return c, nil
}

func (rc *Controller) mayAddToWorkQueue(obj any) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		repairLog.Error("Cannot convert object to pod. Skip adding it to the repair working queue.")
		return
	}
	if rc.reconciler.detectPod(*pod) {
		rc.workQueue.AddRateLimited(obj)
	}
}

func (rc *Controller) Run(stopCh <-chan struct{}) {
	go rc.podController.Run(stopCh)
	if !kube.WaitForCacheSync(stopCh, rc.podController.HasSynced) {
		repairLog.Error("timed out waiting for pod caches to sync")
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
		repairLog.Errorf("Error decoding object, invalid type. Dropping.")
		rc.workQueue.Forget(obj)
		// Short-circuit on this item, but return true to keep
		// processing.
		return true
	}

	err := rc.reconciler.ReconcilePod(*pod)

	if err == nil {
		repairLog.Debugf("Removing %s/%s from work queue", pod.Namespace, pod.Name)
		rc.workQueue.Forget(obj)
	} else if rc.workQueue.NumRequeues(obj) < 50 {
		if strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version and try again") {
			repairLog.Debugf("Object '%s/%s' modified, requeue for retry", pod.Namespace, pod.Name)
			repairLog.Infof("Re-adding %s/%s to work queue", pod.Namespace, pod.Name)
			rc.workQueue.AddRateLimited(obj)
		} else if strings.Contains(err.Error(), "not found") {
			repairLog.Debugf("Object '%s/%s' removed, dequeue", pod.Namespace, pod.Name)
			rc.workQueue.Forget(obj)
		} else {
			repairLog.Errorf("Error: %s", err)
			repairLog.Infof("Re-adding %s/%s to work queue", pod.Namespace, pod.Name)
			rc.workQueue.AddRateLimited(obj)
		}
	} else {
		repairLog.Infof("Requeue limit reached, removing %s/%s", pod.Namespace, pod.Name)
		rc.workQueue.Forget(obj)
		runtime.HandleError(err)
	}

	// Return true to let the loop process the next item
	return true
}
