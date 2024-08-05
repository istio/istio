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

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/discovery/v1"
	"k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/util/sets"
)

type endpointSliceController struct {
	endpointCache *endpointSliceCache
	slices        kclient.Client[*v1.EndpointSlice]
	c             *Controller
}

var _ kubeEndpointsController = &endpointSliceController{}

var (
	endpointSliceRequirement = labelRequirement(mcs.LabelServiceName, selection.DoesNotExist, nil)
	endpointSliceSelector    = klabels.NewSelector().Add(*endpointSliceRequirement)
)

func newEndpointSliceController(c *Controller) *endpointSliceController {
	slices := kclient.NewFiltered[*v1.EndpointSlice](c.client, kclient.Filter{ObjectFilter: c.opts.GetFilter()})
	out := &endpointSliceController{
		c:             c,
		slices:        slices,
		endpointCache: newEndpointSliceCache(),
	}
	registerHandlers[*v1.EndpointSlice](c, slices, "EndpointSlice", out.onEvent, nil)
	return out
}

func (esc *endpointSliceController) sync(name, ns string, event model.Event, filtered bool) error {
	if name != "" {
		ep := esc.slices.Get(name, ns)
		if ep == nil {
			return nil
		}
		return esc.onEvent(nil, ep, event)
	}
	var err *multierror.Error
	var endpoints []*v1.EndpointSlice
	if filtered {
		endpoints = esc.slices.List(ns, klabels.Everything())
	} else {
		endpoints = esc.slices.ListUnfiltered(ns, klabels.Everything())
	}
	log.Debugf("initializing %d endpointslices", len(endpoints))
	for _, s := range endpoints {
		err = multierror.Append(err, esc.onEvent(nil, s, event))
	}
	return err.ErrorOrNil()
}

