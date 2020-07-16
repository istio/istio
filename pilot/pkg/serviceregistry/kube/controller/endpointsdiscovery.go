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
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
)

// Pilot can get EDS information from Kubernetes from two mutually exclusive sources, Endpoints and
// EndpointSlices. The kubeEndpointsController abstracts these details and provides a common interface that
// both sources implement.
type kubeEndpointsController interface {
	HasSynced() bool
	Run(stopCh <-chan struct{})
	getInformer() cache.SharedIndexInformer
	onEvent(curr interface{}, event model.Event) error
	// forgetEndpoint does internal bookkeeping on a deleted endpoint
	forgetEndpoint(endpoint interface{})
	InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
		labelsList labels.Collection) ([]*model.ServiceInstance, error)
	GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance
}

// kubeEndpoints abstracts the common behavior across endpoint and endpoint slices.
type kubeEndpoints struct {
	c        *Controller
	informer cache.SharedIndexInformer
}

// updateEdsFunc is called to send eds updates for endpoints/endpointslice.
type updateEdsFunc func(obj interface{}, event model.Event)

func (e *kubeEndpoints) HasSynced() bool {
	return e.informer.HasSynced()
}

func (e *kubeEndpoints) Run(stopCh <-chan struct{}) {
	e.informer.Run(stopCh)
}

// handleEvent processes the event.
func (e *kubeEndpoints) handleEvent(name string, namespace string, event model.Event, ep interface{}, fn updateEdsFunc) error {
	log.Debugf("Handle event %s for endpoint %s in namespace %s", event, name, namespace)

	// headless service cluster discovery type is ORIGINAL_DST, we do not need update EDS.
	if features.EnableHeadlessService {
		if obj, _, _ := e.c.services.GetIndexer().GetByKey(kube.KeyFunc(name, namespace)); obj != nil {
			svc := obj.(*v1.Service)
			// if the service is headless service, trigger a full push.
			if svc.Spec.ClusterIP == v1.ClusterIPNone {
				e.c.xdsUpdater.ConfigUpdate(&model.PushRequest{
					Full: true,
					// TODO: extend and set service instance type, so no need to re-init push context
					ConfigsUpdated: map[model.ConfigKey]struct{}{{
						Kind:      model.ServiceEntryKind,
						Name:      svc.Name,
						Namespace: svc.Namespace,
					}: {}},
					Reason: []model.TriggerReason{model.EndpointUpdate},
				})
				return nil
			}
		}
	}

	fn(ep, event)

	return nil
}
