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
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type endpointSliceController struct {
	endpointCache *endpointSliceCache
	slices        kclient.Client[*v1.EndpointSlice]
	c             *Controller
}

var (
	endpointSliceRequirement = labelRequirement(mcs.LabelServiceName, selection.DoesNotExist, nil)
	endpointSliceSelector    = klabels.NewSelector().Add(*endpointSliceRequirement)
)

func newEndpointSliceController(c *Controller) *endpointSliceController {
	slices := kclient.NewFiltered[*v1.EndpointSlice](c.client, kclient.Filter{ObjectFilter: c.client.ObjectFilter()})
	out := &endpointSliceController{
		c:             c,
		slices:        slices,
		endpointCache: newEndpointSliceCache(),
	}
	registerHandlers[*v1.EndpointSlice](c, slices, "EndpointSlice", out.onEvent, nil)
	return out
}

func (esc *endpointSliceController) podArrived(name, ns string) error {
	ep := esc.slices.Get(name, ns)
	if ep == nil {
		return nil
	}
	return esc.onEvent(nil, ep, model.EventAdd)
}

// initializeNamespace initializes endpoints for a given namespace.
func (esc *endpointSliceController) initializeNamespace(ns string, filtered bool) error {
	var err *multierror.Error
	var endpoints []*v1.EndpointSlice
	if filtered {
		endpoints = esc.slices.List(ns, klabels.Everything())
	} else {
		endpoints = esc.slices.ListUnfiltered(ns, klabels.Everything())
	}
	log.Debugf("initializing %d endpointslices", len(endpoints))
	for _, s := range endpoints {
		err = multierror.Append(err, esc.onEvent(nil, s, model.EventAdd))
	}
	return err.ErrorOrNil()
}

func (esc *endpointSliceController) onEvent(_, ep *v1.EndpointSlice, event model.Event) error {
	esc.onEventInternal(nil, ep, event)
	return nil
}

func (esc *endpointSliceController) onEventInternal(_, ep *v1.EndpointSlice, event model.Event) {
	esLabels := ep.GetLabels()
	if !endpointSliceSelector.Matches(klabels.Set(esLabels)) {
		return
	}
	// Update internal endpoint cache no matter what kind of service, even headless service.
	// As for gateways, the cluster discovery type is `EDS` for headless service.
	namespacedName := getServiceNamespacedName(ep)
	log.Debugf("Handle EDS endpoint %s %s in namespace %s", namespacedName.Name, event, namespacedName.Namespace)
	if event == model.EventDelete {
		esc.deleteEndpointSlice(ep)
	} else {
		esc.updateEndpointSlice(ep)
	}

	// Now check if we need to do a full push for the service.
	// If the service is headless, we need to do a full push if service exposes TCP ports
	// to create IP based listeners. For pure HTTP headless services, we only need to push NDS.
	name := serviceNameForEndpointSlice(esLabels)
	namespace := ep.GetNamespace()
	svc := esc.c.services.Get(name, namespace)
	if svc != nil && !serviceNeedsPush(svc) {
		return
	}

	hostnames := esc.c.hostNamesForNamespacedName(namespacedName)
	log.Debugf("triggering EDS push for %s in namespace %s", hostnames, namespacedName.Namespace)
	// Trigger EDS push for all hostnames.
	esc.pushEDS(hostnames, namespacedName.Namespace)

	if svc == nil || svc.Spec.ClusterIP != corev1.ClusterIPNone || svc.Spec.Type == corev1.ServiceTypeExternalName {
		return
	}

	configsUpdated := sets.New[model.ConfigKey]()
	supportsOnlyHTTP := true
	for _, modelSvc := range esc.c.servicesForNamespacedName(config.NamespacedName(svc)) {
		for _, p := range modelSvc.Ports {
			if !p.Protocol.IsHTTP() {
				supportsOnlyHTTP = false
				break
			}
		}
		if supportsOnlyHTTP {
			// pure HTTP headless services should not need a full push since they do not
			// require a Listener based on IP: https://github.com/istio/istio/issues/48207
			configsUpdated.Insert(model.ConfigKey{Kind: kind.DNSName, Name: modelSvc.Hostname.String(), Namespace: svc.Namespace})
		} else {
			configsUpdated.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: modelSvc.Hostname.String(), Namespace: svc.Namespace})
		}
	}

	if len(configsUpdated) > 0 {
		esc.c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: configsUpdated,
			Reason:         model.NewReasonStats(model.HeadlessEndpointUpdate),
		})
	}
}

