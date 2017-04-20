// Copyright 2017 Google Inc.
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

package kubernetes

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/mixer/pkg/adapter"
)

type (
	// internal interface used to support testing
	cacheController interface {
		Run(<-chan struct{})
		GetPod(string) (*v1.Pod, error)
		HasSynced() bool
	}

	controllerImpl struct {
		clientset     kubernetes.Interface
		env           adapter.Env
		pods          cache.SharedInformer
		mutationsChan chan resourceMutation
	}

	// used to send updates to the logger
	resourceMutation struct {
		kind eventType
		obj  interface{}
	}

	eventType int
)

const (
	addition eventType = iota
	update
	deletion
)

// mutationBufferSize sets the limit on how many mutation events can be
// outstanding at any moment. 100 is chosen as a reasonable default to start.
// TODO: make this configurable
const mutationBufferSize = 100

// errorDelay controls how long the logger waits when encountering an error
// during logging. This should only ever happen when the underlying cache has
// not yet synced (at which point we need to wait before doing any further
// processing).
// TODO: make this configurable
const errorDelay = 1 * time.Second

const debugVerbosityLevel = 4

// Responsible for setting up the cacheController, based on the supplied client.
// It configures the index informer to list/watch pods and send update events
// to a mutations channel for processing (in this case, logging).
func newCacheController(clientset *kubernetes.Clientset, namespace string, refreshDuration time.Duration, env adapter.Env) cacheController {
	c := &controllerImpl{
		clientset:     clientset,
		env:           env,
		mutationsChan: make(chan resourceMutation, mutationBufferSize),
	}

	c.pods = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts v1.ListOptions) (runtime.Object, error) {
				return clientset.Pods(namespace).List(opts)
			},
			WatchFunc: func(opts v1.ListOptions) (watch.Interface, error) {
				return clientset.Pods(namespace).Watch(opts)
			},
		},
		&v1.Pod{},
		refreshDuration,
		cache.Indexers{},
	)

	// debug logging for pod update events
	if env.Logger().VerbosityLevel(debugVerbosityLevel) {
		eventErr := c.pods.AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					c.mutationsChan <- resourceMutation{addition, obj}
				},
				DeleteFunc: func(obj interface{}) {
					c.mutationsChan <- resourceMutation{deletion, obj}
				},
				UpdateFunc: func(old, cur interface{}) {
					if !reflect.DeepEqual(old, cur) {
						c.mutationsChan <- resourceMutation{update, cur}
					}
				},
			},
		)

		if eventErr != nil {
			c.env.Logger().Warningf("could not add logging event handlers: %v", eventErr)
		}
	}

	return c
}

// Run starts the logger and the controller for the pod cache.
func (c *controllerImpl) Run(stop <-chan struct{}) {
	if c.env.Logger().VerbosityLevel(debugVerbosityLevel) {
		c.env.ScheduleDaemon(func() {
			c.runLogger(stop)
		})
	}
	c.env.ScheduleDaemon(func() {
		c.pods.Run(stop)
		c.env.Logger().Infof("cluster cache started")
	})
	<-stop
	c.env.Logger().Infof("cluster cache updating terminated")
}

// runLogger is responsible for pulling event updates off of the mutations
// channel and logging them via the configured logger.
func (c *controllerImpl) runLogger(stop <-chan struct{}) {
	for {
		select {
		case mutation := <-c.mutationsChan:
			err := c.log(mutation.obj, mutation.kind)
			if err != nil {
				c.env.Logger().Infof("event logging failed, will retry: %v", err)
				select {
				case <-stop:
					c.env.Logger().Infof("cluster cache logging worker terminated")
					return
				case <-time.After(errorDelay):
					// used to wait out errors
					// time.After is OK for usage as there
					// is no real concern here over the
					// slight delay in GC that may occur
					// if a stop message is received before
					// this timer fires.
				}
			}
		case <-stop:
			c.env.Logger().Infof("cluster cache logging worker terminated")
			return
		}
	}
}

func (c *controllerImpl) HasSynced() bool {
	return c.pods.HasSynced()
}

// log is used to record all updates to a cache.
func (c *controllerImpl) log(obj interface{}, kind eventType) error {
	if !c.HasSynced() {
		// should only happen before an initial listing has completed
		return errors.New("resource sync has yet not completed")
	}
	k, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		c.env.Logger().Infof("could not retrieve key for object: %v", err)
		return nil
	}
	c.env.Logger().Infof("%s object with key: '%#v'", kind, k)
	return nil
}

// GetPod returns a Pod object that corresponds to the supplied key, if one
// exists (and is known to the store). Keys are expected in the form of:
// namespace/name (example: "default/curl-2421989462-b2g2d.default").
func (c *controllerImpl) GetPod(podKey string) (*v1.Pod, error) {
	item, exists, err := c.pods.GetStore().GetByKey(podKey)
	if err != nil {
		return nil, fmt.Errorf("error retrieving pod: %v", err)
	}
	if !exists {
		return nil, fmt.Errorf("pod '%s' does not exist", podKey)
	}
	return item.(*v1.Pod), nil
}

func (e eventType) String() string {
	switch e {
	case addition:
		return "Add"
	case deletion:
		return "Delete"
	case update:
		return "Update"
	default:
		return "Unknown"
	}
}
