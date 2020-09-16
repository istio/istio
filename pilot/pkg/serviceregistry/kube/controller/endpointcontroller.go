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
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/log"
)

// Pilot can get EDS information from Kubernetes from two mutually exclusive sources, Endpoints and
// EndpointSlices. The kubeEndpointsController abstracts these details and provides a common interface
// that both sources implement.
type kubeEndpointsController interface {
	HasSynced() bool
	Run(stopCh <-chan struct{})
	getInformer() cache.SharedIndexInformer
	onEvent(curr interface{}, event model.Event) error
	InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
		labelsList labels.Collection) ([]*model.ServiceInstance, error)
	GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance
	buildIstioEndpoints(ep interface{}, host host.Name) []*model.IstioEndpoint
	buildIstioEndpointsWithService(name, namespace string, host host.Name) []*model.IstioEndpoint
	// forgetEndpoint does internal bookkeeping on a deleted endpoint
	forgetEndpoint(endpoint interface{})
	getServiceInfo(ep interface{}) (host.Name, string, string)
}

// kubeEndpoints abstracts the common behavior across endpoint and endpoint slices.
type kubeEndpoints struct {
	c        *Controller
	informer cache.SharedIndexInformer
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
			// if the service is headless service, trigger a full push.
			if svc.Spec.ClusterIP == v1.ClusterIPNone {
				c.xdsUpdater.ConfigUpdate(&model.PushRequest{
					Full: true,
					// TODO: extend and set service instance type, so no need to re-init push context
					ConfigsUpdated: map[model.ConfigKey]struct{}{{
						Kind:      gvk.ServiceEntry,
						Name:      string(kube.ServiceHostname(svc.Name, svc.Namespace, c.domainSuffix)),
						Namespace: svc.Namespace,
					}: {}},
					Reason: []model.TriggerReason{model.EndpointUpdate},
				})
				return nil
			}
		}
	}
	return nil
}

func updateEDS(c *Controller, epc kubeEndpointsController, ep interface{}, event model.Event) {
	host, svcName, ns := epc.getServiceInfo(ep)
	c.RLock()
	svc := c.servicesMap[host]
	c.RUnlock()

	log.Debugf("Handle EDS endpoint %s in namespace %s", svcName, ns)
	var endpoints []*model.IstioEndpoint
	if event == model.EventDelete {
		epc.forgetEndpoint(ep)
	} else {
		endpoints = epc.buildIstioEndpoints(ep, host)
	}

	if features.EnableK8SServiceSelectWorkloadEntries && svc != nil {
		fep := c.collectWorkloadInstanceEndpoints(svc)
		endpoints = append(endpoints, fep...)
	}
	_ = c.xdsUpdater.EDSUpdate(c.clusterID, string(host), ns, endpoints)
}

// getPod fetches a pod by IP address.
// A pod may be missing (nil) for two reasons:
// * It is an endpoint without an associated Pod. In this case, expectPod will be false.
// * It is an endpoint with an associate Pod, but its not found. In this case, expectPod will be true.
//   this may happen due to eventually consistency issues, out of order events, etc. In this case, the caller
//   should not precede with the endpoint, or inaccurate information would be sent which may have impacts on
//   correctness and security.
func getPod(c *Controller, ip string, ep *metav1.ObjectMeta, targetRef *v1.ObjectReference, host host.Name) (rpod *v1.Pod, expectPod bool) {
	pod := c.pods.getPodByIP(ip)
	if pod != nil {
		return pod, false
	}
	// This means, the endpoint event has arrived before pod event.
	// This might happen because PodCache is eventually consistent.
	if targetRef != nil && targetRef.Kind == "Pod" {
		key := kube.KeyFunc(targetRef.Name, targetRef.Namespace)
		// There is a small chance getInformer may have the pod, but it hasn't
		// made its way to the PodCache yet as it a shared queue.
		podFromInformer, f, err := c.pods.informer.GetStore().GetByKey(key)
		if err != nil || !f {
			log.Debugf("Endpoint without pod %s %s.%s error: %v", ip, ep.Name, ep.Namespace, err)
			endpointsWithNoPods.Increment()
			if c.metrics != nil {
				c.metrics.AddMetric(model.EndpointNoPod, string(host), nil, ip)
			}
			// Tell pod cache we want to queue the endpoint event when this pod arrives.
			epkey := kube.KeyFunc(ep.Name, ep.Namespace)
			c.pods.queueEndpointEventOnPodArrival(epkey, ip)
			return nil, true
		}
		pod = podFromInformer.(*v1.Pod)
	}
	return pod, false
}
