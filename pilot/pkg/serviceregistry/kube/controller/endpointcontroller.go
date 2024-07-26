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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
)

// Pilot can get EDS information from Kubernetes from two mutually exclusive sources, Endpoints and
// EndpointSlices. The kubeEndpointsController abstracts these details and provides a common interface
// that both sources implement.
type kubeEndpointsController interface {
	HasSynced() bool
	sync(name, ns string, event model.Event, filtered bool) error
	InstancesByPort(svc *model.Service, reqSvcPort int) []*model.ServiceInstance
	GetProxyServiceInstances(proxy *model.Proxy) []*model.ServiceInstance
	buildIstioEndpointsWithService(name, namespace string, host host.Name, clearCache bool) []*model.IstioEndpoint
}

// getPod fetches a pod by name or IP address.
// A pod may be missing (nil) for two reasons:
//   - It is an endpoint without an associated Pod. In this case, expectPod will be false.
//   - It is an endpoint with an associate Pod, but its not found. In this case, expectPod will be true.
//     this may happen due to eventually consistency issues, out of order events, etc. In this case, the caller
//     should not precede with the endpoint, or inaccurate information would be sent which may have impacts on
//     correctness and security.
//
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
	c.pods.queueEndpointEventOnPodArrival(config.NamespacedName(ep), ip)
}

// getPod fetches a pod by name or IP address.
// A pod may be missing (nil) for two reasons:
// * It is an endpoint without an associated Pod.
// * It is an endpoint with an associate Pod, but its not found.
func (c *Controller) getPod(ip string, namespace string, targetRef *v1.ObjectReference) *v1.Pod {
	if targetRef != nil && targetRef.Kind == "Pod" {
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