func serviceNeedsPush(svc *corev1.Service) bool {
	if svc.Annotations[annotation.NetworkingExportTo.Name] != "" {
		namespaces := strings.Split(svc.Annotations[annotation.NetworkingExportTo.Name], ",")
		for _, ns := range namespaces {
			ns = strings.TrimSpace(ns)
			if ns == string(visibility.None) {
				return false
			}
		}
	}
	return true
}

// GetProxyServiceTargets returns service instances co-located with a given proxy
// This is only used to find the targets associated with a headless service.
// For the service with selector, it will use GetProxyServiceTargetsByPod to get the service targets.
func (esc *endpointSliceController) GetProxyServiceTargets(proxy *model.Proxy) []model.ServiceTarget {
	eps := esc.slices.List(proxy.Metadata.Namespace, endpointSliceSelector)
	var out []model.ServiceTarget
	for _, ep := range eps {
		instances := esc.serviceTargets(ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func serviceNameForEndpointSlice(labels map[string]string) string {
	return labels[v1.LabelServiceName]
}

func (esc *endpointSliceController) serviceTargets(ep *v1.EndpointSlice, proxy *model.Proxy) []model.ServiceTarget {
	var out []model.ServiceTarget
	esc.endpointCache.mu.RLock()
	defer esc.endpointCache.mu.RUnlock()
	for _, svc := range esc.c.servicesForNamespacedName(getServiceNamespacedName(ep)) {
		for _, instance := range esc.endpointCache.get(svc.Hostname) {
			port, f := svc.Ports.Get(instance.ServicePortName)
			if !f {
				log.Warnf("unexpected state, svc %v missing port %v", svc.Hostname, instance.ServicePortName)
				continue
			}
			// The comparison works because both IstioEndpoint and Proxy always use the first PodIP (as provided by Kubernetes)
			// as the first entry of their respective lists.
			if proxy.IPAddresses[0] != instance.FirstAddressOrNil() {
				continue
			}
			// If the endpoint isn't ready, report this
			if instance.HealthStatus == model.UnHealthy && esc.c.opts.Metrics != nil {
				esc.c.opts.Metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy.ID, "")
			}
			si := model.ServiceTarget{
				Service: svc,
				Port: model.ServiceInstancePort{
					ServicePort: port,
					TargetPort:  instance.EndpointPort,
				},
			}
			out = append(out, si)
		}
	}
	return out
}

func (esc *endpointSliceController) deleteEndpointSlice(slice *v1.EndpointSlice) {
	key := config.NamespacedName(slice)
	for _, e := range slice.Endpoints {
		for _, a := range e.Addresses {
			esc.c.pods.endpointDeleted(key, a)
		}
	}

	esc.endpointCache.mu.Lock()
	defer esc.endpointCache.mu.Unlock()
	for _, hostName := range esc.c.hostNamesForNamespacedName(getServiceNamespacedName(slice)) {
		// endpointSlice cache update
		if esc.endpointCache.has(hostName) {
			esc.endpointCache.delete(hostName, slice.Name)
		}
	}
}

func (esc *endpointSliceController) updateEndpointSlice(slice *v1.EndpointSlice) {
	for _, hostname := range esc.c.hostNamesForNamespacedName(getServiceNamespacedName(slice)) {
		esc.updateEndpointCacheForSlice(hostname, slice)
	}
}

func endpointHealthStatus(svc *model.Service, e v1.Endpoint) model.HealthStatus {
	if e.Conditions.Ready == nil || *e.Conditions.Ready {
		return model.Healthy
	}

	// An endpoint is draining only if it was previously ready (serving == true) and persistent sessions is enabled
	if svc != nil && svc.SupportsDrainingEndpoints() &&
		(e.Conditions.Serving == nil || *e.Conditions.Serving) &&
		(e.Conditions.Terminating == nil || *e.Conditions.Terminating) {
		return model.Draining
	}

	// If it is shutting down, mark it as terminating. This occurs regardless of whether it was previously healthy or not.
	if svc != nil &&
		(e.Conditions.Terminating == nil || *e.Conditions.Terminating) {
		return model.Terminating
	}

	return model.UnHealthy
}

func (esc *endpointSliceController) updateEndpointCacheForSlice(hostName host.Name, epSlice *v1.EndpointSlice) {
	var endpoints []*model.IstioEndpoint
	if epSlice.AddressType == v1.AddressTypeFQDN {
		// TODO(https://github.com/istio/istio/issues/34995) support FQDN endpointslice
		return
	}
	svc := esc.c.GetService(hostName)
	svcNamespacedName := getServiceNamespacedName(epSlice)
	// This is not a endpointslice for service, ignore
	if svcNamespacedName.Name == "" {
		return
	}

	svcCore := esc.c.services.Get(svcNamespacedName.Name, svcNamespacedName.Namespace)
	discoverabilityPolicy := esc.c.exports.EndpointDiscoverabilityPolicy(svc)
	for _, e := range epSlice.Endpoints {
		// Draining tracking is only enabled if persistent sessions is enabled.
		// If we start using them for other features, this can be adjusted.
		healthStatus := endpointHealthStatus(svc, e)
		for _, a := range e.Addresses {
			pod, expectedPod := getPod(esc.c, a, &metav1.ObjectMeta{Name: epSlice.Name, Namespace: epSlice.Namespace}, e.TargetRef, hostName)
			if pod == nil && expectedPod {
				continue
			}

			var overrideAddresses []string
			// If not expect a pod, it means this is not an endpointslice not managed by kubernetes.
			// We do not add all pod ips to the istio endpoint.
			if features.EnableDualStack && expectedPod && svcCore != nil && len(pod.Status.PodIPs) > 1 && len(svcCore.Spec.ClusterIPs) > 1 {
				if epSlice.AddressType == v1.AddressTypeIPv6 {
					// For endpointslice with targetRef and the pod has dual stack ip.
					// We ignore ipv6 family address to prevent generating duplicate IstioEndpoints.
					continue
				}
				// get the IP addresses for the dual stack pod
				overrideAddresses = slices.Map(pod.Status.PodIPs, func(e corev1.PodIP) string {
					return e.IP
				})
			}

			builder := esc.c.NewEndpointBuilder(pod)
			// EDS and ServiceEntry use name for service port - ADS will need to map to numbers.
			for _, port := range epSlice.Ports {
				var portNum int32
				if port.Port != nil {
					portNum = *port.Port
				}
				var portName string
				if port.Name != nil {
					portName = *port.Name
				}

				istioEndpoint := builder.buildIstioEndpoint(a, portNum, portName, discoverabilityPolicy, healthStatus, svc.SupportsUnhealthyEndpoints())
				if len(overrideAddresses) > 1 {
					istioEndpoint.Addresses = overrideAddresses
				}
				endpoints = append(endpoints, istioEndpoint)
			}
		}
	}
	esc.endpointCache.Update(hostName, epSlice.Name, endpoints)
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
			key := endpointKey{ep.FirstAddressOrNil(), ep.ServicePortName}
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
		v1.LabelServiceName: name,
	}).AsSelectorPreValidated().Add(*endpointSliceRequirement)
}

