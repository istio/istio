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

package controller

import (
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ServiceController monitors the service definition changes in a namespace.
// Callback functions are called whenever there is a new service, a service deleted,
// or a service updated.
type ServiceController struct {
	core corev1.CoreV1Interface

	// controller for service objects
	controller cache.Controller

	// handlers for service events
	addFunc    func(*v1.Service)
	deleteFunc func(*v1.Service)
	updateFunc func(*v1.Service, *v1.Service)
}

// NewServiceController returns a new ServiceController
func NewServiceController(core corev1.CoreV1Interface, namespace string,
	addFunc, deleteFunc func(*v1.Service),
	updateFunc func(*v1.Service, *v1.Service)) *ServiceController {
	c := &ServiceController{
		core:       core,
		addFunc:    addFunc,
		deleteFunc: deleteFunc,
		updateFunc: updateFunc,
	}

	LW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return core.Services(namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return core.Services(namespace).Watch(options)
		},
	}

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.serviceAdded,
		DeleteFunc: c.serviceDeleted,
		UpdateFunc: c.serviceUpdated,
	}
	_, c.controller = cache.NewInformer(LW, &v1.Service{}, time.Minute, handler)
	return c
}

// Run starts the ServiceController until a value is sent to stopCh.
// It should only be called once.
func (c *ServiceController) Run(stopCh chan struct{}) {
	go c.controller.Run(stopCh)
}

func (c *ServiceController) serviceAdded(obj interface{}) {
	c.addFunc(obj.(*v1.Service))
}

func (c *ServiceController) serviceDeleted(obj interface{}) {
	c.deleteFunc(obj.(*v1.Service))
}

func (c *ServiceController) serviceUpdated(oldObj, newObj interface{}) {
	c.updateFunc(oldObj.(*v1.Service), newObj.(*v1.Service))
}
