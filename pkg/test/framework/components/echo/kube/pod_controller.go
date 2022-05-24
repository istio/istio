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

package kube

import (
	"time"

	kubeCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/pkg/log"
)

var _ cache.Controller = &podController{}

type podHandler func(pod *kubeCore.Pod) error

type podHandlers struct {
	added   podHandler
	updated podHandler
	deleted podHandler
}

type podController struct {
	q        queue.Instance
	informer cache.Controller
}

func newPodController(cfg echo.Config, handlers podHandlers) *podController {
	s := newPodSelector(cfg)
	podListWatch := cache.NewFilteredListWatchFromClient(cfg.Cluster.Kube().CoreV1().RESTClient(),
		"pods",
		cfg.Namespace.Name(),
		func(options *metav1.ListOptions) {
			if len(options.LabelSelector) > 0 {
				options.LabelSelector += ","
			}
			options.LabelSelector += s.String()
		})
	q := queue.NewQueue(1 * time.Second)
	_, informer := cache.NewInformer(podListWatch, &kubeCore.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			q.Push(func() error {
				return handlers.added(newObj.(*kubeCore.Pod))
			})
		},
		UpdateFunc: func(old, cur interface{}) {
			q.Push(func() error {
				oldObj := old.(metav1.Object)
				newObj := cur.(metav1.Object)

				if oldObj.GetResourceVersion() != newObj.GetResourceVersion() {
					return handlers.updated(newObj.(*kubeCore.Pod))
				}
				return nil
			})
		},
		DeleteFunc: func(curr interface{}) {
			q.Push(func() error {
				pod, ok := curr.(*kubeCore.Pod)
				if !ok {
					tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
					if !ok {
						log.Errorf("Couldn't get object from tombstone %#v", curr)
						return nil
					}
					pod, ok = tombstone.Obj.(*kubeCore.Pod)
					if !ok {
						log.Errorf("Tombstone contained object that is not a pod %#v", curr)
						return nil
					}
				}
				return handlers.deleted(pod)
			})
		},
	})

	return &podController{
		q:        q,
		informer: informer,
	}
}

func (c *podController) Run(stop <-chan struct{}) {
	go c.informer.Run(stop)
	kube.WaitForCacheSync(stop, c.HasSynced)
	go c.q.Run(stop)
}

func (c *podController) HasSynced() bool {
	return c.informer.HasSynced()
}

func (c *podController) WaitForSync(stopCh <-chan struct{}) bool {
	return cache.WaitForNamedCacheSync("echo", stopCh, c.informer.HasSynced)
}

func (c *podController) LastSyncResourceVersion() string {
	return c.informer.LastSyncResourceVersion()
}