func (esc *endpointSliceController) pushEDS(hostnames []host.Name, namespace string) {
	shard := model.ShardKeyFromRegistry(esc.c)
	// Even though we just read from the cache, we need the full lock to ensure pushEDS
	// runs sequentially when `EnableK8SServiceSelectWorkloadEntries` is enabled. Otherwise,
	// we may end up with eds updates can go out of order with workload entry updates causing
	// incorrect endpoints. For regular endpoint updates, pushEDS is already serialized
	// because the events are queued.
	esc.endpointCache.mu.Lock()
	defer esc.endpointCache.mu.Unlock()
	for _, hostname := range hostnames {
		endpoints := esc.endpointCache.get(hostname)
		if features.EnableK8SServiceSelectWorkloadEntries {
			svc := esc.c.GetService(hostname)
			if svc != nil {
				fep := esc.c.collectWorkloadInstanceEndpoints(svc)
				endpoints = append(endpoints, fep...)
			} else {
				log.Debugf("Handle EDS endpoint: skip collecting workload entry endpoints, service %s/ has not been populated",
					hostname)
			}
		}

		esc.c.opts.XDSUpdater.EDSUpdate(shard, string(hostname), namespace, endpoints)
	}
}

// getPod fetches a pod by name or IP address.
// A pod may be missing (nil) for two reasons:
//   - It is an endpoint without an associated Pod. In this case, expectPod will be false.
//   - It is an endpoint with an associate Pod, but its not found. In this case, expectPod will be true.
//     this may happen due to eventually consistency issues, out of order events, etc. In this case, the caller
//     should not precede with the endpoint, or inaccurate information would be sent which may have impacts on
//     correctness and security.
//
// Note: this is only used by endpointslice controller
func getPod(c *Controller, ip string, ep *metav1.ObjectMeta, targetRef *corev1.ObjectReference, host host.Name) (*corev1.Pod, bool) {
	var expectPod bool
	pod := c.getPod(ip, ep.Namespace, targetRef)
	if targetRef != nil && targetRef.Kind == kind.Pod.String() {
		expectPod = true
		if pod == nil {
			c.registerEndpointResync(ep, ip, host)
		}
	}

	return pod, expectPod
}

