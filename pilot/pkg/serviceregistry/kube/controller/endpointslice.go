// Copyright Istio Authors
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

	v1 "k8s.io/api/core/v1"
	discoveryv1alpha1 "k8s.io/api/discovery/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers/discovery/v1alpha1"
	discoverylister "k8s.io/client-go/listers/discovery/v1alpha1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

type endpointSliceController struct {
	kubeEndpoints
	endpointCache *endpointSliceCache
}

var _ kubeEndpointsController = &endpointSliceController{}

func newEndpointSliceController(c *Controller, informer v1alpha1.EndpointSliceInformer) *endpointSliceController {
	// TODO Endpoints has a special cache, to filter out irrelevant updates to kube-system
	// Investigate if we need this, or if EndpointSlice is makes this not relevant
	out := &endpointSliceController{
		kubeEndpoints: kubeEndpoints{
			c:        c,
			informer: informer.Informer(),
		},
		endpointCache: newEndpointSliceCache(),
	}
	registerHandlers(informer.Informer(), c.queue, "EndpointSlice", out.onEvent, nil)
	return out
}

func (esc *endpointSliceController) getInformer() cache.SharedIndexInformer {
	return esc.informer
}

func (esc *endpointSliceController) onEvent(curr interface{}, event model.Event) error {
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

	return processEndpointEvent(esc.c, esc, ep.Labels[discoveryv1alpha1.LabelServiceName], ep.Namespace, event, curr)
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
// TODO: this code does not return k8s service instances when the proxy's IP is a workload entry
// To tackle this, we need a ip2instance map like what we have in service entry.
func (esc *endpointSliceController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance {
	eps, err := discoverylister.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(proxy.Metadata.Namespace).List(klabels.Everything())
	if err != nil {
		log.Errorf("Get endpointslice by index failed: %v", err)
		return nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, ep := range eps {
		instances := sliceServiceInstances(c, ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func sliceServiceInstances(c *Controller, ep *discoveryv1alpha1.EndpointSlice, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := kube.ServiceHostname(ep.Labels[discoveryv1alpha1.LabelServiceName], ep.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc == nil {
		return out
	}

	podIP := proxy.IPAddresses[0]
	pod := c.pods.getPodByIP(podIP)
	builder := NewEndpointBuilder(c, pod)

	for _, port := range ep.Ports {
		if port.Name == nil || port.Port == nil {
			continue
		}
		svcPort, exists := svc.Ports.Get(*port.Name)
		if !exists {
			continue
		}

		// consider multiple IP scenarios
		for _, ip := range proxy.IPAddresses {
			for _, ep := range ep.Endpoints {
				for _, a := range ep.Addresses {
					if a == ip {
						istioEndpoint := builder.buildIstioEndpoint(ip, *port.Port, svcPort.Name)
						out = append(out, &model.ServiceInstance{
							Endpoint:    istioEndpoint,
							ServicePort: svcPort,
							Service:     svc,
						})
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

func (esc *endpointSliceController) forgetEndpoint(endpoint interface{}) {
	slice := endpoint.(*discoveryv1alpha1.EndpointSlice)
	key := kube.KeyFunc(slice.Name, slice.Namespace)
	for _, e := range slice.Endpoints {
		for _, a := range e.Addresses {
			esc.c.pods.endpointDeleted(key, a)
		}
	}
}

func (esc *endpointSliceController) buildIstioEndpoints(es interface{}, host host.Name) []*model.IstioEndpoint {
	slice := es.(*discoveryv1alpha1.EndpointSlice)
	endpoints := make([]*model.IstioEndpoint, 0)
	for _, e := range slice.Endpoints {
		if e.Conditions.Ready != nil && !*e.Conditions.Ready {
			// Ignore not ready endpoints
			continue
		}
		for _, a := range e.Addresses {
			pod, expectedPod := getPod(esc.c, a, &metav1.ObjectMeta{Name: slice.Name, Namespace: slice.Namespace}, e.TargetRef, host)
			if pod == nil && expectedPod {
				continue
			}
			builder := esc.newEndpointBuilder(pod, e)
			// EDS and ServiceEntry use name for service port - ADS will need to map to numbers.
			for _, port := range slice.Ports {
				var portNum int32
				if port.Port != nil {
					portNum = *port.Port
				}
				var portName string
				if port.Name != nil {
					portName = *port.Name
				}

				istioEndpoint := builder.buildIstioEndpoint(a, portNum, portName)
				endpoints = append(endpoints, istioEndpoint)
			}
		}
	}
	esc.endpointCache.Update(host, slice.Name, endpoints)
	return esc.endpointCache.Get(host)
}

func (esc *endpointSliceController) buildIstioEndpointsWithService(name, namespace string, host host.Name) []*model.IstioEndpoint {
	esLabelSelector := klabels.Set(map[string]string{discoveryv1alpha1.LabelServiceName: name}).AsSelectorPreValidated()
	slices, err := discoverylister.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(namespace).List(esLabelSelector)
	if err != nil || len(slices) == 0 {
		log.Debugf("endpoint slices of (%s, %s) not found => error %v", name, namespace, err)
		return nil
	}

	endpoints := make([]*model.IstioEndpoint, 0)
	for _, es := range slices {
		endpoints = append(endpoints, esc.buildIstioEndpoints(es, host)...)
	}

	return endpoints
}

func (esc *endpointSliceController) getServiceInfo(es interface{}) (host.Name, string, string) {
	slice := es.(*discoveryv1alpha1.EndpointSlice)
	svcName := slice.Labels[discoveryv1alpha1.LabelServiceName]
	return kube.ServiceHostname(svcName, slice.Namespace, esc.c.domainSuffix), svcName, slice.Namespace
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
	svcPort, exists := svc.Ports.GetByPort(reqSvcPort)
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
					podLabels = pod.Labels
				}

				// check that one of the input labels is a subset of the labels
				if !labelsList.HasSubsetOf(podLabels) {
					continue
				}

				builder := esc.newEndpointBuilder(pod, e)
				// identify the port by name. K8S EndpointPort uses the service port name
				for _, port := range slice.Ports {
					var portNum int32
					if port.Port != nil {
						portNum = *port.Port
					}

					if port.Name == nil ||
						svcPort.Name == *port.Name {
						istioEndpoint := builder.buildIstioEndpoint(a, portNum, svcPort.Name)
						out = append(out, &model.ServiceInstance{
							Endpoint:    istioEndpoint,
							ServicePort: svcPort,
							Service:     svc,
						})
					}
				}
			}
		}
	}
	return out, nil
}

func (esc *endpointSliceController) newEndpointBuilder(pod *v1.Pod, endpoint discoveryv1alpha1.Endpoint) *EndpointBuilder {
	if pod != nil {
		// Respect pod "istio-locality" label
		if pod.Labels[model.LocalityLabel] == "" {
			pod = pod.DeepCopy()
			// mutate the labels, only need `istio-locality`
			pod.Labels[model.LocalityLabel] = getLocalityFromTopology(endpoint.Topology)
		}
	}

	return NewEndpointBuilder(esc.c, pod)
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
