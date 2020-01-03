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
	v1 "k8s.io/api/core/v1"
	discoveryv1alpha1 "k8s.io/api/discovery/v1alpha1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/pkg/log"
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
	updateEDS(obj interface{}, event model.Event)
}

type kubeEndpoints struct {
	c        *Controller
	informer cache.SharedIndexInformer
}

// GetProxyServiceInstances returns service instances of the given proxy.
func (e *kubeEndpoints) GetProxyServiceInstances(c *Controller, proxy *model.Proxy, proxyNamespace string) []*model.ServiceInstance {
	var otherNamespaceEndpoints []*model.ServiceInstance
	var sameNamespaceEndpoints []*model.ServiceInstance

	for _, item := range e.informer.GetStore().List() {
		var proxyServiceInstances []*model.ServiceInstance
		namespace := ""
		switch item := item.(type) {
		case *v1.Endpoints:
			namespace = item.Namespace
			proxyServiceInstances = getProxyServiceInstancesByEndpoint(c, *item, proxy)
		case *discoveryv1alpha1.EndpointSlice:
			namespace = item.Namespace
			proxyServiceInstances = getProxyServiceInstancesByEndpointSlice(c, item, proxy)
		}
		if namespace == proxyNamespace {
			sameNamespaceEndpoints = append(sameNamespaceEndpoints, proxyServiceInstances...)
		} else {
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

func (e *kubeEndpoints) handleEvent(epc kubeEndpointsController, name string, namespace string, event model.Event, obj interface{}) error {
	log.Debugf("Handle event %s for endpoint %s in namespace %s", event, name, namespace)

	// headless service cluster discovery type is ORIGINAL_DST, we do not need update EDS.
	if features.EnableHeadlessService.Get() {
		if obj, _, _ := e.c.services.GetIndexer().GetByKey(kube.KeyFunc(name, namespace)); obj != nil {
			svc := obj.(*v1.Service)
			// if the service is headless service, trigger a full push.
			if svc.Spec.ClusterIP == v1.ClusterIPNone {
				e.c.xdsUpdater.ConfigUpdate(&model.PushRequest{
					Full:              true,
					NamespacesUpdated: map[string]struct{}{namespace: {}},
					// TODO: extend and set service instance type, so no need to re-init push context
					ConfigTypesUpdated: map[string]struct{}{schemas.ServiceEntry.Type: {}},
				})
				return nil
			}
		}
	}

	epc.updateEDS(obj, event)

	return nil
}
