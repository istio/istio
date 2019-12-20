// Copyright 2019 Istio Authors
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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
	v1 "k8s.io/api/core/v1"
	discoveryv1alpha1 "k8s.io/api/discovery/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

// Pilot can get EDS information from Kubernetes from two mutually exclusive sources, Endpoints and
// EndpointSlices. The kubeEndpointsController abstracts these details and provides a common interface that
// both sources implement.
type kubeEndpointsController interface {
	HasSynced() bool
	Run(stopCh <-chan struct{})
	InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
		labelsList labels.Collection) ([]*model.ServiceInstance, error)
	GetProxyServiceInstances(c *Controller, proxy *model.Proxy, proxyNamespace string) []*model.ServiceInstance
}

type kubeEndpoints struct {
	c        *Controller
	informer cache.SharedIndexInformer
}

func (e *kubeEndpoints) GetProxyServiceInstances(c *Controller, proxy *model.Proxy, proxyNamespace string) []*model.ServiceInstance {
	var otherNamespaceEndpoints []*model.ServiceInstance
	sameNamespaceEndpoints := make([]*model.ServiceInstance, 0)

	for _, item := range e.informer.GetStore().List() {
		var proxyServiceInstances []*model.ServiceInstance
		namespace := ""
		switch item.(type) {
		case v1.Endpoints:
			ep := *item.(*v1.Endpoints)
			namespace = ep.Namespace
			proxyServiceInstances = getProxyServiceInstancesByEndpoint(c, ep, proxy)
		case discoveryv1alpha1.EndpointSlice:
			slice := item.(*discoveryv1alpha1.EndpointSlice)
			namespace = slice.Namespace
			proxyServiceInstances = getProxyServiceInstancesByEndpointSlice(c, slice, proxy)
		}
		if namespace == proxyNamespace {
			sameNamespaceEndpoints = append(sameNamespaceEndpoints, proxyServiceInstances...)
		} else {
			if otherNamespaceEndpoints == nil {
				otherNamespaceEndpoints = make([]*model.ServiceInstance, 0)
			}
			otherNamespaceEndpoints = append(otherNamespaceEndpoints, proxyServiceInstances...)
		}
	}

	// Put the sameNamespaceEndpoints in front of otherNamespaceEndpoints so that Pilot will
	// first use endpoints from sameNamespaceEndpoints. This makes sure if there are two endpoints
	// referring to the same IP/port, the one in sameNamespaceEndpoints will be used. (The other one
	// in otherNamespaceEndpoints will thus be rejected by Pilot).
	return append(sameNamespaceEndpoints, otherNamespaceEndpoints...)
}

func (e *kubeEndpoints) HasSynced() bool {
	return e.informer.HasSynced()
}

func (e *kubeEndpoints) Run(stopCh <-chan struct{}) {
	e.informer.Run(stopCh)
}
