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
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	discoveryv1alpha1 "k8s.io/api/discovery/v1alpha1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	configKube "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

type endpointSliceController struct {
	cache         cacheHandler
	endpointCache *endpointSliceCache
}

var _ edsController = &endpointSliceController{}

func newEndpointSliceController(c *Controller, sharedInformers informers.SharedInformerFactory) *endpointSliceController {
	epSliceInformer := sharedInformers.Discovery().V1alpha1().EndpointSlices().Informer()
	// TODO Endpoints has a special cache, to filter out irrelevant updates to kube-system
	// Investigate if we need this, or if EndpointSlice is makes this not relevant
	return &endpointSliceController{
		cache:         c.createCacheHandler(epSliceInformer, "EndpointSlice"),
		endpointCache: newendpointSliceCache(),
	}
}

func (e endpointSliceController) Get() cacheHandler {
	return e.cache
}

func (e endpointSliceController) updateEDSSlice(c *Controller, slice *discoveryv1alpha1.EndpointSlice, event model.Event) {
	svcName := slice.Labels[discoveryv1alpha1.LabelServiceName]
	hostname := kube.ServiceHostname(svcName, slice.Namespace, c.domainSuffix)

	endpoints := make([]*model.IstioEndpoint, 0)
	if event != model.EventDelete {
		for _, e := range slice.Endpoints {
			if e.Conditions.Ready != nil && !*e.Conditions.Ready {
				// Ignore not ready endpoints
				continue
			}
			for _, a := range e.Addresses {
				pod := c.pods.getPodByIP(a)
				if pod == nil {
					// This can not happen in usual case
					if e.TargetRef != nil && e.TargetRef.Kind == "Pod" {
						log.Warnf("Endpoint without pod %s %s.%s", a, svcName, slice.Namespace)

						if c.metrics != nil {
							c.metrics.AddMetric(model.EndpointNoPod, string(hostname), nil, a)
						}
						// TODO: keep them in a list, and check when pod events happen !
						continue
					}
					// For service without selector, maybe there are no related pods
				}

				var labels map[string]string
				locality := GetLocalityFromTopology(e.Topology)
				sa, uid := "", ""
				if pod != nil {
					sa = kube.SecureNamingSAN(pod)
					uid = fmt.Sprintf("kubernetes://%s.%s", pod.Name, pod.Namespace)
					labels = configKube.ConvertLabels(pod.ObjectMeta)
				}

				tlsMode := kube.PodTLSMode(pod)

				// EDS and ServiceEntry use name for service port - ADS will need to
				// map to numbers.
				for _, port := range slice.Ports {
					var portNum uint32
					if port.Port != nil {
						portNum = uint32(*port.Port)
					}
					var portName string
					if port.Name != nil {
						portName = *port.Name
					}
					endpoints = append(endpoints, &model.IstioEndpoint{
						Address:         a,
						EndpointPort:    portNum,
						ServicePortName: portName,
						Labels:          labels,
						UID:             uid,
						ServiceAccount:  sa,
						Network:         c.endpointNetwork(a),
						Locality:        locality,
						Attributes:      model.ServiceAttributes{Name: svcName, Namespace: slice.Namespace},
						TLSMode:         tlsMode,
					})
				}
			}
		}
	}

	e.endpointCache.Update(hostname, slice.Name, endpoints)

	log.Infof("Handle EDS endpoint %s in namespace %s", svcName, slice.Namespace)

	if features.EnableHeadlessService.Get() {
		if obj, _, _ := c.services.informer.GetIndexer().GetByKey(kube.KeyFunc(svcName, slice.Namespace)); obj != nil {
			svc := obj.(*v1.Service)
			// if the service is headless service, trigger a full push.
			if svc.Spec.ClusterIP == v1.ClusterIPNone {
				c.xdsUpdater.ConfigUpdate(&model.PushRequest{Full: true, NamespacesUpdated: map[string]struct{}{slice.Namespace: {}}})
				return
			}
		}
	}

	_ = c.xdsUpdater.EDSUpdate(c.clusterID, string(hostname), slice.Namespace, e.endpointCache.Get(hostname))
}

func (e endpointSliceController) AppendInstanceHandler(c *Controller) {
	if e.cache.handler == nil {
		return
	}
	e.cache.handler.Append(func(old, curr interface{}, event model.Event) error {
		ep, ok := curr.(*discoveryv1alpha1.EndpointSlice)
		if !ok {
			tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
			if !ok {
				log.Errorf("Couldn't get object from tombstone %#v", curr)
				return nil
			}
			ep, ok = tombstone.Obj.(*discoveryv1alpha1.EndpointSlice)
			if !ok {
				log.Errorf("Tombstone contained an object that is not an endpoints slice %#v", curr)
				return nil
			}
		}

		e.updateEDSSlice(c, ep, event)

		return nil
	})
}

