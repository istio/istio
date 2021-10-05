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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	mcsCore "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	mcsLister "sigs.k8s.io/mcs-api/pkg/client/listers/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	kubesr "istio.io/istio/pilot/pkg/serviceregistry/kube"
)

type exportedService struct {
	namespacedName                types.NamespacedName
	endpointDiscoverabilityPolicy string
}

// serviceExportCache reads Kubernetes Multi-Cluster Services (MCS) ServiceExport resources in the
// cluster and generates discoverability policies for the endpoints.
type serviceExportCache interface {
	// EndpointDiscoverabilityPolicy returns the policy for Service endpoints residing within the current cluster.
	EndpointDiscoverabilityPolicy(svc *model.Service) model.EndpointDiscoverabilityPolicy

	// ExportedServices returns the list of services that are exported in this cluster. Used for debugging.
	ExportedServices() []exportedService

	// HasSynced indicates whether the kube client has synced for the watched resources.
	HasSynced() bool
}

// newServiceExportCache creates a new serviceExportCache that observes the given cluster.
func newServiceExportCache(c *Controller) serviceExportCache {
	if features.EnableMCSServiceDiscovery {
		informer := c.client.MCSApisInformer().Multicluster().V1alpha1().ServiceExports().Informer()
		sec := &serviceExportCacheImpl{
			Controller: c,
			informer:   informer,
			lister:     mcsLister.NewServiceExportLister(informer.GetIndexer()),
		}

		// Register callbacks for events.
		c.registerHandlers(informer, "ServiceExports", sec.onServiceExportEvent, nil)
		return sec
	}

	// MCS Service discovery is disabled. Use a placeholder cache.
	return disabledServiceExportCache{}
}

// serviceExportCache reads ServiceExport resources for a single cluster.
type serviceExportCacheImpl struct {
	*Controller
	informer cache.SharedIndexInformer
	lister   mcsLister.ServiceExportLister
}

func (ec *serviceExportCacheImpl) onServiceExportEvent(obj interface{}, event model.Event) error {
	se, ok := obj.(*mcsCore.ServiceExport)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %#v", obj)
		}
		se, ok = tombstone.Obj.(*mcsCore.ServiceExport)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a ServiceExport %#v", obj)
		}
	}

	switch event {
	case model.EventAdd, model.EventDelete:
		ec.updateXDS(se)
	default:
		// Don't care about updates.
	}
	return nil
}

func (ec *serviceExportCacheImpl) updateXDS(se metav1.Object) {
	for _, svc := range ec.servicesForNamespacedName(kubesr.NamespacedNameForK8sObject(se)) {
		// Re-build the endpoints for this service with a new discoverability policy.
		// Also update any internal caching.
		endpoints := ec.buildEndpointsForService(svc, true)
		shard := model.ShardKeyFromRegistry(ec)
		ec.opts.XDSUpdater.EDSUpdate(shard, svc.Hostname.String(), se.GetNamespace(), endpoints)
	}
}

func (ec *serviceExportCacheImpl) EndpointDiscoverabilityPolicy(svc *model.Service) (policy model.EndpointDiscoverabilityPolicy) {
	if svc == nil {
		// Default policy when the service doesn't exist.
		return model.DiscoverableFromSameCluster
	}

	// TODO(nmittler): Once we can configure cluster.local to actually be cluster.local, consider the hostname.

	// MCS hosts (clusterset.local) and exported services are discoverable from anywhere in the mesh.
	if ec.isExported(namespacedNameForService(svc)) {
		return model.AlwaysDiscoverable
	}

	// Otherwise, endpoints are only discoverable from within the same cluster.
	return model.DiscoverableFromSameCluster
}

func (ec *serviceExportCacheImpl) isExported(name types.NamespacedName) bool {
	_, err := ec.lister.ServiceExports(name.Namespace).Get(name.Name)
	return err == nil
}

func (ec *serviceExportCacheImpl) ExportedServices() []exportedService {
	// List all exports in this cluster.
	exports, err := ec.lister.List(klabels.Everything())
	if err != nil {
		return make([]exportedService, 0)
	}

	ec.RLock()

	out := make([]exportedService, 0, len(exports))
	for _, export := range exports {
		svc := ec.servicesMap[kubesr.ServiceHostname(export.Name, export.Namespace, ec.opts.DomainSuffix)]
		out = append(out, exportedService{
			namespacedName:                kubesr.NamespacedNameForK8sObject(export),
			endpointDiscoverabilityPolicy: ec.EndpointDiscoverabilityPolicy(svc).String(),
		})
	}

	ec.RUnlock()

	return out
}

func (ec *serviceExportCacheImpl) HasSynced() bool {
	return ec.informer.HasSynced()
}

type disabledServiceExportCache struct{}

var _ serviceExportCache = disabledServiceExportCache{}

func (c disabledServiceExportCache) EndpointDiscoverabilityPolicy(*model.Service) model.EndpointDiscoverabilityPolicy {
	return model.AlwaysDiscoverable
}

func (c disabledServiceExportCache) HasSynced() bool {
	return true
}

func (c disabledServiceExportCache) ExportedServices() []exportedService {
	// MCS is disabled - returning `nil`, which is semantically different here than an empty list.
	return nil
}
