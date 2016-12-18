// Copyright 2016 Google Inc.
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
	"errors"
	"log"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// Controller is a collection of synchronized resource watchers
type Controller struct {
	client       *kubernetes.Clientset
	resyncPeriod time.Duration
	queue        Queue
	controllers  []*cache.Controller

	endpoints cache.Store
	services  cache.Store
}

// NewController creates a new Kubernetes controller
func NewController(
	client *kubernetes.Clientset,
	namespace string,
	resyncPeriod time.Duration) *Controller {
	out := &Controller{
		client:       client,
		resyncPeriod: resyncPeriod,
		queue:        NewQueue(1 * time.Second),
	}

	out.services = out.addWatcher(&v1.Service{},
		func(opt v1.ListOptions) (runtime.Object, error) {
			return client.Services(namespace).List(opt)
		},
		func(opt v1.ListOptions) (watch.Interface, error) {
			return client.Services(namespace).Watch(opt)
		},
		out.notify,
	)

	out.endpoints = out.addWatcher(&v1.Endpoints{},
		func(opt v1.ListOptions) (runtime.Object, error) {
			return client.Endpoints(namespace).List(opt)
		},
		func(opt v1.ListOptions) (watch.Interface, error) {
			return client.Endpoints(namespace).Watch(opt)
		},
		out.notify,
	)

	return out
}

func (c *Controller) notify(obj interface{}) error {
	if !c.HasSynced() {
		return errors.New("Waiting till full synchronization")
	}
	log.Printf("%#v", obj)
	return nil
}

func (c *Controller) addWatcher(
	o runtime.Object,
	listFunc cache.ListFunc,
	watchFunc cache.WatchFunc,
	handler func(obj interface{}) error) cache.Store {
	store, controller := cache.NewInformer(
		&cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc},
		o, c.resyncPeriod,
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources
			AddFunc: func(obj interface{}) {
				c.queue.Push(Task{handler: handler, obj: obj})
			},
			UpdateFunc: func(old, obj interface{}) {
				c.queue.Push(Task{handler: handler, obj: obj})
			},
			DeleteFunc: func(obj interface{}) {
				c.queue.Push(Task{handler: handler, obj: obj})
			},
		},
	)

	c.controllers = append(c.controllers, controller)
	return store
}

// HasSynced returns true after the initial state synchronization
func (c *Controller) HasSynced() bool {
	for _, ctl := range c.controllers {
		if !ctl.HasSynced() {
			log.Println("Controllers are syncing...")
			return false
		}
	}
	return true
}

// Run all controllers until a signal is received
func (c *Controller) Run(stop chan struct{}) {
	go c.queue.Run(stop)
	for _, ctl := range c.controllers {
		go ctl.Run(stop)
	}
	<-stop
}

// key function used internally by kubernetes (here, namespace is non-empty)
func keyFunc(namespace, name string) string {
	return namespace + "/" + name
}
