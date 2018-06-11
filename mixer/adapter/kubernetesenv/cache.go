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
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	"k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mixer/pkg/adapter"
)

type (
	// internal interface used to support testing
	cacheController interface {
		Run(<-chan struct{})
		Pod(string) (*v1.Pod, bool)
		Workload(*v1.Pod) (workload, bool)
		HasSynced() bool
	}

	controllerImpl struct {
		env           adapter.Env
		pods          cache.SharedIndexInformer
		appsv1RS      cache.SharedIndexInformer
		appsv1beta2RS cache.SharedIndexInformer
		extv1beta1RS  cache.SharedIndexInformer
	}

	workload struct {
		uid, name, namespace string
		selfLinkURL          string
	}
)

func podIP(obj interface{}) ([]string, error) {
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
// It configures the index informer to list/watch k8sCache and send update events
// to a mutations channel for processing (in this case, logging).
<<<<<<< HEAD
func newCacheController(clientset kubernetes.Interface, refreshDuration time.Duration, env adapter.Env) cacheController {
	return &controllerImpl{
=======
func newCacheController(clientset kubernetes.Interface, refreshDuration time.Duration, is18Cluster bool, env adapter.Env) cacheController {
	namespace := "" // todo: address unparam linter issue

	controller := &controllerImpl{
>>>>>>> Add backwards compat for v1.8.x clusters
		env: env,
		pods: cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
					return clientset.CoreV1().Pods(metav1.NamespaceAll).List(opts)
				},
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					return clientset.CoreV1().Pods(metav1.NamespaceAll).Watch(opts)
				},
			},
			&v1.Pod{},
			refreshDuration,
			cache.Indexers{
				"ip": podIP,
			},
		),
<<<<<<< HEAD
		appsv1RS: cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
					return clientset.AppsV1().ReplicaSets(metav1.NamespaceAll).List(opts)
				},
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					return clientset.AppsV1().ReplicaSets(metav1.NamespaceAll).Watch(opts)
				},
			},
			&appsv1.ReplicaSet{},
			refreshDuration,
			cache.Indexers{},
		),
=======
>>>>>>> Add backwards compat for v1.8.x clusters
		appsv1beta2RS: cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
					return clientset.AppsV1beta2().ReplicaSets(metav1.NamespaceAll).List(opts)
				},
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					return clientset.AppsV1beta2().ReplicaSets(metav1.NamespaceAll).Watch(opts)
				},
			},
			&appsv1beta2.ReplicaSet{},
			refreshDuration,
			cache.Indexers{},
		),
		extv1beta1RS: cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
					return clientset.ExtensionsV1beta1().ReplicaSets(metav1.NamespaceAll).List(opts)
				},
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					return clientset.ExtensionsV1beta1().ReplicaSets(metav1.NamespaceAll).Watch(opts)
				},
			},
			&extv1beta1.ReplicaSet{},
			refreshDuration,
			cache.Indexers{},
		),
	}

	if !is18Cluster {
		controller.appsv1RS = cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
					return clientset.AppsV1().ReplicaSets(namespace).List(opts)
				},
				WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
					return clientset.AppsV1().ReplicaSets(namespace).Watch(opts)
				},
			},
			&appsv1.ReplicaSet{},
			refreshDuration,
			cache.Indexers{},
		)
	}

	return controller
}

func (c *controllerImpl) HasSynced() bool {
	return c.pods.HasSynced() &&
		c.appsv1beta2RS.HasSynced() &&
		c.extv1beta1RS.HasSynced() &&
		(c.appsv1RS != nil && c.appsv1RS.HasSynced())
}

func (c *controllerImpl) Run(stop <-chan struct{}) {
	// TODO: scheduledaemon
	c.env.ScheduleDaemon(func() { c.pods.Run(stop) })
	c.env.ScheduleDaemon(func() { c.appsv1beta2RS.Run(stop) })
	c.env.ScheduleDaemon(func() { c.extv1beta1RS.Run(stop) })
	if c.appsv1RS != nil {
		c.env.ScheduleDaemon(func() { c.appsv1RS.Run(stop) })
	}
	<-stop
	// TODO: logging?
}

// Pod returns a k8s Pod object that corresponds to the supplied key, if one
// exists (and is known to the store). Keys are expected in the form of:
// namespace/name or IP address (example: "default/curl-2421989462-b2g2d.default").
func (c *controllerImpl) Pod(podKey string) (*v1.Pod, bool) {
	indexer := c.pods.GetIndexer()
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

func (c *controllerImpl) Workload(pod *v1.Pod) (workload, bool) {
	wl := workload{name: pod.Name, namespace: pod.Namespace, selfLinkURL: pod.SelfLink}
	if owner, found := c.rootController(&pod.ObjectMeta); found {
		wl.name = owner.Name
		wl.selfLinkURL = fmt.Sprintf("kubernetes://apis/%s/namespaces/%s/%ss/%s", owner.APIVersion, pod.Namespace, strings.ToLower(owner.Kind), owner.Name)
	}
	wl.uid = "istio://" + wl.namespace + "/workloads/" + wl.name
	return wl, true
}

func (c *controllerImpl) rootController(obj *metav1.ObjectMeta) (metav1.OwnerReference, bool) {
	for _, ref := range obj.OwnerReferences {
		if *ref.Controller {
			switch ref.Kind {
			case "ReplicaSet":
				var indexer cache.Indexer
				switch ref.APIVersion {
				case "extensions/v1beta1":
					indexer = c.extv1beta1RS.GetIndexer()
				case "apps/v1beta2":
					indexer = c.appsv1beta2RS.GetIndexer()
				default:
					indexer = c.appsv1RS.GetIndexer()
				}
				if rs, found := c.objectMeta(indexer, key(obj.Namespace, ref.Name)); found {
					if rootRef, ok := c.rootController(rs); ok {
						return rootRef, true
					}
				}
			}
			return ref, true
		}
	}
	return metav1.OwnerReference{}, false
}

func (c *controllerImpl) objectMeta(keyGetter cache.KeyGetter, key string) (*metav1.ObjectMeta, bool) {
	item, exists, err := keyGetter.GetByKey(key)
	if !exists || err != nil {
		return nil, false
	}
	switch v := item.(type) {
	case *appsv1.ReplicaSet:
		return &v.ObjectMeta, true
	case *appsv1beta2.ReplicaSet:
		return &v.ObjectMeta, true
	case *extv1beta1.ReplicaSet:
		return &v.ObjectMeta, true
	}
	return nil, false
}
