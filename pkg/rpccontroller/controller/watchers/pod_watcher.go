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

package watchers

import (
	"reflect"
	"time"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	cache "k8s.io/client-go/tools/cache"
)

type PodUpdate struct {
	Pod *api.Pod
	Op  Operation
}

type PodWatcher struct {
	podController cache.Controller
	broadcaster   *Broadcaster
	informer      cache.SharedIndexInformer
	podLister     cache.Indexer
}

type PodUpdatesHandler interface {
	OnPodUpdate(podUpdate *PodUpdate)
}

func (pw *PodWatcher) addEventHandler(obj interface{}) {
	pod, ok := obj.(*api.Pod)
	if !ok {
		return
	}
	pw.broadcaster.Notify(&PodUpdate{Op: ADD, Pod: pod})
}

func (pw *PodWatcher) deleteEventHandler(obj interface{}) {
	pod, ok := obj.(*api.Pod)
	if !ok {
		return
	}
	pw.broadcaster.Notify(&PodUpdate{Op: REMOVE, Pod: pod})
}

func (pw *PodWatcher) updateEventHandler(oldObj, npwObj interface{}) {
	pod, ok := npwObj.(*api.Pod)
	if !ok {
		return
	}
	if !reflect.DeepEqual(npwObj, oldObj) {
		pw.broadcaster.Notify(&PodUpdate{Op: UPDATE, Pod: pod})
	}
}

func (pw *PodWatcher) RegisterHandler(handler PodUpdatesHandler) {
	pw.broadcaster.Add(ListenerFunc(func(instance interface{}) {
		handler.OnPodUpdate(instance.(*PodUpdate))
	}))
}

func (pw *PodWatcher) ListBySelector(set map[string]string) (ret []*api.Pod, err error) {
	selector := labels.SelectorFromSet(set)
	err = cache.ListAll(pw.podLister, selector, func(m interface{}) {
		ret = append(ret, m.(*api.Pod))
	})
	return ret, err
}

func (pw *PodWatcher) HasSynced() bool {
	return pw.podController.HasSynced()
}

// StartPodWatcher: start watching updates for pods from Kuberentes API server
func StartPodWatcher(clientset kubernetes.Interface, resyncPeriod time.Duration, stopCh <-chan struct{}) (*PodWatcher, error) {
	pw := PodWatcher{}

	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    pw.addEventHandler,
		DeleteFunc: pw.deleteEventHandler,
		UpdateFunc: pw.updateEventHandler,
	}

	pw.broadcaster = NewBroadcaster()
	lw := cache.NewListWatchFromClient(clientset.Core().RESTClient(), "pods", metav1.NamespaceAll, fields.Everything())

	pw.podLister, pw.podController = cache.NewIndexerInformer(
		lw,
		&api.Pod{}, resyncPeriod, eventHandler,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	go pw.podController.Run(stopCh)

	return &pw, nil
}
