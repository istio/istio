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
	"istio.io/istio/pkg/config/schema/gvk"
)

// serviceImportCache provides import state for all services in the cluster.
type serviceImportCache interface {
	GetClusterSetIPs(name types.NamespacedName) []string
	HasSynced() bool
	ImportedServices() []model.ClusterServiceInfo
}

// newServiceImportCache creates a new cache of ServiceImport resources in the cluster.
func newServiceImportCache(c *Controller) serviceImportCache {
	if c.opts.EnableMCSServiceDiscovery {
		informer := c.client.MCSApisInformer().Multicluster().V1alpha1().ServiceImports().Informer()
		sic := &serviceImportCacheImpl{
			Controller: c,
			informer:   informer,
			lister:     mcsLister.NewServiceImportLister(informer.GetIndexer()),
		}

		// Register callbacks for events.
		c.registerHandlers(informer, "ServiceImports", sic.onEvent, nil)
		return sic
	}

	// MCS Service discovery is disabled. Use a placeholder cache.
	return disabledServiceImportCache{}
}

// serviceImportCacheImpl reads ServiceImport resources for a single cluster.
type serviceImportCacheImpl struct {
	*Controller
	informer cache.SharedIndexInformer
	lister   mcsLister.ServiceImportLister
}

func (ic *serviceImportCacheImpl) onEvent(obj interface{}, e model.Event) error {
	si, ok := obj.(*mcsCore.ServiceImport)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %#v", obj)
		}
		si, ok = tombstone.Obj.(*mcsCore.ServiceImport)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a ServiceImport %#v", obj)
		}
	}

	// Update the cached service, if it exists.
	ic.updateService(si, e)

	// Trigger an XDS update.
	ic.updateXDS(si)
	return nil
}

func (ic *serviceImportCacheImpl) updateService(si *mcsCore.ServiceImport, e model.Event) {
	// Extract the new IPs for the ClusterSet.
	var ips []string
	switch e {
	case model.EventAdd, model.EventUpdate:
		if si.Spec.Type == mcsCore.ClusterSetIP {
			ips = si.Spec.IPs
		}
	}

	// Update the cached service object, if it exists.
	if svc, _ := ic.GetService(kubesr.ServiceHostnameForKR(si, ic.opts.DomainSuffix)); svc != nil {
		svc.ClusterSetLocal.ClusterVIPs.SetAddressesFor(ic.Cluster(), ips)
	}
}

func (ic *serviceImportCacheImpl) updateXDS(si *mcsCore.ServiceImport) {
	hostname := kubesr.ServiceHostnameForKR(si, ic.opts.DomainSuffix)
	pushReq := &model.PushRequest{
		Full: true,
		ConfigsUpdated: map[model.ConfigKey]struct{}{{
			Kind:      gvk.ServiceEntry,
			Name:      string(hostname),
			Namespace: si.Namespace,
		}: {}},
		Reason: []model.TriggerReason{model.ServiceUpdate},
	}
	ic.opts.XDSUpdater.ConfigUpdate(pushReq)
}

func (ic *serviceImportCacheImpl) GetClusterSetIPs(name types.NamespacedName) []string {
	if si, _ := ic.lister.ServiceImports(name.Namespace).Get(name.Name); si != nil {
		return si.Spec.IPs
	}
	return nil
}

func (ic *serviceImportCacheImpl) ImportedServices() []model.ClusterServiceInfo {
	objs, err := ic.lister.List(klabels.NewSelector())
	if err != nil {
		return make([]model.ClusterServiceInfo, 0)
	}

	out := make([]model.ClusterServiceInfo, 0, len(objs))
	for _, obj := range objs {
		out = append(out, model.ClusterServiceInfo{
			Name:      obj.Name,
			Namespace: obj.Namespace,
			Cluster:   ic.Cluster(),
		})
	}

	return out
}

func (ic *serviceImportCacheImpl) HasSynced() bool {
	return ic.informer.HasSynced()
}

type disabledServiceImportCache struct{}

var _ serviceImportCache = disabledServiceImportCache{}

func (c disabledServiceImportCache) GetClusterSetIPs(types.NamespacedName) []string {
	return nil
}

func (c disabledServiceImportCache) HasSynced() bool {
	return true
}

func (c disabledServiceImportCache) ImportedServices() []model.ClusterServiceInfo {
	// MCS is disabled - returning `nil`, which is semantically different here than an empty list.
	return nil
}
