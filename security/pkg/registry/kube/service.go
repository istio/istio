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

package kube

import (
	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/listwatch"
	"istio.io/istio/security/pkg/registry"
)

// ServiceController monitors the service definition changes in a namespace. If a
// new service is added with "alpha.istio.io/kubernetes-serviceaccounts" or
// "alpha.istio.io/canonical-serviceaccounts" annotations enabled,
// the corresponding service account will be added to the identity registry
// for whitelisting.
type ServiceController struct {
	core corev1.CoreV1Interface

	// identity registry object
	reg registry.Registry

	// controller for service objects
	controller cache.Controller
}

// NewServiceController returns a new ServiceController
func NewServiceController(core corev1.CoreV1Interface, namespaces []string, reg registry.Registry) *ServiceController {
	c := &ServiceController{
		core: core,
		reg:  reg,
	}

	LW := listwatch.MultiNamespaceListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return core.Services(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return core.Services(namespace).Watch(options)
			},
		}
	})

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
	svc := obj.(*v1.Service)
	svcAcct, ok := svc.ObjectMeta.Annotations[kube.KubeServiceAccountsOnVMAnnotation]
	if ok {
		err := c.reg.AddMapping(svcAcct, svcAcct)
		if err != nil {
			log.Errorf("cannot add mapping %q -> %q to registry: %s", svcAcct, svcAcct, err.Error())
		}
	}
	canonicalSvcAcct, ok := svc.ObjectMeta.Annotations[kube.CanonicalServiceAccountsAnnotation]
	if ok {
		err := c.reg.AddMapping(canonicalSvcAcct, canonicalSvcAcct)
		if err != nil {
			log.Errorf("cannot add mapping %q -> %q to registry: %s", canonicalSvcAcct, canonicalSvcAcct, err.Error())
		}
	}
}

func (c *ServiceController) serviceDeleted(obj interface{}) {
	svc := obj.(*v1.Service)
	svcAcct, ok := svc.ObjectMeta.Annotations[kube.KubeServiceAccountsOnVMAnnotation]
	if ok {
		err := c.reg.DeleteMapping(svcAcct, svcAcct)
		if err != nil {
			log.Errorf("cannot delete mapping %q to %q from registry: %s", svcAcct, svcAcct, err.Error())
		}
	}
	canonicalSvcAcct, ok := svc.ObjectMeta.Annotations[kube.CanonicalServiceAccountsAnnotation]
	if ok {
		err := c.reg.DeleteMapping(canonicalSvcAcct, canonicalSvcAcct)
		if err != nil {
			log.Errorf("cannot delete mapping %q to %q from registry: %s", canonicalSvcAcct, canonicalSvcAcct, err.Error())
		}
	}
}

func (c *ServiceController) serviceUpdated(oldObj, newObj interface{}) {
	if oldObj == newObj || reflect.DeepEqual(oldObj, newObj) {
		// Nothing is changed. The method is invoked by periodical re-sync with the apiserver.
		return
	}
	c.serviceDeleted(oldObj)
	c.serviceAdded(newObj)
}
