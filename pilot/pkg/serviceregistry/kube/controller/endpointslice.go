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

	v1 "k8s.io/api/discovery/v1"
	"k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/discovery/v1"
	listerv1beta1 "k8s.io/client-go/listers/discovery/v1beta1"
	"k8s.io/client-go/tools/cache"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	kubelib "istio.io/istio/pkg/kube"
)

type endpointSliceController struct {
	kubeEndpoints
	endpointCache *endpointSliceCache
	useV1Resource bool
}

var _ kubeEndpointsController = &endpointSliceController{}

var (
	endpointSliceRequirement = labelRequirement(mcs.LabelServiceName, selection.DoesNotExist, nil)
	endpointSliceSelector    = klabels.NewSelector().Add(*endpointSliceRequirement)
)

func newEndpointSliceController(c *Controller) *endpointSliceController {
	// TODO Endpoints has a special cache, to filter out irrelevant updates to kube-system
	// Investigate if we need this, or if EndpointSlice is makes this not relevant
	useV1Resource := endpointSliceV1Available(c.client)
	var informer cache.SharedIndexInformer
	if useV1Resource {
		informer = c.client.KubeInformer().Discovery().V1().EndpointSlices().Informer()
	} else {
		informer = c.client.KubeInformer().Discovery().V1beta1().EndpointSlices().Informer()
	}

	filteredInformer := filter.NewFilteredSharedIndexInformer(
		c.opts.DiscoveryNamespacesFilter.Filter,
		informer,
	)
	out := &endpointSliceController{
		kubeEndpoints: kubeEndpoints{
			c:        c,
			informer: filteredInformer,
		},
		useV1Resource: useV1Resource,
		endpointCache: newEndpointSliceCache(),
	}
	c.registerHandlers(filteredInformer, "EndpointSlice", out.onEvent, nil)
	return out
}

// TODO use this to automatically switch to EndpointSlice mode
func endpointSliceV1Available(client kubelib.Client) bool {
	return client != nil && kubelib.IsAtLeastVersion(client, 21)
}

func (esc *endpointSliceController) getInformer() filter.FilteredSharedIndexInformer {
	return esc.informer
}

func (esc *endpointSliceController) listSlices(ns string, selector klabels.Selector) (slices []interface{}, err error) {
	if esc.useV1Resource {
		var eps []*v1.EndpointSlice
		eps, err = listerv1.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(ns).List(selector)
		slices = make([]interface{}, len(eps))
		for i, ep := range eps {
			slices[i] = ep
		}
	} else {
		var eps []*v1beta1.EndpointSlice
		eps, err = listerv1beta1.NewEndpointSliceLister(esc.informer.GetIndexer()).EndpointSlices(ns).List(selector)
		slices = make([]interface{}, len(eps))
		for i, ep := range eps {
			slices[i] = ep
		}
	}
	return
}

