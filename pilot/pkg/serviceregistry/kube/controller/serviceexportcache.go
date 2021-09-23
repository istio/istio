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

// serviceExportCache reads Kubernetes Multi-Cluster Services (MCS) ServiceExport resources in the
// cluster and generates discoverability policies for the endpoints.
type serviceExportCache interface {
	// EndpointDiscoverabilityPolicy returns the policy for Service endpoints residing within the current cluster.
	EndpointDiscoverabilityPolicy(svc *model.Service) model.EndpointDiscoverabilityPolicy

	// ExportedServices returns the list of services that are exported in this cluster. Used for debugging.
	ExportedServices() []model.ClusterServiceInfo

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
		c.registerHandlers(informer, "ServiceExports", sec.onEvent, nil)
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

func (ec *serviceExportCacheImpl) onEvent(obj interface{}, event model.Event) error {
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
	hostname := kubesr.ServiceHostnameForKR(se, ec.opts.DomainSuffix)
	svc := ec.GetService(hostname)
	if svc == nil {
		// The service doesn't exist - nothing to update.
		return
	}

	// Update the endpoint cache for this cluster and push an update.
	endpoints := ec.buildEndpointsForService(svc)
	if len(endpoints) > 0 {
		shard := model.ShardKeyFromRegistry(ec)
		ec.opts.XDSUpdater.EDSUpdate(shard, string(hostname), se.GetNamespace(), endpoints)
	}
}

func (ec *serviceExportCacheImpl) EndpointDiscoverabilityPolicy(svc *model.Service) model.EndpointDiscoverabilityPolicy {
	if svc == nil || !ec.isExported(namespacedNameForService(svc)) {
		return model.DiscoverableFromSameCluster
	}

	// TODO(nmittler): Once we can configure cluster.local to actually be cluster.local, consider the hostname.
	return model.AlwaysDiscoverable
}

func (ec *serviceExportCacheImpl) isExported(name types.NamespacedName) bool {
	_, err := ec.lister.ServiceExports(name.Namespace).Get(name.Name)
	return err == nil
}

func (ec *serviceExportCacheImpl) ExportedServices() []model.ClusterServiceInfo {
	// List all exports in this cluster.
	exports, err := ec.lister.List(klabels.NewSelector())
	if err != nil {
		return make([]model.ClusterServiceInfo, 0)
	}

	// Convert to ExportedService
	out := make([]model.ClusterServiceInfo, 0, len(exports))
	for _, export := range exports {
		out = append(out, model.ClusterServiceInfo{
			Name:      export.Name,
			Namespace: export.Namespace,
			Cluster:   ec.Cluster(),
		})
	}

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

func (c disabledServiceExportCache) ExportedServices() []model.ClusterServiceInfo {
	// MCS is disabled - returning `nil`, which is semantically different here than an empty list.
	return nil
}