func (esc *endpointSliceController) onEvent(_, ep *v1.EndpointSlice, event model.Event) error {
	esLabels := ep.GetLabels()
	if endpointSliceSelector.Matches(klabels.Set(esLabels)) {
		return esc.processEndpointEvent(serviceNameForEndpointSlice(esLabels), ep.GetNamespace(), event, ep)
	}
	return nil
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
// TODO: this code does not return k8s service instances when the proxy's IP is a workload entry
// To tackle this, we need a ip2instance map like what we have in service entry.
func (esc *endpointSliceController) GetProxyServiceInstances(proxy *model.Proxy) []*model.ServiceInstance {
	eps := esc.slices.List(proxy.Metadata.Namespace, endpointSliceSelector)
	var out []*model.ServiceInstance
	for _, ep := range eps {
		instances := esc.sliceServiceInstances(ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func (esc *endpointSliceController) HasSynced() bool {
	return esc.slices.HasSynced()
}

func serviceNameForEndpointSlice(labels map[string]string) string {
	return labels[v1beta1.LabelServiceName]
}

func (esc *endpointSliceController) sliceServiceInstances(ep *v1.EndpointSlice, proxy *model.Proxy) []*model.ServiceInstance {
	var out []*model.ServiceInstance
	esc.endpointCache.mu.RLock()
	defer esc.endpointCache.mu.RUnlock()
	for _, svc := range esc.c.servicesForNamespacedName(getServiceNamespacedName(ep)) {
		for _, instance := range esc.endpointCache.get(svc.Hostname) {
			port, f := svc.Ports.Get(instance.ServicePortName)
			if !f {
				log.Warnf("unexpected state, svc %v missing port %v", svc.Hostname, instance.ServicePortName)
				continue
			}
			si := &model.ServiceInstance{
				Service:     svc,
				ServicePort: port,
				Endpoint:    instance,
			}
			// If the endpoint isn't ready, report this
			if instance.HealthStatus == model.UnHealthy && esc.c.opts.Metrics != nil {
				esc.c.opts.Metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy.ID, "")
			}
			out = append(out, si)
		}
	}
	return out
}

func (esc *endpointSliceController) forgetEndpoint(slice *v1.EndpointSlice) map[host.Name][]*model.IstioEndpoint {
	key := config.NamespacedName(slice)
	for _, e := range slice.Endpoints {
		for _, a := range e.Addresses {
			esc.c.pods.endpointDeleted(key, a)
		}
	}

	out := make(map[host.Name][]*model.IstioEndpoint)
	esc.endpointCache.mu.Lock()
	defer esc.endpointCache.mu.Unlock()
	for _, hostName := range esc.c.hostNamesForNamespacedName(getServiceNamespacedName(slice)) {
		// endpointSlice cache update
		if esc.endpointCache.has(hostName) {
			esc.endpointCache.delete(hostName, slice.Name)
			out[hostName] = esc.endpointCache.get(hostName)
		}
	}
	return out
}

func (esc *endpointSliceController) buildIstioEndpoints(es *v1.EndpointSlice, hostName host.Name) []*model.IstioEndpoint {
	esc.updateEndpointCacheForSlice(hostName, es)
	return esc.endpointCache.Get(hostName)
}

func endpointHealthStatus(svc *model.Service, e v1.Endpoint) model.HealthStatus {
	if e.Conditions.Ready == nil || *e.Conditions.Ready {
		return model.Healthy
	}

	if features.PersistentSessionLabel != "" &&
		svc != nil &&
		svc.Attributes.Labels[features.PersistentSessionLabel] != "" &&
		(e.Conditions.Serving == nil || *e.Conditions.Serving) &&
		(e.Conditions.Terminating == nil || *e.Conditions.Terminating) {
		return model.Draining
	}

	return model.UnHealthy
}

func (esc *endpointSliceController) updateEndpointCacheForSlice(hostName host.Name, slice *v1.EndpointSlice) {
	var endpoints []*model.IstioEndpoint
	if slice.AddressType == v1.AddressTypeFQDN {
		// TODO(https://github.com/istio/istio/issues/34995) support FQDN endpointslice
		return
	}
	svc := esc.c.GetService(hostName)
	discoverabilityPolicy := esc.c.exports.EndpointDiscoverabilityPolicy(svc)

	for _, e := range slice.Endpoints {
		// Draining tracking is only enabled if persistent sessions is enabled.
		// If we start using them for other features, this can be adjusted.
		healthStatus := endpointHealthStatus(svc, e)
		for _, a := range e.Addresses {
			pod, expectedPod := getPod(esc.c, a, &metav1.ObjectMeta{Name: slice.Name, Namespace: slice.Namespace}, e.TargetRef, hostName)
			if pod == nil && expectedPod {
				continue
			}
			builder := NewEndpointBuilder(esc.c, pod)
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

				istioEndpoint := builder.buildIstioEndpoint(a, portNum, portName, discoverabilityPolicy, healthStatus)
				endpoints = append(endpoints, istioEndpoint)
			}
		}
	}
	esc.endpointCache.Update(hostName, slice.Name, endpoints)
}

func (esc *endpointSliceController) buildIstioEndpointsWithService(name, namespace string, hostName host.Name, updateCache bool) []*model.IstioEndpoint {
	esLabelSelector := endpointSliceSelectorForService(name)
	slices := esc.slices.List(namespace, esLabelSelector)
	if len(slices) == 0 {
		log.Debugf("endpoint slices of (%s, %s) not found", name, namespace)
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

func getServiceNamespacedName(slice *v1.EndpointSlice) types.NamespacedName {
	return types.NamespacedName{
		Namespace: slice.GetNamespace(),
		Name:      serviceNameForEndpointSlice(slice.GetLabels()),
	}
}

func (esc *endpointSliceController) InstancesByPort(svc *model.Service, reqSvcPort int) []*model.ServiceInstance {
	// Locate all ports in the actual service
	svcPort, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil
	}
	var out []*model.ServiceInstance
	for _, ep := range esc.endpointCache.Get(svc.Hostname) {
		if svcPort.Name == ep.ServicePortName {
			out = append(out, &model.ServiceInstance{
				Service:     svc,
				ServicePort: svcPort,
				Endpoint:    ep,
			})
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
	e.update(hostname, slice, endpoints)
}

func (e *endpointSliceCache) update(hostname host.Name, slice string, endpoints []*model.IstioEndpoint) {
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
	// we have not gotten updates may be stale; therefore we always take the new
	// update.
	e.endpointsByServiceAndSlice[hostname][slice] = endpoints
}

func (e *endpointSliceCache) Delete(hostname host.Name, slice string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delete(hostname, slice)
}

func (e *endpointSliceCache) delete(hostname host.Name, slice string) {
	delete(e.endpointsByServiceAndSlice[hostname], slice)
	if len(e.endpointsByServiceAndSlice[hostname]) == 0 {
		delete(e.endpointsByServiceAndSlice, hostname)
	}
}

func (e *endpointSliceCache) Get(hostname host.Name) []*model.IstioEndpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.get(hostname)
}

func (e *endpointSliceCache) get(hostname host.Name) []*model.IstioEndpoint {
	var endpoints []*model.IstioEndpoint
	found := sets.New[endpointKey]()
	for _, eps := range e.endpointsByServiceAndSlice[hostname] {
		for _, ep := range eps {
			key := endpointKey{ep.Address, ep.ServicePortName}
			if found.InsertContains(key) {
				// This a duplicate. Update() already handles conflict resolution, so we don't
				// need to pick the "right" one here.
				continue
			}
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints
}

func (e *endpointSliceCache) Has(hostname host.Name) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.has(hostname)
}

func (e *endpointSliceCache) has(hostname host.Name) bool {
	_, found := e.endpointsByServiceAndSlice[hostname]
	return found
}

func endpointSliceSelectorForService(name string) klabels.Selector {
	return klabels.Set(map[string]string{
		v1beta1.LabelServiceName: name,
	}).AsSelectorPreValidated().Add(*endpointSliceRequirement)
}

// processEndpointEvent triggers the config update.
func (esc *endpointSliceController) processEndpointEvent(name string, namespace string, event model.Event, ep *v1.EndpointSlice) error {
	// Update internal endpoint cache no matter what kind of service, even headless service.
	// As for gateways, the cluster discovery type is `EDS` for headless service.
	esc.updateEDS(ep, event)
	if svc := esc.c.services.Get(name, namespace); svc != nil {
		// if the service is headless service, trigger a full push if EnableHeadlessService is true,
		// otherwise push endpoint updates - needed for NDS output.
		if svc.Spec.ClusterIP == corev1.ClusterIPNone {
			for _, modelSvc := range esc.c.servicesForNamespacedName(config.NamespacedName(svc)) {
				esc.c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full: features.EnableHeadlessService,
					// TODO: extend and set service instance type, so no need to re-init push context
					ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: modelSvc.Hostname.String(), Namespace: svc.Namespace}),

					Reason: model.NewReasonStats(model.HeadlessEndpointUpdate),
				})
				return nil
			}
		}
	}

	return nil
}

func (esc *endpointSliceController) updateEDS(ep *v1.EndpointSlice, event model.Event) {
	namespacedName := getServiceNamespacedName(ep)
	log.Debugf("Handle EDS endpoint %s %s in namespace %s", namespacedName.Name, event, namespacedName.Namespace)
	var forgottenEndpointsByHost map[host.Name][]*model.IstioEndpoint
	if event == model.EventDelete {
		forgottenEndpointsByHost = esc.forgetEndpoint(ep)
	}

	shard := model.ShardKeyFromRegistry(esc.c)

	for _, hostName := range esc.c.hostNamesForNamespacedName(namespacedName) {
		var endpoints []*model.IstioEndpoint
		if forgottenEndpointsByHost != nil {
			endpoints = forgottenEndpointsByHost[hostName]
		} else {
			endpoints = esc.buildIstioEndpoints(ep, hostName)
		}

		if features.EnableK8SServiceSelectWorkloadEntries {
			svc := esc.c.GetService(hostName)
			if svc != nil {
				fep := esc.c.collectWorkloadInstanceEndpoints(svc)
				endpoints = append(endpoints, fep...)
			} else {
				log.Debugf("Handle EDS endpoint: skip collecting workload entry endpoints, service %s/%s has not been populated",
					namespacedName.Namespace, namespacedName.Name)
			}
		}

		esc.c.opts.XDSUpdater.EDSUpdate(shard, string(hostName), namespacedName.Namespace, endpoints)
	}
}
