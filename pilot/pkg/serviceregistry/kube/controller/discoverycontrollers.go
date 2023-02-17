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

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	filter "istio.io/istio/pkg/kube/namespace"
)

// initialize handlers for discovery selection scoping
func (c *Controller) initDiscoveryHandlers(
	kubeClient kubelib.Client,
	endpointMode EndpointMode,
	meshWatcher mesh.Watcher,
	discoveryNamespacesFilter filter.DiscoveryNamespacesFilter,
) {
	c.initDiscoveryNamespaceHandlers(kubeClient, endpointMode, discoveryNamespacesFilter)
	c.initMeshWatcherHandler(kubeClient, endpointMode, meshWatcher, discoveryNamespacesFilter)
}

// handle discovery namespace membership changes triggered by namespace events,
// which requires triggering create/delete event handlers for services, pods, and endpoints,
// and updating the DiscoveryNamespacesFilter.
func (c *Controller) initDiscoveryNamespaceHandlers(
	kubeClient kubelib.Client,
	endpointMode EndpointMode,
	discoveryNamespacesFilter filter.DiscoveryNamespacesFilter,
) {
	otype := "Namespaces"
	_, _ = c.nsInformer.AddEventHandler(controllers.EventHandler[*v1.Namespace]{
		AddFunc: func(ns *v1.Namespace) {
			incrementEvent(otype, "add")
			if discoveryNamespacesFilter.NamespaceCreated(ns.ObjectMeta) {
				c.queue.Push(func() error {
					c.handleSelectedNamespace(endpointMode, ns.Name)
					// This is necessary because namespace handled by discoveryNamespacesFilter may take some time,
					// if a CR is processed before discoveryNamespacesFilter takes effect, it will be ignored.
					if features.EnableEnhancedResourceScoping {
						c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
							Full:   true,
							Reason: []model.TriggerReason{model.NamespaceUpdate},
						})
					}
					return nil
				})
			}
		},
		UpdateFunc: func(old, new *v1.Namespace) {
			incrementEvent(otype, "update")
			membershipChanged, namespaceAdded := discoveryNamespacesFilter.NamespaceUpdated(old.ObjectMeta, new.ObjectMeta)
			if membershipChanged {
				handleFunc := func() error {
					if namespaceAdded {
						c.handleSelectedNamespace(endpointMode, new.Name)
					} else {
						c.handleDeselectedNamespace(kubeClient, endpointMode, new.Name)
					}
					// This is necessary because namespace handled by discoveryNamespacesFilter may take some time,
					// if a CR is processed before discoveryNamespacesFilter takes effect, it will be ignored.
					if features.EnableEnhancedResourceScoping {
						c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
							Full:   true,
							Reason: []model.TriggerReason{model.NamespaceUpdate},
						})
					}
					return nil
				}
				c.queue.Push(handleFunc)
			}
		},
		DeleteFunc: func(ns *v1.Namespace) {
			incrementEvent(otype, "delete")
			discoveryNamespacesFilter.NamespaceDeleted(ns.ObjectMeta)
			// no need to invoke object handlers since objects within the namespace will trigger delete events
		},
	})
}

// handle discovery namespace membership changes triggered by changes to meshConfig's discovery selectors
// which requires updating the DiscoveryNamespaceFilter and triggering create/delete event handlers for services/pods/endpoints
// for membership changes
func (c *Controller) initMeshWatcherHandler(
	kubeClient kubelib.Client,
	endpointMode EndpointMode,
	meshWatcher mesh.Watcher,
	discoveryNamespacesFilter filter.DiscoveryNamespacesFilter,
) {
	meshWatcher.AddMeshHandler(func() {
		newSelectedNamespaces, deselectedNamespaces := discoveryNamespacesFilter.SelectorsChanged(meshWatcher.Mesh().GetDiscoverySelectors())

		for _, nsName := range newSelectedNamespaces {
			nsName := nsName // need to shadow variable to ensure correct value when evaluated inside the closure below
			c.queue.Push(func() error {
				c.handleSelectedNamespace(endpointMode, nsName)
				return nil
			})
		}

		for _, nsName := range deselectedNamespaces {
			nsName := nsName // need to shadow variable to ensure correct value when evaluated inside the closure below
			c.queue.Push(func() error {
				c.handleDeselectedNamespace(kubeClient, endpointMode, nsName)
				return nil
			})
		}

		if features.EnableEnhancedResourceScoping && (len(newSelectedNamespaces) > 0 || len(deselectedNamespaces) > 0) {
			c.queue.Push(func() error {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:   true,
					Reason: []model.TriggerReason{model.GlobalUpdate},
				})
				return nil
			})
		}
	})
}

