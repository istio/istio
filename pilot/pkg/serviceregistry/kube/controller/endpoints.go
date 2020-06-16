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
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/listwatch"
)

type endpointsController struct {
	kubeEndpoints
}

var _ kubeEndpointsController = &endpointsController{}

func newEndpointsController(c *Controller, options Options) *endpointsController {
	namespaces := strings.Split(options.WatchedNamespaces, ",")

	mlw := listwatch.MultiNamespaceListerWatcher(namespaces, func(namespace string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Endpoints(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Endpoints(namespace).Watch(context.TODO(), opts)
			},
		}
	})

	informer := cache.NewSharedIndexInformer(mlw, &v1.Endpoints{}, options.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	out := &endpointsController{
		kubeEndpoints: kubeEndpoints{
			c:        c,
			informer: informer,
		},
	}
	registerHandlers(informer, c.queue, "Endpoints", endpointsEqual, out.onEvent)
	return out
}

func (e *endpointsController) GetProxyServiceInstances(c *Controller, proxy *model.Proxy) []*model.ServiceInstance {
	eps, err := listerv1.NewEndpointsLister(e.informer.GetIndexer()).Endpoints(proxy.Metadata.Namespace).List(klabels.Everything())
	if err != nil {
		log.Errorf("Get endpoints by index failed: %v", err)
		return nil
	}
	out := make([]*model.ServiceInstance, 0)
	for _, ep := range eps {
		instances := endpointServiceInstances(c, ep, proxy)
		out = append(out, instances...)
	}

	return out
}

func endpointServiceInstances(c *Controller, endpoints *v1.Endpoints, proxy *model.Proxy) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	hostname := kube.ServiceHostname(endpoints.Name, endpoints.Namespace, c.domainSuffix)
	c.RLock()
	svc := c.servicesMap[hostname]
	c.RUnlock()

	if svc != nil {
		podIP := proxy.IPAddresses[0]
		pod := c.pods.getPodByIP(podIP)
		builder := NewEndpointBuilder(c, pod)

		for _, ss := range endpoints.Subsets {
			for _, port := range ss.Ports {
				svcPort, exists := svc.Ports.Get(port.Name)
				if !exists {
					continue
				}

				// consider multiple IP scenarios
				for _, ip := range proxy.IPAddresses {
					if hasProxyIP(ss.Addresses, ip) || hasProxyIP(ss.NotReadyAddresses, ip) {
						istioEndpoint := builder.buildIstioEndpoint(ip, port.Port, svcPort.Name)
						out = append(out, &model.ServiceInstance{
							Endpoint:    istioEndpoint,
							ServicePort: svcPort,
							Service:     svc,
						})
					}

					if hasProxyIP(ss.NotReadyAddresses, ip) {
						if c.metrics != nil {
							c.metrics.AddMetric(model.ProxyStatusEndpointNotReady, proxy.ID, proxy, "")
						}
					}
				}
			}
		}
	}

	return out
}

func (e *endpointsController) InstancesByPort(c *Controller, svc *model.Service, reqSvcPort int,
	labelsList labels.Collection) ([]*model.ServiceInstance, error) {
	item, exists, err := e.informer.GetStore().GetByKey(kube.KeyFunc(svc.Attributes.Name, svc.Attributes.Namespace))
	if err != nil {
		log.Infof("get endpoints(%s, %s) => error %v", svc.Attributes.Name, svc.Attributes.Namespace, err)
		return nil, nil
	}
	if !exists {
		return nil, nil
	}

	// Locate all ports in the actual service
	svcPort, exists := svc.Ports.GetByPort(reqSvcPort)
	if !exists {
		return nil, nil
	}
	ep := item.(*v1.Endpoints)
	var out []*model.ServiceInstance
	for _, ss := range ep.Subsets {
		for _, ea := range ss.Addresses {
			var podLabels labels.Instance
			pod := c.pods.getPodByIP(ea.IP)
			if pod != nil {
				podLabels = pod.Labels
			}

			// check that one of the input labels is a subset of the labels
			if !labelsList.HasSubsetOf(podLabels) {
				continue
			}

			builder := NewEndpointBuilder(c, pod)

			// identify the port by name. K8S EndpointPort uses the service port name
			for _, port := range ss.Ports {
				if port.Name == "" || // 'name optional if single port is defined'
					svcPort.Name == port.Name {
					istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, svcPort.Name)
					out = append(out, &model.ServiceInstance{
						Endpoint:    istioEndpoint,
						ServicePort: svcPort,
						Service:     svc,
					})
				}
			}
		}
	}

	return out, nil
}

func (e *endpointsController) onEvent(curr interface{}, event model.Event) error {
	if err := e.c.checkReadyForEvents(); err != nil {
		return err
	}

	ep, ok := curr.(*v1.Endpoints)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("Couldn't get object from tombstone %#v", curr)
			return nil
		}
		ep, ok = tombstone.Obj.(*v1.Endpoints)
		if !ok {
			log.Errorf("Tombstone contained object that is not an endpoints %#v", curr)
			return nil
		}
	}

	return processEndpointEvent(e.c, e, ep.Name, ep.Namespace, event, curr)
}

func (e *endpointsController) buildIstioEndpoints(endpoint interface{}, host host.Name) []*model.IstioEndpoint {
	endpoints := make([]*model.IstioEndpoint, 0)
	ep := endpoint.(*v1.Endpoints)
	for _, ss := range ep.Subsets {
		for _, ea := range ss.Addresses {
			pod := e.c.pods.getPodByIP(ea.IP)
			if pod == nil {
				// This means, the endpoint event has arrived before pod event. This might happen because
				// PodCache is eventually consistent. We should try to get the pod from kube-api server.
				if ea.TargetRef != nil && ea.TargetRef.Kind == "Pod" {
					pod = e.c.pods.getPod(ea.TargetRef.Name, ea.TargetRef.Namespace)
					if pod == nil {
						// If pod is still not available, this an unusual case.
						endpointsWithNoPods.Increment()
						log.Errorf("Endpoint without pod %s %s.%s", ea.IP, ep.Name, ep.Namespace)
						if e.c.metrics != nil {
							e.c.metrics.AddMetric(model.EndpointNoPod, string(host), nil, ea.IP)
						}
						continue
					}
				}
			}

			builder := NewEndpointBuilder(e.c, pod)

			// EDS and ServiceEntry use name for service port - ADS will need to
			// map to numbers.
			for _, port := range ss.Ports {
				istioEndpoint := builder.buildIstioEndpoint(ea.IP, port.Port, port.Name)
				endpoints = append(endpoints, istioEndpoint)
			}
		}
	}
	return endpoints
}

func (e *endpointsController) getServiceInfo(ep interface{}) (host.Name, string, string) {
	endpoint := ep.(*v1.Endpoints)
	return kube.ServiceHostname(endpoint.Name, endpoint.Namespace, e.c.domainSuffix), endpoint.Name, endpoint.Namespace
}

// endpointsEqual returns true if the two endpoints are the same in aspects Pilot cares about
// This currently means only looking at "Ready" endpoints
func endpointsEqual(first, second interface{}) bool {
	a := first.(*v1.Endpoints)
	b := second.(*v1.Endpoints)
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
