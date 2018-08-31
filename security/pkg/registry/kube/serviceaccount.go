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
	"fmt"
	"reflect"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	coreinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/registry"
)

// ServiceAccountController monitors service account definition changes in a namespace.
// For each service account object, its SpiffeID is added to identity registry for
// whitelisting purpose.
type ServiceAccountController struct {
	core kubernetes.Interface

	// identity registry object
	reg registry.Registry

	// controller for service objects
	controller cache.Controller
}

// NewServiceAccountController returns a new ServiceAccountController
func NewServiceAccountController(core kubernetes.Interface, namespace string, reg registry.Registry) *ServiceAccountController {
	c := &ServiceAccountController{
		core: core,
		reg:  reg,
	}

	informer := coreinformer.NewFilteredServiceAccountInformer(core, namespace, time.Minute, cache.Indexers{}, nil)
	informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.serviceAccountAdded,
			DeleteFunc: c.serviceAccountDeleted,
			UpdateFunc: c.serviceAccountUpdated,
		},
	)

	c.controller = informer

	return c
}

// Run starts the ServiceAccountController until a value is sent to stopCh.
// It should only be called once.
func (c *ServiceAccountController) Run(stopCh chan struct{}) {
	go c.controller.Run(stopCh)
}

func getSpiffeID(sa *v1.ServiceAccount) string {
	// borrowed from security/pkg/pki/ca/controller/secret.go:generateKeyAndCert()
	return fmt.Sprintf("%s://cluster.local/ns/%s/sa/%s", util.URIScheme, sa.GetNamespace(), sa.GetName())
}

func (c *ServiceAccountController) serviceAccountAdded(obj interface{}) {
	sa := obj.(*v1.ServiceAccount)
	id := getSpiffeID(sa)
	err := c.reg.AddMapping(id, id)
	if err != nil {
		log.Errorf("cannot add mapping %q -> %q to registry: %s", id, id, err.Error())
	}
}

func (c *ServiceAccountController) serviceAccountDeleted(obj interface{}) {
	sa := obj.(*v1.ServiceAccount)
	id := getSpiffeID(sa)
	err := c.reg.DeleteMapping(id, id)
	if err != nil {
		log.Errorf("cannot delete mapping %q to %q from registry: %s", id, id, err.Error())
	}

}

func (c *ServiceAccountController) serviceAccountUpdated(oldObj, newObj interface{}) {
	if oldObj == newObj || reflect.DeepEqual(oldObj, newObj) {
		// Nothing is changed. The method is invoked by periodical re-sync with the apiserver.
		return
	}

	oldSa := oldObj.(*v1.ServiceAccount)
	newSa := newObj.(*v1.ServiceAccount)
	// if name or namespace has changed
	if oldSa.GetName() != newSa.GetName() || oldSa.GetNamespace() != newSa.GetNamespace() {
		oldID := getSpiffeID(oldSa)
		newID := getSpiffeID(newSa)
		_ = c.reg.DeleteMapping(oldID, oldID)
		_ = c.reg.AddMapping(newID, newID)
	}
}
