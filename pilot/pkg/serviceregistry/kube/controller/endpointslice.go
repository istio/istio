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
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	discoverylister "k8s.io/client-go/listers/discovery/v1beta1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

type endpointSliceController struct {
	kubeEndpoints
	endpointCache *endpointSliceCache
}

var _ kubeEndpointsController = &endpointSliceController{}

func newEndpointSliceController(c *Controller, informer filter.FilteredSharedIndexInformer) *endpointSliceController {
	// TODO Endpoints has a special cache, to filter out irrelevant updates to kube-system
	// Investigate if we need this, or if EndpointSlice is makes this not relevant
	out := &endpointSliceController{
		kubeEndpoints: kubeEndpoints{
			c:        c,
			informer: informer,
		},
		endpointCache: newEndpointSliceCache(),
	}
	registerHandlers(informer, c.queue, "EndpointSlice", out.onEvent, nil)
	return out
}

func (esc *endpointSliceController) getInformer() filter.FilteredSharedIndexInformer {
	return esc.informer
}

func (esc *endpointSliceController) onEvent(curr interface{}, event model.Event) error {
	ep, ok := curr.(*discovery.EndpointSlice)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", curr)
			return nil
		}
		ep, ok = tombstone.Obj.(*discovery.EndpointSlice)
		if !ok {
			log.Errorf("Tombstone contained an object that is not an endpoints slice %#v", curr)
			return nil
		}
	}

	return processEndpointEvent(esc.c, esc, ep.Labels[discovery.LabelServiceName], ep.Namespace, event, ep)
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

func sliceServiceInstances(c *Controller, ep *discovery.EndpointSlice, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := kube.ServiceHostname(ep.Labels[discovery.LabelServiceName], ep.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc == nil {
		return out
	}

	pod := c.pods.getPodByProxy(proxy)
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
							c.metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy.ID, "")
						}
					}
				}
			}
		}
	}

	return out
}

func (esc *endpointSliceController) forgetEndpoint(endpoint interface{}) {
	slice := endpoint.(*discovery.EndpointSlice)
	key := kube.KeyFunc(slice.Name, slice.Namespace)
	for _, e := range slice.Endpoints {
		for _, a := range e.Addresses {
			esc.c.pods.endpointDeleted(key, a)
		}
	}
	host, _, _ := esc.getServiceInfo(slice)
	// endpointSlice cache update
	esc.endpointCache.Delete(host, slice.Name)
}

func (esc *endpointSliceController) buildIstioEndpoints(es interface{}, host host.Name) []*model.IstioEndpoint {
	slice := es.(*discovery.EndpointSlice)
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
	esLabelSelector := klabels.Set(map[string]string{discovery.LabelServiceName: name}).AsSelectorPreValidated()
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
	slice := es.(*discovery.EndpointSlice)
	svcName := slice.Labels[discovery.LabelServiceName]
	return kube.ServiceHostname(svcName, slice.Namespace, esc.c.domainSuffix), svcName, slice.Namespace
}

func (esc *endpointSliceController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int, labelsList labels.Collection) []*model.ServiceInstance {
	esLabelSelector := klabels.Set(map[string]string{discovery.LabelServiceName: svc.Attributes.Name}).AsSelectorPreValidated()
	slices, err := discoverylister.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(svc.Attributes.Namespace).List(esLabelSelector)
	if err != nil {
		log.Infof("get endpoints(%s, %s) => error %v", svc.Attributes.Name, svc.Attributes.Namespace, err)
		return nil
	}
	if len(slices) == 0 {
		return nil
	}

	// Locate all ports in the actual service
	svcPort, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil
	}

	var out []*model.ServiceInstance
	for _, slice := range slices {
		for _, e := range slice.Endpoints {
			for _, a := range e.Addresses {
				var podLabels labels.Instance
				// TODO(@hzxuzhonghu): handle pod occurs later than endpoint
				pod := c.getPod(a, &metav1.ObjectMeta{Name: slice.Labels[discovery.LabelServiceName], Namespace: slice.Namespace}, e.TargetRef)
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
	return out
}

func (esc *endpointSliceController) newEndpointBuilder(pod *v1.Pod, endpoint discovery.Endpoint) *EndpointBuilder {
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
	if _, f := topology[label.TopologySubzone.Name]; f {
		locality += "/" + topology[label.TopologySubzone.Name]
	}
	return locality
}

// endpointKey unique identifies an endpoint by IP and port name
// This is used for deduping endpoints accros slices.
type endpointKey struct {
	ip   string
	port string
}

type endpointSliceCache struct {
	mu                            sync.RWMutex
	endpointKeysByServiceAndSlice map[host.Name]map[string][]endpointKey
	endpointByKey                 map[endpointKey]*model.IstioEndpoint
}

func newEndpointSliceCache() *endpointSliceCache {
	out := &endpointSliceCache{
		endpointKeysByServiceAndSlice: make(map[host.Name]map[string][]endpointKey),
		endpointByKey:                 make(map[endpointKey]*model.IstioEndpoint),
	}
	return out
}

func (e *endpointSliceCache) Update(hostname host.Name, slice string, endpoints []*model.IstioEndpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(endpoints) == 0 {
		for _, ip := range e.endpointKeysByServiceAndSlice[hostname][slice] {
			delete(e.endpointByKey, ip)
		}
		delete(e.endpointKeysByServiceAndSlice[hostname], slice)
	}
	if _, f := e.endpointKeysByServiceAndSlice[hostname]; !f {
		e.endpointKeysByServiceAndSlice[hostname] = make(map[string][]endpointKey)
	}
	keys := make([]endpointKey, 0, len(endpoints))
	for _, ep := range endpoints {
		key := endpointKey{ep.Address, ep.ServicePortName}
		keys = append(keys, key)
		// We will always overwrite. A conflict here means an endpoint is transitioning
		// from one slice to another See
		// https://github.com/kubernetes/website/blob/master/content/en/docs/concepts/services-networking/endpoint-slices.md#duplicate-endpoints
		// In this case, we can always assume and update is fresh, although older slices
		// we have not gotten updates may be stale; therefor we always take the new
		// update.
		e.endpointByKey[key] = ep
	}
	e.endpointKeysByServiceAndSlice[hostname][slice] = keys
}

func (e *endpointSliceCache) Delete(hostname host.Name, slice string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.endpointKeysByServiceAndSlice[hostname], slice)
	if len(e.endpointKeysByServiceAndSlice[hostname]) == 0 {
		delete(e.endpointKeysByServiceAndSlice, hostname)
	}
}

func (e *endpointSliceCache) Get(hostname host.Name) []*model.IstioEndpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var endpoints []*model.IstioEndpoint
	found := map[endpointKey]struct{}{}
	for _, keys := range e.endpointKeysByServiceAndSlice[hostname] {
		for _, key := range keys {
			if _, f := found[key]; f {
				// This a duplicate. Update() already handles conflict resolution, so we don't
				// need to pick the "right" one here.
				continue
			}
			found[key] = struct{}{}
			endpoints = append(endpoints, e.endpointByKey[key])
		}
	}
	return endpoints
}