func (e endpointSliceController) GetEndpointServiceInstances(c *Controller, proxy *model.Proxy, proxyNamespace string) []*model.ServiceInstance {
	endpointsForPodInSameNS := make([]*model.ServiceInstance, 0)
	endpointsForPodInDifferentNS := make([]*model.ServiceInstance, 0)

	for _, item := range e.cache.informer.GetStore().List() {
		slice := *item.(*discoveryv1alpha1.EndpointSlice)
		endpoints := &endpointsForPodInSameNS
		if slice.Namespace != proxyNamespace {
			endpoints = &endpointsForPodInDifferentNS
		}

		*endpoints = append(*endpoints, c.getProxyServiceInstancesByEndpointSlice(slice, proxy)...)
	}

	// Put the endpointsForPodInSameNS in front of endpointsForPodInDifferentNS so that Pilot will
	// first use endpoints from endpointsForPodInSameNS. This makes sure if there are two endpoints
	// referring to the same IP/port, the one in endpointsForPodInSameNS will be used. (The other one
	// in endpointsForPodInDifferentNS will thus be rejected by Pilot).
	return append(endpointsForPodInSameNS, endpointsForPodInDifferentNS...)
}

func (e *endpointSliceController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
	labelsList labels.Collection) ([]*model.ServiceInstance, error) {
	esLabelSelector := klabels.Set(map[string]string{discoveryv1alpha1.LabelServiceName: svc.Attributes.Name}).AsSelectorPreValidated()
	var slices []*discoveryv1alpha1.EndpointSlice
	err := cache.ListAllByNamespace(e.cache.informer.GetIndexer(), svc.Attributes.Namespace, esLabelSelector, func(i interface{}) {
		slices = append(slices, i.(*discoveryv1alpha1.EndpointSlice))
	})
	if err != nil {
		log.Infof("get endpoints(%s, %s) => error %v", svc.Attributes.Name, svc.Attributes.Namespace, err)
		return nil, nil
	}
	if len(slices) == 0 {
		return nil, nil
	}

	// Locate all ports in the actual service
	svcPortEntry, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil, nil
	}

	var out []*model.ServiceInstance
	for _, slice := range slices {
		for _, e := range slice.Endpoints {
			for _, a := range e.Addresses {
				var podLabels labels.Instance
				pod := c.pods.getPodByIP(a)
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
					uid = fmt.Sprintf("kubernetes://%s.%s", pod.Name, pod.Namespace)
				}
				tlsMode := kube.PodTLSMode(pod)

				// identify the port by name. K8S EndpointPort uses the service port name
				for _, port := range slice.Ports {
					var portNum uint32
					if port.Port != nil {
						portNum = uint32(*port.Port)
					}

					if port.Name == nil ||
						svcPortEntry.Name == *port.Name {

						out = append(out, &model.ServiceInstance{
							Endpoint: model.NetworkEndpoint{
								Address:     a,
								Port:        int(portNum),
								ServicePort: svcPortEntry,
								UID:         uid,
								Network:     c.endpointNetwork(a),
								Locality:    az,
							},
							Service:        svc,
							Labels:         podLabels,
							ServiceAccount: sa,
							TLSMode:        tlsMode,
						})
					}
				}
			}
		}
	}
	return out, nil
}

type endpointSliceCache struct {
	mu                         sync.RWMutex
	endpointsByServiceAndSlice map[host.Name]map[string][]*model.IstioEndpoint
}

func newendpointSliceCache() *endpointSliceCache {
	out := &endpointSliceCache{
		endpointsByServiceAndSlice: make(map[host.Name]map[string][]*model.IstioEndpoint),
	}
	return out
}

func (e *endpointSliceCache) Update(hostname host.Name, slice string, endpoints []*model.IstioEndpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, f := e.endpointsByServiceAndSlice[hostname]; !f {
		e.endpointsByServiceAndSlice[hostname] = make(map[string][]*model.IstioEndpoint)
	}
	e.endpointsByServiceAndSlice[hostname][slice] = endpoints
}

func (e *endpointSliceCache) Get(hostname host.Name) []*model.IstioEndpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var endpoints []*model.IstioEndpoint
	for _, eps := range e.endpointsByServiceAndSlice[hostname] {
		endpoints = append(endpoints, eps...)
	}
	return endpoints
}
