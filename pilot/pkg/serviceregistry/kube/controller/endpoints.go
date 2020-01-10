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
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	configKube "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/labels"
)

type endpointsController struct {
	kubeEndpoints
}

var _ kubeEndpointsController = &endpointsController{}

func newEndpointsController(c *Controller, sharedInformers informers.SharedInformerFactory) *endpointsController {
	informer := sharedInformers.Core().V1().Endpoints().Informer()
	out := &endpointsController{
		kubeEndpoints: kubeEndpoints{
			c:        c,
			informer: informer,
		},
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

func (e *endpointsController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance {
	eps, err := listerv1.NewEndpointsLister(e.informer.GetIndexer()).Endpoints(proxy.Metadata.Namespace).List(klabels.Everything())
	if err != nil {
		log.Errorf("Get endpoints by index failed: %v", err)
		return nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, ep := range eps {
		instances := e.proxyServiceInstances(c, ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func (e *endpointsController) proxyServiceInstances(c *Controller, endpoints *v1.Endpoints, proxy *model.Proxy) []*model.ServiceInstance {
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

	return e.handleEvent(ep.Name, ep.Namespace, event, curr, func(obj interface{}, event model.Event) {
		ep := obj.(*v1.Endpoints)
		e.c.updateEDS(ep, event)
	})
}
