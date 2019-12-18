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
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	configKube "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schemas"
)

type endpointsController struct {
	c        *Controller
	informer cache.SharedIndexInformer
}

var _ kubeEndpointsController = &endpointsController{}

func newEndpointsController(c *Controller, sharedInformers informers.SharedInformerFactory) *endpointsController {
	informer := sharedInformers.Core().V1().Endpoints().Informer()
	out := &endpointsController{
		c:        c,
		informer: informer,
	}
	out.registerEndpointsHandler()
	return out
}

func (e *endpointsController) registerEndpointsHandler() {
	e.informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			// TODO: filtering functions to skip over un-referenced resources (perf)
			AddFunc: func(obj interface{}) {
				incrementEvent("Endpoints", "add")
				e.c.queue.Push(func() error {
					return e.onEvent(obj, model.EventAdd)
				})
			},
			UpdateFunc: func(old, cur interface{}) {
				// Avoid pushes if only resource version changed (kube-scheduller, cluster-autoscaller, etc)
				oldE := old.(*v1.Endpoints)
				curE := cur.(*v1.Endpoints)

				if !compareEndpoints(oldE, curE) {
					incrementEvent("Endpoints", "update")
					e.c.queue.Push(func() error {
						return e.onEvent(cur, model.EventUpdate)
					})
				} else {
					incrementEvent("Endpoints", "updatesame")
				}
			},
			DeleteFunc: func(obj interface{}) {
				incrementEvent("Endpoints", "delete")
				// Deleting the endpoints results in an empty set from EDS perspective - only
				// deleting the service should delete the resources. The full sync replaces the
				// maps.
				// c.updateEDS(obj.(*v1.Endpoints))
				e.c.queue.Push(func() error {
					return e.onEvent(obj, model.EventDelete)
				})
			},
		})
}

func (e *endpointsController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy, proxyNamespace string) []*model.ServiceInstance {
	endpointsForPodInSameNS := make([]*model.ServiceInstance, 0)
	// TODO we may be able to remove this, endpoints should be in same NS?
	endpointsForPodInDifferentNS := make([]*model.ServiceInstance, 0)

	for _, item := range e.informer.GetStore().List() {
		ep := *item.(*v1.Endpoints)
		endpoints := &endpointsForPodInSameNS
		if ep.Namespace != proxyNamespace {
			endpoints = &endpointsForPodInDifferentNS
		}

		*endpoints = append(*endpoints, getProxyServiceInstancesByEndpoint(c, ep, proxy)...)
	}

	// Put the endpointsForPodInSameNS in front of endpointsForPodInDifferentNS so that Pilot will
	// first use endpoints from endpointsForPodInSameNS. This makes sure if there are two endpoints
	// referring to the same IP/port, the one in endpointsForPodInSameNS will be used. (The other one
	// in endpointsForPodInDifferentNS will thus be rejected by Pilot).
	return append(endpointsForPodInSameNS, endpointsForPodInDifferentNS...)
}

func getProxyServiceInstancesByEndpoint(c *Controller, endpoints v1.Endpoints, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := kube.ServiceHostname(endpoints.Name, endpoints.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc != nil {
		for _, ss := range endpoints.Subsets {
			for _, port := range ss.Ports {
				svcPort, exists := svc.Ports.Get(port.Name)
				if !exists {
					continue
				}

				podIP := proxy.IPAddresses[0]

				// consider multiple IP scenarios
				for _, ip := range proxy.IPAddresses {
					if hasProxyIP(ss.Addresses, ip) {
						out = append(out, c.getEndpoints(podIP, ip, port.Port, svcPort, svc))
					}

					if hasProxyIP(ss.NotReadyAddresses, ip) {
						out = append(out, c.getEndpoints(podIP, ip, port.Port, svcPort, svc))
						if c.metrics != nil {
							c.metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy, "")
						}
					}
				}
			}
		}
	}

	return out
}

func (e *endpointsController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
	labelsList labels.Collection) ([]*model.ServiceInstance, error) {
	item, exists, err := e.informer.GetStore().GetByKey(kube.KeyFunc(svc.Attributes.Name, svc.Attributes.Namespace))
	if err != nil {
		log.Infof("get endpoints(%s, %s) => error %v", svc.Attributes.Name, svc.Attributes.Namespace, err)
		return nil, nil
	}
	if !exists {
		return nil, nil
	}

	// Locate all ports in the actual service
	svcPortEntry, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil, nil
	}
	ep := item.(*v1.Endpoints)
	var out []*model.ServiceInstance
	for _, ss := range ep.Subsets {
		for _, ea := range ss.Addresses {
			var podLabels labels.Instance
			pod := c.pods.getPodByIP(ea.IP)
			if pod != nil {
				podLabels = configKube.ConvertLabels(pod.ObjectMeta)
			}

			// check that one of the input labels is a subset of the labels
			if !labelsList.HasSubsetOf(podLabels) {
				continue
			}

			az, sa, uid := "", "", ""
			if pod != nil {
				az = c.GetPodLocality(pod)
				sa = kube.SecureNamingSAN(pod)
				uid = createUID(pod.Name, pod.Namespace)
			}
			tlsMode := kube.PodTLSMode(pod)

			// identify the port by name. K8S EndpointPort uses the service port name
			for _, port := range ss.Ports {
				if port.Name == "" || // 'name optional if single port is defined'
					svcPortEntry.Name == port.Name {

					out = append(out, &model.ServiceInstance{
						Endpoint: &model.IstioEndpoint{
							Address:         ea.IP,
							EndpointPort:    uint32(port.Port),
							ServicePortName: svcPortEntry.Name,
							UID:             uid,
							Network:         c.endpointNetwork(ea.IP),
							Locality:        az,
							Labels:          podLabels,
							ServiceAccount:  sa,
							TLSMode:         tlsMode,
						},
						ServicePort: svcPortEntry,
						Service:     svc,
					})
				}
			}
		}
	}

	return out, nil
}

func (e *endpointsController) onEvent(curr interface{}, event model.Event) error {
	if err := e.c.checkReadyForEvents(); err != nil {
		return err
	}

	ep, ok := curr.(*v1.Endpoints)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", curr)
			return nil
		}
		ep, ok = tombstone.Obj.(*v1.Endpoints)
		if !ok {
			log.Errorf("Tombstone contained object that is not an endpoints %#v", curr)
			return nil
		}
	}

	log.Debugf("Handle event %s for endpoint %s in namespace %s", event, ep.Name, ep.Namespace)

	// headless service cluster discovery type is ORIGINAL_DST, we do not need update EDS.
	if features.EnableHeadlessService.Get() {
		if obj, _, _ := e.c.services.GetIndexer().GetByKey(kube.KeyFunc(ep.Name, ep.Namespace)); obj != nil {
			svc := obj.(*v1.Service)
			// if the service is headless service, trigger a full push.
			if svc.Spec.ClusterIP == v1.ClusterIPNone {
				e.c.xdsUpdater.ConfigUpdate(&model.PushRequest{
					Full:              true,
					NamespacesUpdated: map[string]struct{}{ep.Namespace: {}},
					// TODO: extend and set service instance type, so no need to re-init push context
					ConfigTypesUpdated: map[string]struct{}{schemas.ServiceEntry.Type: {}},
				})
				return nil
			}
		}
	}

	e.c.updateEDS(ep, event)

	return nil
}

func (e *endpointsController) HasSynced() bool {
	return e.informer.HasSynced()
}

func (e *endpointsController) Run(stopCh <-chan struct{}) {
	e.informer.Run(stopCh)
}
