// Copyright 2017 Istio Authors.
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

package kubernetesenv

import (
	"errors"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type (
	// internal interface used to support testing
	cacheController interface {
		Run(<-chan struct{})
		GetPod(string) (*v1.Pod, bool)
		HasSynced() bool
	}

	controllerImpl struct {
		cache.SharedIndexInformer
	}
)

func ipIndex(obj interface{}) ([]string, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return nil, errors.New("object is not a pod")
	}
	ip := pod.Status.PodIP
	if ip == "" {
		return nil, nil
	}
	return []string{ip}, nil
}

// Responsible for setting up the cacheController, based on the supplied client.
// It configures the index informer to list/watch pods and send update events
// to a mutations channel for processing (in this case, logging).
func newCacheController(clientset kubernetes.Interface, refreshDuration time.Duration) cacheController {
	namespace := "" // todo: address unparam linter issue

	return &controllerImpl{
		cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
					return clientset.CoreV1().Pods(namespace).List(opts)
				},
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					return clientset.CoreV1().Pods(namespace).Watch(opts)
				},
			},
			&v1.Pod{},
			refreshDuration,
			cache.Indexers{
				"ip": ipIndex,
			},
		),
	}
}

// GetPod returns a Pod object that corresponds to the supplied key, if one
// exists (and is known to the store). Keys are expected in the form of:
// namespace/name or IP address (example: "default/curl-2421989462-b2g2d.default").
func (c *controllerImpl) GetPod(podKey string) (*v1.Pod, bool) {
	indexer := c.GetIndexer()
	objs, err := indexer.ByIndex("ip", podKey)
	if err != nil {
		return nil, false
	}
	if len(objs) > 0 {
		pod, ok := objs[0].(*v1.Pod)
		if !ok {
			return nil, false
		}
		return pod, true
	}
	item, exists, err := indexer.GetByKey(podKey)
	if !exists || err != nil {
		return nil, false
	}
	return item.(*v1.Pod), true
}

func key(namespace, name string) string {
	return namespace + "/" + name
}
