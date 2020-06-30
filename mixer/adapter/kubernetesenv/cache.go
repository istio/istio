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

package kubernetesenv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
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
		Workload(*v1.Pod) workload
		HasSynced() bool
		StopControlChannel()
	}

	controllerImpl struct {
		env      adapter.Env
		stopChan chan struct{}
		pods     cache.SharedIndexInformer
		rs       cache.SharedIndexInformer
		rc       cache.SharedIndexInformer
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
func newCacheController(clientset kubernetes.Interface, refreshDuration time.Duration, env adapter.Env, stopChan chan struct{}) cacheController {
	podInformer := env.NewInformer(clientset, &v1.Pod{}, refreshDuration, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				return clientset.CoreV1().Pods(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				return clientset.CoreV1().Pods(namespace).Watch(context.TODO(), opts)
			},
		}
	}, cache.Indexers{
		"ip": podIP,
	})
	replicaSetInformer := env.NewInformer(clientset, &appsv1.ReplicaSet{}, refreshDuration, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				return clientset.AppsV1().ReplicaSets(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				return clientset.AppsV1().ReplicaSets(namespace).Watch(context.TODO(), opts)
			},
		}
	}, cache.Indexers{})
	replicationControllerInformer := env.NewInformer(clientset, &v1.ReplicationController{}, refreshDuration, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				return clientset.CoreV1().ReplicationControllers(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				return clientset.CoreV1().ReplicationControllers(namespace).Watch(context.TODO(), opts)
			},
		}
	}, cache.Indexers{})

	return &controllerImpl{
		env:      env,
		stopChan: stopChan,
		pods:     podInformer,
		rs:       replicaSetInformer,
		rc:       replicationControllerInformer,
	}
}

func (c *controllerImpl) StopControlChannel() {
	close(c.stopChan)
}

func (c *controllerImpl) HasSynced() bool {
	if c.pods.HasSynced() && c.rs.HasSynced() && c.rc.HasSynced() {
		return true
	}
	return false
}

func (c *controllerImpl) Run(stop <-chan struct{}) {
	// TODO: scheduledaemon
	c.env.ScheduleDaemon(func() { c.pods.Run(stop) })
	c.env.ScheduleDaemon(func() { c.rs.Run(stop) })
	c.env.ScheduleDaemon(func() { c.rc.Run(stop) })
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
		var latestPod *v1.Pod
		for i, obj := range objs {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				return nil, false
			}
			// If Pods associated with completed Jobs exist, there can be a case
			// where more than 1 Pod is found during lookup, and we should
			// always pick the latest created Pod out of the lot.
			if i == 0 || latestPod.CreationTimestamp.Before(&pod.CreationTimestamp) {
				latestPod = pod
			}
		}
		return latestPod, true
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

func (c *controllerImpl) Workload(pod *v1.Pod) workload {
	wl := workload{name: pod.Name, namespace: pod.Namespace, selfLinkURL: pod.SelfLink}
	if owner, found := c.rootController(&pod.ObjectMeta); found {
		wl.name = owner.Name
		wl.selfLinkURL = fmt.Sprintf("kubernetes://apis/%s/namespaces/%s/%ss/%s", owner.APIVersion, pod.Namespace, strings.ToLower(owner.Kind), owner.Name)
	}
	wl.uid = "istio://" + wl.namespace + "/workloads/" + wl.name
	return wl
}

func (c *controllerImpl) rootController(obj *metav1.ObjectMeta) (metav1.OwnerReference, bool) {
	for _, ref := range obj.OwnerReferences {
		if *ref.Controller {
			switch ref.Kind {
			case "ReplicaSet":
				indexer := c.rs.GetIndexer()
				if rs, found := c.objectMeta(indexer, key(obj.Namespace, ref.Name)); found {
					if rootRef, ok := c.rootController(rs); ok {
						return rootRef, true
					}
				}
			case "ReplicationController":
				indexer := c.rc.GetIndexer()
				if rc, found := c.objectMeta(indexer, key(obj.Namespace, ref.Name)); found {
					if rootRef, ok := c.rootController(rc); ok {
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
	case *v1.ReplicationController:
		return &v.ObjectMeta, true
	case *appsv1.ReplicaSet:
		return &v.ObjectMeta, true
	}
	return nil, false
}
