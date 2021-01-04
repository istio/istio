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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/listers/discovery/v1beta1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// Handle discovery namespace membership changes, which entails triggering create/delete event handlers for services, pods, and endpoints,
// and adding/removing discovery namespaces from the DiscoveryNamespacesFilter.
func (c *Controller) initDiscoveryNamespaceHandlers(
	kubeClient kubelib.Client,
	endpointMode EndpointMode,
	discoveryNamespacesFilter filter.DiscoveryNamespacesFilter,
) {
	otype := "Namespaces"
	c.nsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			incrementEvent(otype, "add")
			ns := obj.(*v1.Namespace)
			// handle creation of labeled namespace
			if ns.Labels[filter.PilotDiscoveryLabelName] == filter.PilotDiscoveryLabelValue {
				discoveryNamespacesFilter.AddNamespace(ns.Name)
				c.queue.Push(func() error {
					c.handleLabeledNamespace(endpointMode, ns.Name)
					return nil
				})
			}
		},
		UpdateFunc: func(old, new interface{}) {
			incrementEvent(otype, "update")
			oldNs := old.(*v1.Namespace)
			newNs := new.(*v1.Namespace)
			if oldNs.Labels[filter.PilotDiscoveryLabelName] != newNs.Labels[filter.PilotDiscoveryLabelName] {
				var handleFunc func() error
				if oldNs.Labels[filter.PilotDiscoveryLabelName] == filter.PilotDiscoveryLabelValue {
					// namespace was delabeled, issue deletes for all services, pods, and endpoints in namespace
					discoveryNamespacesFilter.RemoveNamespace(newNs.Name)
					handleFunc = func() error {
						c.handleDelabledNamespace(kubeClient, endpointMode, newNs.Name)
						return nil
					}
				} else {
					// namespace was newly labeled, issue creates for all services, pods, and endpoints in namespace
					discoveryNamespacesFilter.AddNamespace(newNs.Name)
					handleFunc = func() error {
						c.handleLabeledNamespace(endpointMode, newNs.Name)
						return nil
					}
				}
				c.queue.Push(handleFunc)
			}
		},
		DeleteFunc: func(obj interface{}) {
			incrementEvent(otype, "delete")
			ns := obj.(*v1.Namespace)
			if ns.Labels[filter.PilotDiscoveryLabelName] == filter.PilotDiscoveryLabelValue {
				discoveryNamespacesFilter.RemoveNamespace(ns.Name)
			}
			// no need to invoke object handlers since objects within the namespace will trigger delete events
		},
	})
}

// issue create events for all services, pods, and endpoints in the newly labeled namespace
func (c *Controller) handleLabeledNamespace(endpointMode EndpointMode, ns string) {
	var errs *multierror.Error

	// for each resource type, issue create events for objects in the labeled namespace

	services, err := c.serviceLister.Services(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing services: %v", err)
		return
	}
	for _, svc := range services {
		errs = multierror.Append(errs, c.onServiceEvent(svc, model.EventAdd))
	}

	pods, err := listerv1.NewPodLister(c.pods.informer.GetIndexer()).Pods(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing pods: %v", err)
		return
	}
	for _, pod := range pods {
		errs = multierror.Append(errs, c.pods.onEvent(pod, model.EventAdd))
	}

	switch endpointMode {
	case EndpointsOnly:
		endpoints, err := listerv1.NewEndpointsLister(c.endpoints.getInformer().GetIndexer()).Endpoints(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoints: %v", err)
			return
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventAdd))
		}
	case EndpointSliceOnly:
		endpointSlices, err := v1beta1.NewEndpointSliceLister(c.endpoints.getInformer().GetIndexer()).EndpointSlices(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoint slices: %v", err)
			return
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventAdd))
		}
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling newly labeled discovery namespace %s: %v", ns, err)
	}
}

// issue delete events for all services, pods, and endpoints in the delabled namespace
// use kubeClient to bypass filter in order to list resources from non-labeled namespace
func (c *Controller) handleDelabledNamespace(kubeClient kubelib.Client, endpointMode EndpointMode, ns string) {
	var errs *multierror.Error

	// for each resource type, issue delete events for objects in the delabled namespace

	services, err := kubeClient.KubeInformer().Core().V1().Services().Lister().Services(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing services: %v", err)
		return
	}
	for _, svc := range services {
		errs = multierror.Append(errs, c.onServiceEvent(svc, model.EventDelete))
	}

	pods, err := kubeClient.KubeInformer().Core().V1().Pods().Lister().Pods(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing pods: %v", err)
		return
	}
	for _, pod := range pods {
		errs = multierror.Append(errs, c.pods.onEvent(pod, model.EventDelete))
	}

	switch endpointMode {
	case EndpointsOnly:
		endpoints, err := kubeClient.KubeInformer().Core().V1().Endpoints().Lister().Endpoints(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoints: %v", err)
			return
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventDelete))
		}
	case EndpointSliceOnly:
		endpointSlices, err := kubeClient.KubeInformer().Discovery().V1beta1().EndpointSlices().Lister().EndpointSlices(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoint slices: %v", err)
			return
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(ep, model.EventDelete))
		}
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling delabeled discovery namespace %s: %v", ns, err)
	}
}
