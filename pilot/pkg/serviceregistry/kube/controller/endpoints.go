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
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/util/sets"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
)

type endpointsController struct {
	endpoints kclient.Client[*v1.Endpoints]
	c         *Controller
}

var _ kubeEndpointsController = &endpointsController{}

func newEndpointsController(c *Controller) *endpointsController {
	endpoints := kclient.NewFiltered[*v1.Endpoints](c.client, kclient.Filter{ObjectFilter: c.opts.GetFilter()})
	out := &endpointsController{
		endpoints: endpoints,
		c:         c,
	}
	registerHandlers[*v1.Endpoints](c, endpoints, "Endpoints", out.onEvent, endpointsEqual)
	return out
}

func (e *endpointsController) HasSynced() bool {
	return e.endpoints.HasSynced()
}

func (e *endpointsController) GetProxyServiceInstances(proxy *model.Proxy) []*model.ServiceInstance {
	eps := e.endpoints.List(proxy.Metadata.Namespace, klabels.Everything())
	var out []*model.ServiceInstance
	for _, ep := range eps {
		instances := e.endpointServiceInstances(ep, proxy)
		out = append(out, instances...)
	}
	return out
}

func (e *endpointsController) endpointServiceInstances(endpoints *v1.Endpoints, proxy *model.Proxy) []*model.ServiceInstance {
	var out []*model.ServiceInstance

	for _, svc := range e.c.servicesForNamespacedName(config.NamespacedName(endpoints)) {
		pod := e.c.pods.getPodByProxy(proxy)
		builder := NewEndpointBuilder(e.c, pod)

		discoverabilityPolicy := e.c.exports.EndpointDiscoverabilityPolicy(svc)

		for _, ss := range endpoints.Subsets {
			for _, port := range ss.Ports {
				svcPort, exists := svc.Ports.Get(port.Name)
				if !exists {
					continue
				}

				// consider multiple IP scenarios
				for _, ip := range proxy.IPAddresses {
					if e.hasProxyIP(ss.Addresses, ip) || e.hasProxyIP(ss.NotReadyAddresses, ip) {
						istioEndpoint := builder.buildIstioEndpoint(ip, port.Port, svcPort.Name, discoverabilityPolicy, model.Healthy)
						out = append(out, &model.ServiceInstance{
							Endpoint:    istioEndpoint,
							ServicePort: svcPort,
							Service:     svc,
						})
					}

					if e.hasProxyIP(ss.NotReadyAddresses, ip) {
						if e.c.opts.Metrics != nil {
							e.c.opts.Metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy.ID, "")
						}
					}
				}
			}
		}
	}

	return out
}

func (e *endpointsController) InstancesByPort(svc *model.Service, reqSvcPort int) []*model.ServiceInstance {
	ep := e.endpoints.Get(svc.Attributes.Name, svc.Attributes.Namespace)
	if ep == nil {
		return nil
	}
	discoverabilityPolicy := e.c.exports.EndpointDiscoverabilityPolicy(svc)

	// Locate all ports in the actual service
	svcPort, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil
	}
	var out []*model.ServiceInstance
	for _, ss := range ep.Subsets {
		out = append(out, e.buildServiceInstances(ep, ss, ss.Addresses, svc, discoverabilityPolicy, svcPort, model.Healthy)...)
		if features.SendUnhealthyEndpoints.Load() {
			out = append(out, e.buildServiceInstances(ep, ss, ss.NotReadyAddresses, svc, discoverabilityPolicy, svcPort, model.UnHealthy)...)
		}
	}
	return out
}

func (e *endpointsController) sync(name, ns string, event model.Event, filtered bool) error {
	if name != "" {
		ep := e.endpoints.Get(name, ns)
		if ep == nil {
			return nil
		}
		return e.onEvent(nil, ep, model.EventAdd)
	}
	var err *multierror.Error
	var endpoints []*v1.Endpoints
	if filtered {
		endpoints = e.endpoints.List(ns, klabels.Everything())
	} else {
		endpoints = e.endpoints.ListUnfiltered(ns, klabels.Everything())
	}
	log.Debugf("syncing %d endpoints", len(endpoints))
	for _, s := range endpoints {
		err = multierror.Append(err, e.onEvent(nil, s, event))
	}
	return err.ErrorOrNil()
}

