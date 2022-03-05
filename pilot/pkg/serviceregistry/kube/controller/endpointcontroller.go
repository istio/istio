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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
)

// Pilot can get EDS information from Kubernetes from two mutually exclusive sources, Endpoints and
// EndpointSlices. The kubeEndpointsController abstracts these details and provides a common interface
// that both sources implement.
type kubeEndpointsController interface {
	HasSynced() bool
	Run(stopCh <-chan struct{})
	getInformer() filter.FilteredSharedIndexInformer
	onEvent(curr interface{}, event model.Event) error
	InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int, labelsList labels.Collection) []*model.ServiceInstance
	GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance
	buildIstioEndpoints(ep interface{}, host host.Name) []*model.IstioEndpoint
	buildIstioEndpointsWithService(name, namespace string, host host.Name, clearCache bool) []*model.IstioEndpoint
	// forgetEndpoint does internal bookkeeping on a deleted endpoint
	forgetEndpoint(endpoint interface{}) map[host.Name][]*model.IstioEndpoint
	getServiceNamespacedName(ep interface{}) types.NamespacedName
}

// kubeEndpoints abstracts the common behavior across endpoint and endpoint slices.
type kubeEndpoints struct {
	c        *Controller
	informer filter.FilteredSharedIndexInformer
}

func (e *kubeEndpoints) HasSynced() bool {
	return e.informer.HasSynced()
}

func (e *kubeEndpoints) Run(stopCh <-chan struct{}) {
	e.informer.Run(stopCh)
}

// processEndpointEvent triggers the config update.
func processEndpointEvent(c *Controller, epc kubeEndpointsController, name string, namespace string, event model.Event, ep interface{}) error {
	// Update internal endpoint cache no matter what kind of service, even headless service.
	// As for gateways, the cluster discovery type is `EDS` for headless service.
	updateEDS(c, epc, ep, event)
	if features.EnableHeadlessService {
		if svc, _ := c.serviceLister.Services(namespace).Get(name); svc != nil {
			for _, modelSvc := range c.servicesForNamespacedName(kube.NamespacedNameForK8sObject(svc)) {
				// if the service is headless service, trigger a full push.
				if svc.Spec.ClusterIP == v1.ClusterIPNone {
					c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
						Full: true,
						// TODO: extend and set service instance type, so no need to re-init push context
						ConfigsUpdated: map[model.ConfigKey]struct{}{{
							Kind:      gvk.ServiceEntry,
							Name:      modelSvc.Hostname.String(),
							Namespace: svc.Namespace,
						}: {}},
						Reason: []model.TriggerReason{model.EndpointUpdate},
					})
					return nil
				}
			}
		}
	}

	return nil
}

func updateEDS(c *Controller, epc kubeEndpointsController, ep interface{}, event model.Event) {
	namespacedName := epc.getServiceNamespacedName(ep)
	log.Debugf("Handle EDS endpoint %s %s %s in namespace %s", namespacedName.Name, event, namespacedName.Namespace)
	var forgottenEndpointsByHost map[host.Name][]*model.IstioEndpoint
	if event == model.EventDelete {
		forgottenEndpointsByHost = epc.forgetEndpoint(ep)
	}

	shard := model.ShardKeyFromRegistry(c)

	for _, hostName := range c.hostNamesForNamespacedName(namespacedName) {
		var endpoints []*model.IstioEndpoint
		if forgottenEndpointsByHost != nil {
			endpoints = forgottenEndpointsByHost[hostName]
		} else {
			endpoints = epc.buildIstioEndpoints(ep, hostName)
		}

		svc := c.GetService(hostName)
		if svc != nil {
			fep := c.collectWorkloadInstanceEndpoints(svc)
			endpoints = append(endpoints, fep...)
		} else {
			log.Debugf("Handle EDS endpoint: skip collecting workload entry endpoints, service %s/%s has not been populated",
				namespacedName.Namespace, namespacedName.Name)
		}

		c.opts.XDSUpdater.EDSUpdate(shard, string(hostName), namespacedName.Namespace, endpoints)
	}
}

// getPod fetches a pod by name or IP address.
// A pod may be missing (nil) for two reasons:
// * It is an endpoint without an associated Pod. In this case, expectPod will be false.
// * It is an endpoint with an associate Pod, but its not found. In this case, expectPod will be true.
//   this may happen due to eventually consistency issues, out of order events, etc. In this case, the caller
//   should not precede with the endpoint, or inaccurate information would be sent which may have impacts on
//   correctness and security.
// Note: this is only used by endpoints and endpointslice controller
func getPod(c *Controller, ip string, ep *metav1.ObjectMeta, targetRef *v1.ObjectReference, host host.Name) (*v1.Pod, bool) {
	var expectPod bool
	pod := c.getPod(ip, ep.Namespace, targetRef)
	if targetRef != nil && targetRef.Kind == "Pod" {
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
	epkey := kube.KeyFunc(ep.Name, ep.Namespace)
	c.pods.queueEndpointEventOnPodArrival(epkey, ip)
}

// getPod fetches a pod by name or IP address.
// A pod may be missing (nil) for two reasons:
// * It is an endpoint without an associated Pod.
// * It is an endpoint with an associate Pod, but its not found.
func (c *Controller) getPod(ip string, namespace string, targetRef *v1.ObjectReference) *v1.Pod {
	if targetRef != nil && targetRef.Kind == "Pod" {
		key := kube.KeyFunc(targetRef.Name, targetRef.Namespace)
		pod := c.pods.getPodByKey(key)
		return pod
	}

	// This means the endpoint is manually controlled
	// TODO: this may be not correct because of the hostnetwork pods may have same ip address
	// Do we have a way to get the pod from only endpoint?
	pod := c.pods.getPodByIP(ip)
	if pod != nil {
		// This prevents selecting a pod in another different namespace
		if pod.Namespace != namespace {
			pod = nil
		}
	}
	// There maybe no pod at all
	return pod
}
