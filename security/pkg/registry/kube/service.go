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

	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/registry"
)

// ServiceController monitors the service definition changes in a namespace. If a
// new service is added with "alpha.istio.io/kubernetes-serviceaccounts" annotation
// enabled, the corresponding service account will be added to the identity registry
// for whitelisting.
// TODO: change it to monitor "alpha.istio.io/canonical-serviceaccounts" annotation
type ServiceController struct {
	core kubernetes.Interface

	// identity registry object
	reg registry.Registry

	// controller for service objects
	controller cache.Controller
}

// NewServiceController returns a new ServiceController
func NewServiceController(core kubernetes.Interface, namespace string, reg registry.Registry) *ServiceController {
	c := &ServiceController{
		core: core,
		reg:  reg,
	}

	serviceInformer := coreinformer.NewFilteredServiceInformer(core, namespace, time.Minute, cache.Indexers{}, nil)
	serviceInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.serviceAdded,
			DeleteFunc: c.serviceDeleted,
			UpdateFunc: c.serviceUpdated,
		},
	)

	c.controller = serviceInformer

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
}

func (c *ServiceController) serviceUpdated(oldObj, newObj interface{}) {
	if oldObj == newObj || reflect.DeepEqual(oldObj, newObj) {
		// Nothing is changed. The method is invoked by periodical re-sync with the apiserver.
		return
	}
	c.serviceDeleted(oldObj)
	c.serviceAdded(newObj)
}