func (e *endpointsController) onEvent(_, ep *v1.Endpoints, event model.Event) error {
	return e.processEndpointEvent(ep.Name, ep.Namespace, event, ep)
}

func (e *endpointsController) forgetEndpoint(ep *v1.Endpoints) map[host.Name][]*model.IstioEndpoint {
	key := config.NamespacedName(ep)
	for _, ss := range ep.Subsets {
		for _, ea := range ss.Addresses {
			e.c.pods.endpointDeleted(key, ea.IP)
		}
	}
	return make(map[host.Name][]*model.IstioEndpoint)
}

func (e *endpointsController) buildIstioEndpoints(endpoint *v1.Endpoints, host host.Name) []*model.IstioEndpoint {
	var endpoints []*model.IstioEndpoint

	discoverabilityPolicy := e.c.exports.EndpointDiscoverabilityPolicy(e.c.GetService(host))

	for _, ss := range endpoint.Subsets {
		endpoints = append(endpoints, e.buildIstioEndpointFromAddress(endpoint, ss, ss.Addresses, host, discoverabilityPolicy, model.Healthy)...)
		if features.SendUnhealthyEndpoints.Load() {
			endpoints = append(endpoints, e.buildIstioEndpointFromAddress(endpoint, ss, ss.NotReadyAddresses, host, discoverabilityPolicy, model.UnHealthy)...)
		}
	}
	return endpoints
}

func (e *endpointsController) buildServiceInstances(ep *v1.Endpoints, ss v1.EndpointSubset, endpoints []v1.EndpointAddress,
	svc *model.Service, discoverabilityPolicy model.EndpointDiscoverabilityPolicy,
	svcPort *model.Port, health model.HealthStatus,
) []*model.ServiceInstance {
	var out []*model.ServiceInstance
	for _, ea := range endpoints {
		pod, expectedPod := getPod(e.c, ea.IP, &metav1.ObjectMeta{Name: ep.Name, Namespace: ep.Namespace}, ea.TargetRef, svc.Hostname)
		if pod == nil && expectedPod {
			continue
		}

		builder := NewEndpointBuilder(e.c, pod)

		// identify the port by name. K8S EndpointPort uses the service port name
		for _, port := range ss.Ports {
			if port.Name == "" || // 'name optional if single port is defined'
				svcPort.Name == port.Name {
				istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, svcPort.Name, discoverabilityPolicy, model.Healthy)
				istioEndpoint.HealthStatus = health
				out = append(out, &model.ServiceInstance{
					Endpoint:    istioEndpoint,
					ServicePort: svcPort,
					Service:     svc,
				})
			}
		}
	}
	return out
}

func (e *endpointsController) buildIstioEndpointFromAddress(ep *v1.Endpoints, ss v1.EndpointSubset, endpoints []v1.EndpointAddress,
	host host.Name, discoverabilityPolicy model.EndpointDiscoverabilityPolicy, health model.HealthStatus,
) []*model.IstioEndpoint {
	var istioEndpoints []*model.IstioEndpoint
	for _, ea := range endpoints {
		pod, expectedPod := getPod(e.c, ea.IP, &metav1.ObjectMeta{Name: ep.Name, Namespace: ep.Namespace}, ea.TargetRef, host)
		if pod == nil && expectedPod {
			continue
		}
		builder := NewEndpointBuilder(e.c, pod)
		// EDS and ServiceEntry use name for service port - ADS will need to map to numbers.
		for _, port := range ss.Ports {
			istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, port.Name, discoverabilityPolicy, health)
			istioEndpoints = append(istioEndpoints, istioEndpoint)
		}
	}
	return istioEndpoints
}

