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
	"sync"

	discoveryv1alpha1 "k8s.io/api/discovery/v1alpha1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	discoverylister "k8s.io/client-go/listers/discovery/v1alpha1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	configKube "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/labels"
)

type endpointSliceController struct {
	kubeEndpoints
	endpointCache *endpointSliceCache
}

var _ kubeEndpointsController = &endpointSliceController{}

func newEndpointSliceController(c *Controller, sharedInformers informers.SharedInformerFactory) *endpointSliceController {
	informer := sharedInformers.Discovery().V1alpha1().EndpointSlices().Informer()
	// TODO Endpoints has a special cache, to filter out irrelevant updates to kube-system
	// Investigate if we need this, or if EndpointSlice is makes this not relevant
	out := &endpointSliceController{
		kubeEndpoints: kubeEndpoints{
			c:        c,
			informer: informer,
		},
		endpointCache: newEndpointSliceCache(),
	}
	registerHandlers(informer, c.queue, "EndpointSlice", out.onEvent)
	return out
}

func (esc *endpointSliceController) updateEDS(es interface{}, event model.Event) {
	slice := es.(*discoveryv1alpha1.EndpointSlice)
	svcName := slice.Labels[discoveryv1alpha1.LabelServiceName]
	hostname := kube.ServiceHostname(svcName, slice.Namespace, esc.c.domainSuffix)

	endpoints := make([]*model.IstioEndpoint, 0)
	if event != model.EventDelete {
		for _, e := range slice.Endpoints {
			if e.Conditions.Ready != nil && !*e.Conditions.Ready {
				// Ignore not ready endpoints
				continue
			}
			for _, a := range e.Addresses {
				pod := esc.c.pods.getPodByIP(a)
				if pod == nil {
					// This can not happen in usual case
					if e.TargetRef != nil && e.TargetRef.Kind == "Pod" {
						log.Warnf("Endpoint without pod %s %s.%s", a, svcName, slice.Namespace)

						if esc.c.metrics != nil {
							esc.c.metrics.AddMetric(model.EndpointNoPod, string(hostname), nil, a)
						}
						// TODO: keep them in a list, and check when pod events happen !
						continue
					}
					// For service without selector, maybe there are no related pods
				}

				var labels map[string]string
				locality := getLocalityFromTopology(e.Topology)
				sa, uid := "", ""
				if pod != nil {
					sa = kube.SecureNamingSAN(pod)
					uid = createUID(pod.Name, pod.Namespace)
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
						Network:         esc.c.endpointNetwork(a),
						Locality:        locality,
						Attributes:      model.ServiceAttributes{Name: svcName, Namespace: slice.Namespace},
						TLSMode:         tlsMode,
					})
				}
			}
		}
	}

	esc.endpointCache.Update(hostname, slice.Name, endpoints)

	log.Infof("Handle EDS endpoint %s in namespace %s", svcName, slice.Namespace)

	_ = esc.c.xdsUpdater.EDSUpdate(esc.c.clusterID, string(hostname), slice.Namespace, esc.endpointCache.Get(hostname))
}

func (esc *endpointSliceController) onEvent(curr interface{}, event model.Event) error {
	if err := esc.c.checkReadyForEvents(); err != nil {
		return err
	}

	ep, ok := curr.(*discoveryv1alpha1.EndpointSlice)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("1 Couldn't get object from tombstone %#v", curr)
			return nil
		}
		ep, ok = tombstone.Obj.(*discoveryv1alpha1.EndpointSlice)
		if !ok {
			log.Errorf("Tombstone contained an object that is not an endpoints slice %#v", curr)
			return nil
		}
	}

	return esc.handleEvent(ep.Labels[discoveryv1alpha1.LabelServiceName], ep.Namespace, event, curr, func(obj interface{}, event model.Event) {
		esc.updateEDS(obj, event)
	})
}

func (esc *endpointSliceController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance {
	eps, err := discoverylister.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(proxy.Metadata.Namespace).List(klabels.Everything())
	if err != nil {
		log.Errorf("Get endpointslice by index failed: %v", err)
		return nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, ep := range eps {
		instances := esc.proxyServiceInstances(c, ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func (esc *endpointSliceController) proxyServiceInstances(c *Controller, ep *discoveryv1alpha1.EndpointSlice, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := kube.ServiceHostname(ep.Labels[discoveryv1alpha1.LabelServiceName], ep.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc == nil {
		return out
	}

	for _, port := range ep.Ports {
		if port.Name == nil || port.Port == nil {
			continue
		}
		svcPort, exists := svc.Ports.Get(*port.Name)
		if !exists {
			continue
		}

		podIP := proxy.IPAddresses[0]

		// consider multiple IP scenarios
		for _, ip := range proxy.IPAddresses {
			for _, ep := range ep.Endpoints {
				for _, a := range ep.Addresses {
					if a == ip {
						out = append(out, c.getEndpoints(podIP, ip, *port.Port, svcPort, svc))
						// If the endpoint isn't ready, report this
						if ep.Conditions.Ready != nil && !*ep.Conditions.Ready && c.metrics != nil {
							c.metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy, "")
						}
					}
				}
			}
		}
	}

	return out
}

func (esc *endpointSliceController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
	labelsList labels.Collection) ([]*model.ServiceInstance, error) {
	esLabelSelector := klabels.Set(map[string]string{discoveryv1alpha1.LabelServiceName: svc.Attributes.Name}).AsSelectorPreValidated()
	slices, err := discoverylister.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(svc.Attributes.Namespace).List(esLabelSelector)
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

				sa, uid := "", ""
				if pod != nil {
					sa = kube.SecureNamingSAN(pod)
					uid = createUID(pod.Name, pod.Namespace)
				}
				az := getLocalityFromTopology(e.Topology)
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
							Endpoint: &model.IstioEndpoint{
								Address:         a,
								EndpointPort:    portNum,
								ServicePortName: svcPortEntry.Name,
								UID:             uid,
								Network:         c.endpointNetwork(a),
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
	}
	return out, nil
}

func getLocalityFromTopology(topology map[string]string) string {
	locality := topology[NodeRegionLabelGA]
	if _, f := topology[NodeZoneLabelGA]; f {
		locality += "/" + topology[NodeZoneLabelGA]
	}
	if _, f := topology[IstioSubzoneLabel]; f {
		locality += "/" + topology[IstioSubzoneLabel]
	}
	return locality
}

type endpointSliceCache struct {
	mu                         sync.RWMutex
	endpointsByServiceAndSlice map[host.Name]map[string][]*model.IstioEndpoint
}

func newEndpointSliceCache() *endpointSliceCache {
	out := &endpointSliceCache{
		endpointsByServiceAndSlice: make(map[host.Name]map[string][]*model.IstioEndpoint),
	}
	return out
}

func (e *endpointSliceCache) Update(hostname host.Name, slice string, endpoints []*model.IstioEndpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(endpoints) == 0 {
		delete(e.endpointsByServiceAndSlice[hostname], slice)
	}
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