func (esc *endpointSliceController) onEvent(curr interface{}, event model.Event) error {
	ep, ok := curr.(metav1.Object)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", curr)
			return nil
		}
		epGvk, ok := tombstone.Obj.(runtime.Object)
		if !ok || epGvk.GetObjectKind().GroupVersionKind().Kind != "EndpointSlice" {
			log.Errorf("Tombstone contained an object that is not an endpoints slice %#v", curr)
			return nil
		}
	}

	esLabels := ep.GetLabels()
	if endpointSliceSelector.Matches(klabels.Set(esLabels)) {
		return processEndpointEvent(esc.c, esc, serviceNameForEndpointSlice(esLabels), ep.GetNamespace(), event, ep)
	}
	return nil
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
// TODO: this code does not return k8s service instances when the proxy's IP is a workload entry
// To tackle this, we need a ip2instance map like what we have in service entry.
func (esc *endpointSliceController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance {
	eps, err := esc.listSlices(proxy.Metadata.Namespace, endpointSliceSelector)
	if err != nil {
		log.Errorf("Get endpointslice by index failed: %v", err)
		return nil
	}
	var out []*model.ServiceInstance
	for _, ep := range eps {
		instances := esc.sliceServiceInstances(c, ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func serviceNameForEndpointSlice(labels map[string]string) string {
	return labels[v1beta1.LabelServiceName]
}

func (esc *endpointSliceController) sliceServiceInstances(c *Controller, slice interface{}, proxy *model.Proxy) []*model.ServiceInstance {
	var out []*model.ServiceInstance
	ep := wrapEndpointSlice(slice)
	if ep.AddressType() == v1.AddressTypeFQDN {
		// TODO(https://github.com/istio/istio/issues/34995) support FQDN endpointslice
		return out
	}
	for _, svc := range c.servicesForNamespacedName(esc.getServiceNamespacedName(ep)) {
		pod := c.pods.getPodByProxy(proxy)
		builder := NewEndpointBuilder(c, pod)

		discoverabilityPolicy := c.exports.EndpointDiscoverabilityPolicy(svc)

		for _, port := range ep.Ports() {
			if port.Name == nil || port.Port == nil {
				continue
			}
			svcPort, exists := svc.Ports.Get(*port.Name)
			if !exists {
				continue
			}
			// consider multiple IP scenarios
			for _, ip := range proxy.IPAddresses {
				for _, ep := range ep.Endpoints() {
					for _, a := range ep.Addresses {
						if a == ip {
							istioEndpoint := builder.buildIstioEndpoint(ip, *port.Port, svcPort.Name, discoverabilityPolicy)
							out = append(out, &model.ServiceInstance{
								Endpoint:    istioEndpoint,
								ServicePort: svcPort,
								Service:     svc,
							})
							// If the endpoint isn't ready, report this
							if ep.Conditions.Ready != nil && !*ep.Conditions.Ready && c.opts.Metrics != nil {
								c.opts.Metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy.ID, "")
							}
						}
					}
				}
			}
		}
	}

	return out
}

func (esc *endpointSliceController) forgetEndpoint(endpoint interface{}) map[host.Name][]*model.IstioEndpoint {
	slice := wrapEndpointSlice(endpoint)
	key := kube.KeyFunc(slice.Name, slice.Namespace)
	for _, e := range slice.Endpoints() {
		for _, a := range e.Addresses {
			esc.c.pods.endpointDeleted(key, a)
		}
	}

	out := make(map[host.Name][]*model.IstioEndpoint)
	for _, hostName := range esc.c.hostNamesForNamespacedName(esc.getServiceNamespacedName(slice)) {
		// endpointSlice cache update
		if esc.endpointCache.Has(hostName) {
			esc.endpointCache.Delete(hostName, slice.Name)
			out[hostName] = esc.endpointCache.Get(hostName)
		}
	}
	return out
}

func (esc *endpointSliceController) buildIstioEndpoints(es interface{}, hostName host.Name) []*model.IstioEndpoint {
	esc.updateEndpointCacheForSlice(hostName, es)
	return esc.endpointCache.Get(hostName)
}

func (esc *endpointSliceController) updateEndpointCacheForSlice(hostName host.Name, ep interface{}) {
	var endpoints []*model.IstioEndpoint
	slice := wrapEndpointSlice(ep)
	if slice.AddressType() == v1.AddressTypeFQDN {
		// TODO(https://github.com/istio/istio/issues/34995) support FQDN endpointslice
		return
	}
	discoverabilityPolicy := esc.c.exports.EndpointDiscoverabilityPolicy(esc.c.GetService(hostName))

	for _, e := range slice.Endpoints() {
		if !features.SendUnhealthyEndpoints {
			if e.Conditions.Ready != nil && !*e.Conditions.Ready {
				// Ignore not ready endpoints
				continue
			}
		}
		ready := e.Conditions.Ready == nil || *e.Conditions.Ready
		for _, a := range e.Addresses {
			pod, expectedPod := getPod(esc.c, a, &metav1.ObjectMeta{Name: slice.Name, Namespace: slice.Namespace}, e.TargetRef, hostName)
			if pod == nil && expectedPod {
				continue
			}
			builder := NewEndpointBuilder(esc.c, pod)
			// EDS and ServiceEntry use name for service port - ADS will need to map to numbers.
			for _, port := range slice.Ports() {
				var portNum int32
				if port.Port != nil {
					portNum = *port.Port
				}
				var portName string
				if port.Name != nil {
					portName = *port.Name
				}

				istioEndpoint := builder.buildIstioEndpoint(a, portNum, portName, discoverabilityPolicy)
				if ready {
					istioEndpoint.HealthStatus = model.Healthy
				} else {
					istioEndpoint.HealthStatus = model.UnHealthy
				}
				endpoints = append(endpoints, istioEndpoint)
			}
		}
	}
	esc.endpointCache.Update(hostName, slice.Name, endpoints)
}

func (esc *endpointSliceController) buildIstioEndpointsWithService(name, namespace string, hostName host.Name, updateCache bool) []*model.IstioEndpoint {
	esLabelSelector := endpointSliceSelectorForService(name)
	slices, err := esc.listSlices(namespace, esLabelSelector)
	if err != nil || len(slices) == 0 {
		log.Debugf("endpoint slices of (%s, %s) not found => error %v", name, namespace, err)
		return nil
	}

	if updateCache {
		// A cache update was requested. Rebuild the endpoints for these slices.
		for _, slice := range slices {
			esc.updateEndpointCacheForSlice(hostName, slice)
		}
	}

	return esc.endpointCache.Get(hostName)
}

func (esc *endpointSliceController) getServiceNamespacedName(es interface{}) types.NamespacedName {
	slice := es.(metav1.Object)
	return types.NamespacedName{
		Namespace: slice.GetNamespace(),
		Name:      serviceNameForEndpointSlice(slice.GetLabels()),
	}
}

func (esc *endpointSliceController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int, lbls labels.Instance) []*model.ServiceInstance {
	esLabelSelector := endpointSliceSelectorForService(svc.Attributes.Name)
	slices, err := esc.listSlices(svc.Attributes.Namespace, esLabelSelector)
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

	discoverabilityPolicy := c.exports.EndpointDiscoverabilityPolicy(svc)

	var out []*model.ServiceInstance
	for _, es := range slices {
		slice := wrapEndpointSlice(es)
		if slice.AddressType() == v1.AddressTypeFQDN {
			// TODO(https://github.com/istio/istio/issues/34995) support FQDN endpointslice
			continue
		}
		for _, e := range slice.Endpoints() {
			for _, a := range e.Addresses {
				var podLabels labels.Instance
				pod, expectedPod := getPod(c, a, &metav1.ObjectMeta{Name: slice.Name, Namespace: slice.Namespace}, e.TargetRef, svc.Hostname)
				if pod == nil && expectedPod {
					continue
				}
				if pod != nil {
					podLabels = pod.Labels
				}
				// check that one of the input labels is a subset of the labels
				if !lbls.SubsetOf(podLabels) {
					continue
				}

				builder := NewEndpointBuilder(esc.c, pod)
				// identify the port by name. K8S EndpointPort uses the service port name
				for _, port := range slice.Ports() {
					var portNum int32
					if port.Port != nil {
						portNum = *port.Port
					}

					if port.Name == nil ||
						svcPort.Name == *port.Name {
						istioEndpoint := builder.buildIstioEndpoint(a, portNum, svcPort.Name, discoverabilityPolicy)
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

// endpointKey unique identifies an endpoint by IP and port name
// This is used for deduping endpoints across slices.
type endpointKey struct {
	ip   string
	port string
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
	// We will always overwrite. A conflict here means an endpoint is transitioning
	// from one slice to another See
	// https://github.com/kubernetes/website/blob/master/content/en/docs/concepts/services-networking/endpoint-slices.md#duplicate-endpoints
	// In this case, we can always assume and update is fresh, although older slices
	// we have not gotten updates may be stale; therefor we always take the new
	// update.
	e.endpointsByServiceAndSlice[hostname][slice] = endpoints
}

func (e *endpointSliceCache) Delete(hostname host.Name, slice string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.endpointsByServiceAndSlice[hostname], slice)
	if len(e.endpointsByServiceAndSlice[hostname]) == 0 {
		delete(e.endpointsByServiceAndSlice, hostname)
	}
}

func (e *endpointSliceCache) Get(hostname host.Name) []*model.IstioEndpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var endpoints []*model.IstioEndpoint
	found := map[endpointKey]struct{}{}
	for _, eps := range e.endpointsByServiceAndSlice[hostname] {
		for _, ep := range eps {
			key := endpointKey{ep.Address, ep.ServicePortName}
			if _, f := found[key]; f {
				// This a duplicate. Update() already handles conflict resolution, so we don't
				// need to pick the "right" one here.
				continue
			}
			found[key] = struct{}{}
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints
}

func (e *endpointSliceCache) Has(hostname host.Name) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, found := e.endpointsByServiceAndSlice[hostname]
	return found
}

func endpointSliceSelectorForService(name string) klabels.Selector {
	return klabels.Set(map[string]string{
		v1beta1.LabelServiceName: name,
	}).AsSelectorPreValidated().Add(*endpointSliceRequirement)
}

func wrapEndpointSlice(slice interface{}) *endpointSliceWrapper {
	switch es := slice.(type) {
	case *v1.EndpointSlice:
		return &endpointSliceWrapper{ObjectMeta: es.ObjectMeta, v1: es}
	case *v1beta1.EndpointSlice:
		return &endpointSliceWrapper{ObjectMeta: es.ObjectMeta, v1beta1: es}
	}
	return nil
}

type endpointSliceWrapper struct {
	metav1.ObjectMeta
	v1beta1 *v1beta1.EndpointSlice
	v1      *v1.EndpointSlice
}

func (esw *endpointSliceWrapper) AddressType() v1.AddressType {
	if esw.v1 != nil {
		return esw.v1.AddressType
	}
	return v1.AddressType(esw.v1beta1.AddressType)
}

func (esw *endpointSliceWrapper) Ports() []v1.EndpointPort {
	if esw.v1 != nil {
		return esw.v1.Ports
	}
	out := make([]v1.EndpointPort, len(esw.v1beta1.Ports))
	for i, p := range esw.v1beta1.Ports {
		out[i] = v1.EndpointPort{
			Name:        p.Name,
			Protocol:    p.Protocol,
			Port:        p.Port,
			AppProtocol: p.AppProtocol,
		}
	}
	return out
}

func (esw *endpointSliceWrapper) Endpoints() []v1.Endpoint {
	if esw.v1 != nil {
		return esw.v1.Endpoints
	}
	out := make([]v1.Endpoint, len(esw.v1beta1.Endpoints))
	for i, ep := range esw.v1beta1.Endpoints {
		zone := ep.Topology[NodeZoneLabelGA]

		var fz []v1.ForZone
		if ep.Hints != nil {
			fz = make([]v1.ForZone, len(ep.Hints.ForZones))
			for i, el := range fz {
				fz[i] = v1.ForZone{Name: el.Name}
			}
		}

		out[i] = v1.Endpoint{
			Addresses: ep.Addresses,
			Conditions: v1.EndpointConditions{
				Ready:       ep.Conditions.Ready,
				Serving:     ep.Conditions.Serving,
				Terminating: ep.Conditions.Serving,
			},
			Hostname:           ep.Hostname,
			TargetRef:          ep.TargetRef,
			DeprecatedTopology: ep.Topology,
			NodeName:           ep.NodeName,
			Zone:               &zone,
			Hints: &v1.EndpointHints{
				ForZones: fz,
			},
		}
	}
	return out
}
