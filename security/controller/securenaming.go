// Copyright 2017 Istio Authors
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

// TODO: describes what this module is for and comments the methods below.

package controller

import (
	"reflect"
	"time"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// TODO: check whether this resync period is reasonable.
const resyncPeriod = 10 * time.Second

type SecureNamingController struct {
	client v1core.CoreV1Interface

	mapping *SecureNamingMapping

	podController cache.Controller
	podStore      cache.Store

	serviceController cache.Controller
	serviceIndexer    cache.Indexer

	serviceQueue workqueue.RateLimitingInterface
}

func NewSecureNamingController(core v1core.CoreV1Interface) *SecureNamingController {
	snc := &SecureNamingController{
		client:       core,
		mapping:      NewSecureNamingMapping(),
		serviceQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	podHandlers := cache.ResourceEventHandlerFuncs{
		AddFunc:    snc.updatePodServices,
		DeleteFunc: snc.updatePodServices,
		UpdateFunc: snc.updatePod,
	}
	snc.podStore, snc.podController = newPodInformer(core, podHandlers)

	svcHandlers := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			snc.enqueueService(obj.(*v1.Service))
		},
		DeleteFunc: func(obj interface{}) {
			snc.enqueueService(obj.(*v1.Service))
		},
		UpdateFunc: func(old, cur interface{}) {
			snc.enqueueService(cur.(*v1.Service))
		},
	}
	snc.serviceIndexer, snc.serviceController = newServiceIndexerInformer(core, svcHandlers)

	return snc
}

func (snc *SecureNamingController) Run(stopCh chan struct{}) {
	defer snc.serviceQueue.ShutDown()

	go snc.podController.Run(stopCh)
	go snc.serviceController.Run(stopCh)

	go snc.worker()

	<-stopCh
}

func (snc *SecureNamingController) worker() {
	glog.Infof("Starting a worker")

	for snc.processNextService() {
	}
}

// TODO: do we need to return a boolean for this function?
func (snc *SecureNamingController) processNextService() bool {
	k, quit := snc.serviceQueue.Get()
	if quit {
		glog.Infof("No service to be process in the queue; the worker is quitting")
		return false
	}
	defer snc.serviceQueue.Done(k)

	key := k.(string)
	obj, exist, err := snc.serviceIndexer.GetByKey(key)
	if err != nil {
		glog.Warningf("Failed to retrieve a service with key %s", key)

		return true
	}
	if !exist {
		glog.Infof("Service with key %s does not exsit in the Indexer", key)

		// Remove service from the indexer
		snc.mapping.RemoveService(key)
		return true
	}

	svc := obj.(*v1.Service)
	lo := metav1.ListOptions{
		LabelSelector: labels.Set(svc.Spec.Selector).String(),
	}
	pods, err := snc.client.Pods(svc.Namespace).List(lo)
	if err != nil {
		glog.Errorf("Failed to get pods for services")
		return true
	}

	accounts := []string{}
	for _, p := range pods.Items {
		accounts = append(accounts, p.Spec.ServiceAccountName)
	}
	// Use `key` instead of `svc.GetName()` since `key` contains both service
	// name and service namespace.
	snc.mapping.SetServiceAccounts(key, sets.NewString(accounts...))

	return true
}

// Finds the services that the pod belongs to and push those services into
// queue so that the services can be updated by the workers.
func (snc *SecureNamingController) updatePodServices(obj interface{}) {
	p := obj.(*v1.Pod)
	snc.enqueueServices(snc.getPodServices(p))
}

func (snc *SecureNamingController) updatePod(oldObj, newObj interface{}) {
	op := oldObj.(*v1.Pod)
	np := newObj.(*v1.Pod)

	if op.GetResourceVersion() == np.GetResourceVersion() {
		// This `updatePod` method is called due to periodic resync but the pod
		// spec is not changed since they have the same resource versions.
		return
	}

	snc.enqueueServices(snc.getPodServices(np))
	if !reflect.DeepEqual(op.GetLabels(), np.GetLabels()) {
		// If the labels has been updated, we need to update services that the pod
		// used to belong to, since the pod may no longer be part of these
		// services.
		snc.enqueueServices(snc.getPodServices(np))
	}
}

func newServiceIndexerInformer(core v1core.CoreV1Interface, rehf cache.ResourceEventHandlerFuncs) (cache.Indexer, cache.Controller) {
	si := core.Services(v1.NamespaceAll)
	return cache.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return si.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return si.Watch(options)
			},
		},
		&v1.Service{},
		resyncPeriod,
		rehf,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
}

func newPodInformer(core v1core.CoreV1Interface, rehf cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller) {
	pi := core.Pods(v1.NamespaceAll)
	return cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return pi.List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return pi.Watch(options)
			},
		},
		&v1.Pod{},
		resyncPeriod,
		rehf,
	)
}

func (snc *SecureNamingController) enqueueService(s *v1.Service) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(s)
	if err != nil {
		// TODO: log an error here
		return
	}

	snc.serviceQueue.Add(key)
}

func (snc *SecureNamingController) enqueueServices(ss []*v1.Service) {
	for _, s := range ss {
		snc.enqueueService(s)
	}
}

func (snc *SecureNamingController) getPodServices(pod *v1.Pod) []*v1.Service {
	allServices := []*v1.Service{}
	cache.ListAllByNamespace(snc.serviceIndexer, pod.Namespace, labels.Everything(), func(m interface{}) {
		allServices = append(allServices, m.(*v1.Service))
	})

	services := []*v1.Service{}
	for _, service := range allServices {
		if service.Spec.Selector == nil {
			continue
		}
		selector := labels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if selector.Matches(labels.Set(pod.Labels)) {
			services = append(services, service)
		}
	}
	return services
}