// issue create events for all services, pods, and endpoints in the newly labeled namespace
func (c *Controller) handleSelectedNamespace(endpointMode EndpointMode, ns string) {
	var errs *multierror.Error

	// for each resource type, issue create events for objects in the labeled namespace

	services, err := c.serviceLister.Services(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing services: %v", err)
		return
	}
	for _, svc := range services {
		errs = multierror.Append(errs, c.onServiceEvent(nil, svc, model.EventAdd))
	}

	pods, err := listerv1.NewPodLister(c.pods.informer.GetIndexer()).Pods(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing pods: %v", err)
		return
	}
	for _, pod := range pods {
		errs = multierror.Append(errs, c.pods.onEvent(nil, pod, model.EventAdd))
	}
	if c.ambientIndex != nil {
		c.ambientIndex.handlePods(pods)
	}

	switch endpointMode {
	case EndpointsOnly:
		endpoints, err := listerv1.NewEndpointsLister(c.endpoints.getInformer().GetIndexer()).Endpoints(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoints: %v", err)
			return
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(nil, ep, model.EventAdd))
		}
	case EndpointSliceOnly:
		endpointSlices, err := c.endpoints.(*endpointSliceController).listSlices(ns, labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoint slices: %v", err)
			return
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(nil, ep, model.EventAdd))
		}
	}

	for _, handler := range c.namespaceDiscoveryHandlers {
		handler(ns, model.EventAdd)
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling newly labeled discovery namespace %s: %v", ns, err)
	}
}

// issue delete events for all services, pods, and endpoints in the delabled namespace
// use kubeClient.KubeInformer() to bypass filter in order to list resources from non-labeled namespace,
// which fetches informers from the SharedInformerFactory cache (i.e. does not instantiate a new informer)
func (c *Controller) handleDeselectedNamespace(kubeClient kubelib.Client, endpointMode EndpointMode, ns string) {
	var errs *multierror.Error

	// for each resource type, issue delete events for objects in the delabled namespace

	services, err := kubeClient.KubeInformer().Core().V1().Services().Lister().Services(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing services: %v", err)
		return
	}
	for _, svc := range services {
		errs = multierror.Append(errs, c.onServiceEvent(nil, svc, model.EventDelete))
	}

	pods, err := kubeClient.KubeInformer().Core().V1().Pods().Lister().Pods(ns).List(labels.Everything())
	if err != nil {
		log.Errorf("error listing pods: %v", err)
		return
	}
	for _, pod := range pods {
		errs = multierror.Append(errs, c.pods.onEvent(nil, pod, model.EventDelete))
	}

	switch endpointMode {
	case EndpointsOnly:
		endpoints, err := kubeClient.KubeInformer().Core().V1().Endpoints().Lister().Endpoints(ns).List(labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoints: %v", err)
			return
		}
		for _, ep := range endpoints {
			errs = multierror.Append(errs, c.endpoints.onEvent(nil, ep, model.EventDelete))
		}
	case EndpointSliceOnly:
		endpointSlices, err := c.endpoints.(*endpointSliceController).listSlices(ns, labels.Everything())
		if err != nil {
			log.Errorf("error listing endpoint slices: %v", err)
			return
		}
		for _, ep := range endpointSlices {
			errs = multierror.Append(errs, c.endpoints.onEvent(nil, ep, model.EventDelete))
		}
	}

	for _, handler := range c.namespaceDiscoveryHandlers {
		handler(ns, model.EventDelete)
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling delabeled discovery namespace %s: %v", ns, err)
	}
}