func (e *endpointsController) buildIstioEndpointsWithService(name, namespace string, host host.Name, _ bool) []*model.IstioEndpoint {
	ep := e.endpoints.Get(name, namespace)
	if ep == nil {
		log.Debugf("endpoints(%s, %s) not found", name, namespace)
		return nil
	}

	return e.buildIstioEndpoints(ep, host)
}

// processEndpointEvent triggers the config update.
func (e *endpointsController) processEndpointEvent(name string, namespace string, event model.Event, ep *v1.Endpoints) error {
	// Update internal endpoint cache no matter what kind of service, even headless service.
	// As for gateways, the cluster discovery type is `EDS` for headless service.
	e.updateEDS(ep, event)
	if svc := e.c.services.Get(name, namespace); svc != nil {
		// if the service is headless service, trigger a full push if EnableHeadlessService is true,
		// otherwise push endpoint updates - needed for NDS output.
		if svc.Spec.ClusterIP == v1.ClusterIPNone {
			for _, modelSvc := range e.c.servicesForNamespacedName(config.NamespacedName(svc)) {
				e.c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
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

func (e *endpointsController) updateEDS(ep *v1.Endpoints, event model.Event) {
	namespacedName := config.NamespacedName(ep)
	log.Debugf("Handle EDS endpoint %s %s in namespace %s", namespacedName.Name, event, namespacedName.Namespace)
	var forgottenEndpointsByHost map[host.Name][]*model.IstioEndpoint
	if event == model.EventDelete {
		forgottenEndpointsByHost = e.forgetEndpoint(ep)
	}

	shard := model.ShardKeyFromRegistry(e.c)

	for _, hostName := range e.c.hostNamesForNamespacedName(namespacedName) {
		var endpoints []*model.IstioEndpoint
		if forgottenEndpointsByHost != nil {
			endpoints = forgottenEndpointsByHost[hostName]
		} else {
			endpoints = e.buildIstioEndpoints(ep, hostName)
		}

		if features.EnableK8SServiceSelectWorkloadEntries {
			svc := e.c.GetService(hostName)
			if svc != nil {
				fep := e.c.collectWorkloadInstanceEndpoints(svc)
				endpoints = append(endpoints, fep...)
			} else {
				log.Debugf("Handle EDS endpoint: skip collecting workload entry endpoints, service %s/%s has not been populated",
					namespacedName.Namespace, namespacedName.Name)
			}
		}

		e.c.opts.XDSUpdater.EDSUpdate(shard, string(hostName), namespacedName.Namespace, endpoints)
	}
}
func (e *endpointsController) hasProxyIP(addresses []v1.EndpointAddress, proxyIP string) bool {
	for _, addr := range addresses {
		if addr.IP == proxyIP {
			return true
		}
	}
	return false
}

// endpointsEqual returns true if the two endpoints are the same in aspects Pilot cares about
// This currently means only looking at "Ready" endpoints
func endpointsEqual(a, b *v1.Endpoints) bool {
	if len(a.Subsets) != len(b.Subsets) {
		return false
	}
	for i := range a.Subsets {
		if !portsEqual(a.Subsets[i].Ports, b.Subsets[i].Ports) {
			return false
		}
		if !addressesEqual(a.Subsets[i].Addresses, b.Subsets[i].Addresses) {
			return false
		}
	}
	return true
}

func portsEqual(a, b []v1.EndpointPort) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Name != b[i].Name || a[i].Port != b[i].Port || a[i].Protocol != b[i].Protocol ||
			ptrValueOrEmpty(a[i].AppProtocol) != ptrValueOrEmpty(b[i].AppProtocol) {
			return false
		}
	}

	return true
}

func addressesEqual(a, b []v1.EndpointAddress) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].IP != b[i].IP || a[i].Hostname != b[i].Hostname ||
			ptrValueOrEmpty(a[i].NodeName) != ptrValueOrEmpty(b[i].NodeName) {
			return false
		}
	}

	return true
}

func ptrValueOrEmpty(ptr *string) string {
	if ptr != nil {
		return *ptr
	}
	return ""
}