func (c *Controller) registerEndpointResync(ep *metav1.ObjectMeta, ip string, host host.Name) {
	// This means, the endpoint event has arrived before pod event.
	// This might happen because PodCache is eventually consistent.
	log.Debugf("Endpoint without pod %s %s.%s", ip, ep.Name, ep.Namespace)
	endpointsWithNoPods.Increment()
	if c.opts.Metrics != nil {
		c.opts.Metrics.AddMetric(model.EndpointNoPod, string(host), "", ip)
	}
	// Tell pod cache we want to queue the endpoint event when this pod arrives.
	c.pods.queueEndpointEventOnPodArrival(config.NamespacedName(ep), ip)
}

// getPod fetches a pod by name or IP address.
// A pod may be missing (nil) for two reasons:
// * It is an endpoint without an associated Pod.
// * It is an endpoint with an associate Pod, but its not found.
func (c *Controller) getPod(ip string, namespace string, targetRef *corev1.ObjectReference) *corev1.Pod {
	if targetRef != nil && targetRef.Kind == kind.Pod.String() {
		key := types.NamespacedName{Name: targetRef.Name, Namespace: targetRef.Namespace}
		pod := c.pods.getPodByKey(key)
		return pod
	}
	// This means the endpoint is manually controlled
	// We will want to lookup a pod to find metadata like service account, labels, etc. But for hostNetwork, we just get a raw IP,
	// and the IP may be shared by many pods. Best we can do is guess.
	pods := c.pods.getPodsByIP(ip)
	for _, p := range pods {
		if p.Namespace == namespace {
			// Might not be right, but best we can do.
			return p
		}
	}
	return nil
}
