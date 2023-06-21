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
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	filter "istio.io/istio/pkg/kube/namespace"
)

// initialize handlers for discovery selection scoping
func (c *Controller) initDiscoveryHandlers(meshWatcher mesh.Watcher, discoveryNamespacesFilter filter.DiscoveryNamespacesFilter) {
	c.initDiscoveryNamespaceHandlers(discoveryNamespacesFilter)
	c.initMeshWatcherHandler(meshWatcher, discoveryNamespacesFilter)
}

// handle discovery namespace membership changes triggered by namespace events,
// which requires triggering create/delete event handlers for services, pods, and endpoints,
// and updating the DiscoveryNamespacesFilter.
func (c *Controller) initDiscoveryNamespaceHandlers(discoveryNamespacesFilter filter.DiscoveryNamespacesFilter) {
	discoveryNamespacesFilter.AddHandler(func(ns string, event model.Event) {
		switch event {
		case model.EventAdd:
			c.queue.Push(func() error {
				c.handleSelectedNamespace(ns)
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
		case model.EventDelete:
			c.queue.Push(func() error {
				c.handleDeselectedNamespace(ns)
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
	})
}

// handle discovery namespace membership changes triggered by changes to meshConfig's discovery selectors
// which requires updating the DiscoveryNamespaceFilter and triggering create/delete event handlers for services/pods/endpoints
// for membership changes
func (c *Controller) initMeshWatcherHandler(meshWatcher mesh.Watcher, discoveryNamespacesFilter filter.DiscoveryNamespacesFilter) {
	meshWatcher.AddMeshHandler(func() {
		discoveryNamespacesFilter.SelectorsChanged(meshWatcher.Mesh().GetDiscoverySelectors())
	})
}

// issue create events for all services, pods, and endpoints in the newly labeled namespace
func (c *Controller) handleSelectedNamespace(ns string) {
	var errs *multierror.Error

	// for each resource type, issue create events for objects in the labeled namespace
	for _, svc := range c.services.List(ns, labels.Everything()) {
		errs = multierror.Append(errs, c.onServiceEvent(nil, svc, model.EventAdd))
	}

	pods := c.podsClient.List(ns, labels.Everything())
	for _, pod := range pods {
		errs = multierror.Append(errs, c.pods.onEvent(nil, pod, model.EventAdd))
	}

	if c.ambientIndex != nil {
		c.ambientIndex.handlePods(pods, c)
		allWorkloadEntries := c.getControllerWorkloadEntries(ns)
		c.ambientIndex.handleWorkloadEntries(allWorkloadEntries, c)

	}

	errs = multierror.Append(errs, c.endpoints.sync("", ns, model.EventAdd, false))

	for _, handler := range c.namespaceDiscoveryHandlers {
		handler(ns, model.EventAdd)
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling newly labeled discovery namespace %s: %v", ns, err)
	}
}

// issue delete events for all services, pods, and endpoints in the deselected namespace
// use kubeClient.KubeInformer() to bypass filter in order to list resources from non-labeled namespace,
// which fetches informers from the SharedInformerFactory cache (i.e. does not instantiate a new informer)
func (c *Controller) handleDeselectedNamespace(ns string) {
	var errs *multierror.Error

	// for each resource type, issue delete events for objects in the deselected namespace
	for _, svc := range c.services.ListUnfiltered(ns, labels.Everything()) {
		errs = multierror.Append(errs, c.onServiceEvent(nil, svc, model.EventDelete))
	}

	for _, pod := range c.podsClient.ListUnfiltered(ns, labels.Everything()) {
		errs = multierror.Append(errs, c.pods.onEvent(nil, pod, model.EventDelete))
	}

	errs = multierror.Append(errs, c.endpoints.sync("", ns, model.EventDelete, false))

	for _, handler := range c.namespaceDiscoveryHandlers {
		handler(ns, model.EventDelete)
	}

	if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
		log.Errorf("one or more errors while handling deselected discovery namespace %s: %v", ns, err)
	}
}
