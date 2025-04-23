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
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/test/framework/components/echo"
)

var _ cache.Controller = &podController{}

type podHandler func(pod *corev1.Pod) error

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
	// nolint: staticcheck
	_, informer := cache.NewInformer(podListWatch, &corev1.Pod{}, 0, controllers.EventHandler[*corev1.Pod]{
		AddFunc: func(pod *corev1.Pod) {
			q.Push(func() error {
				return handlers.added(pod)
			})
		},
		UpdateFunc: func(old, cur *corev1.Pod) {
			q.Push(func() error {
				if old.GetResourceVersion() != cur.GetResourceVersion() {
					return handlers.updated(cur)
				}
				return nil
			})
		},
		DeleteFunc: func(pod *corev1.Pod) {
			q.Push(func() error {
				return handlers.deleted(pod)
			})
		},
	})

	return &podController{
		q:        q,
		informer: informer,
	}
}

func (c *podController) RunWithContext(ctx context.Context) {
	go c.informer.Run(ctx.Done())
	kube.WaitForCacheSync("pod controller", ctx.Done(), c.informer.HasSynced)
	c.q.Run(ctx.Done())
}

func (c *podController) Run(stop <-chan struct{}) {
	go c.informer.Run(stop)
	kube.WaitForCacheSync("pod controller", stop, c.informer.HasSynced)
	c.q.Run(stop)
}

func (c *podController) HasSynced() bool {
	return c.q.HasSynced()
}

func (c *podController) WaitForSync(stopCh <-chan struct{}) bool {
	return cache.WaitForNamedCacheSync("echo", stopCh, c.informer.HasSynced)
}

func (c *podController) LastSyncResourceVersion() string {
	return c.informer.LastSyncResourceVersion()
}
