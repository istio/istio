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

	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	mcsCore "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	mcsLister "sigs.k8s.io/mcs-api/pkg/client/listers/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/model"
	kubesr "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
)

// serviceExportCache provides export state for all services in the cluster.
type serviceExportCache interface {
	isExported(name types.NamespacedName) bool
	HasSynced() bool
	ExportedServices() []string
}

// newServiceExportCache creates a new serviceExportCache that observes the given cluster.
func newServiceExportCache(c *Controller) serviceExportCache {
	if c.opts.EnableMCSServiceDiscovery {
		informer := c.client.MCSApisInformer().Multicluster().V1alpha1().ServiceExports().Informer()
		sec := &serviceExportCacheImpl{
			Controller: c,
			informer:   informer,
			lister:     mcsLister.NewServiceExportLister(informer.GetIndexer()),
		}

		// Register callbacks for ServiceImport events.
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

func (ec *serviceExportCacheImpl) updateXDS(se *mcsCore.ServiceExport) {
	hostname := ec.getHostname(se)
	svc, err := ec.GetService(hostname)
	if err != nil {
		// The service doesn't exist - nothing to update.
		return
	}

	// Update the endpoint cache for this cluster and push an update.
	endpoints := ec.buildEndpointsForService(svc)
	if len(endpoints) > 0 {
		ec.opts.XDSUpdater.EDSUpdate(string(ec.Cluster()), string(hostname), se.Namespace, endpoints)
	}
}

func (ec *serviceExportCacheImpl) isExported(name types.NamespacedName) bool {
	_, err := ec.lister.ServiceExports(name.Namespace).Get(name.Name)
	return err == nil
}

func (ec *serviceExportCacheImpl) ExportedServices() []string {
	// List all exports in this cluster.
	exports, err := ec.lister.List(klabels.NewSelector())
	if err != nil {
		return make([]string, 0)
	}

	// Convert to ExportedService
	out := make([]string, 0, len(exports))
	for _, export := range exports {
		out = append(out, fmt.Sprintf("%s:%s/%s", ec.Cluster(), export.Namespace, export.Name))
	}

	return out
}

func (ec *serviceExportCacheImpl) HasSynced() bool {
	return ec.informer.HasSynced()
}

func (ec *serviceExportCacheImpl) getHostname(se *mcsCore.ServiceExport) host.Name {
	return kubesr.ServiceHostname(se.Name, se.Namespace, ec.opts.DomainSuffix)
}

type disabledServiceExportCache struct{}

var _ serviceExportCache = disabledServiceExportCache{}

func (c disabledServiceExportCache) isExported(types.NamespacedName) bool {
	// When disabled, assume all services are exported (default Istio behavior).
	return true
}

func (c disabledServiceExportCache) HasSynced() bool {
	return true
}

func (c disabledServiceExportCache) ExportedServices() []string {
	// MCS is disabled - returning `nil`, which is semantically different here than an empty list.
	return nil
}
