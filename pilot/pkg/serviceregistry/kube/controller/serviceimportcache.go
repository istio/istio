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
	"strings"

	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	mcsLister "sigs.k8s.io/mcs-api/pkg/client/listers/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	mcsDomainSuffix = "." + constants.DefaultClusterSetLocalDomain
)

type importedService struct {
	namespacedName types.NamespacedName
	clusterSetVIP  string
}

// serviceImportCache reads and processes Kubernetes Multi-Cluster Services (MCS) ServiceImport
// resources.
//
// An MCS controller is responsible for reading ServiceExport resources in one cluster and generating
// ServiceImport in all clusters of the ClusterSet (i.e. mesh). While the serviceExportCache reads
// ServiceExport to control the discoverability policy for individual endpoints, this controller
// reads ServiceImport in the cluster in order to extract the ClusterSet VIP and generate a
// synthetic service for the MCS host (i.e. clusterset.local). The aggregate.Controller will then
// merge together the MCS services from all the clusters, filling out the full map of Cluster IPs.
//
// The synthetic MCS service is a copy of the real k8s Service (e.g. cluster.local) with the same
// namespaced name, but with the hostname and VIPs changed to the appropriate ClusterSet values.
// The real k8s Service can live anywhere in the mesh and does not have to reside in the same
// cluster as the ServiceImport.
type serviceImportCache interface {
	GetClusterSetIPs(name types.NamespacedName) []string
	HasSynced() bool
	ImportedServices() []importedService
}

// newServiceImportCache creates a new cache of ServiceImport resources in the cluster.
func newServiceImportCache(c *Controller) serviceImportCache {
	if features.EnableMCSHost {
		informer := c.client.MCSApisInformer().Multicluster().V1alpha1().ServiceImports().Informer()
		sic := &serviceImportCacheImpl{
			Controller: c,
			informer:   informer,
			lister:     mcsLister.NewServiceImportLister(informer.GetIndexer()),
		}

		// Register callbacks for Service events anywhere in the mesh.
		c.opts.MeshServiceController.AppendServiceHandler(sic.onServiceEvent)

		// Register callbacks for ServiceImport events in this cluster only.
		c.registerHandlers(informer, "ServiceImports", sic.onServiceImportEvent, nil)
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

// onServiceEvent is called when the controller receives an event for the kube Service (i.e. cluster.local).
// When this happens, we need to update the state of the associated synthetic MCS service.
func (ic *serviceImportCacheImpl) onServiceEvent(svc *model.Service, event model.Event) {
	if strings.HasSuffix(svc.Hostname.String(), mcsDomainSuffix) {
		// Ignore events for MCS services that were triggered by this controller.
		return
	}

	namespacedName := namespacedNameForService(svc)

	// Lookup the previous MCS service if there was one.
	mcsHost := serviceClusterSetLocalHostname(namespacedName)
	prevMcsService := ic.GetService(mcsHost)

	// Get the ClusterSet VIPs for this service in this cluster. Will only be populated if the
	// service has a ServiceImport in this cluster.
	vips := ic.imports.GetClusterSetIPs(namespacedName)

	if event == model.EventDelete || len(vips) == 0 {
		if prevMcsService != nil {
			// There are no vips in this cluster. Just delete the MCS service now.
			ic.deleteService(prevMcsService)
		}
		return
	}

	if prevMcsService != nil {
		event = model.EventUpdate
	} else {
		event = model.EventAdd
	}

	mcsService := ic.genMCSService(svc, mcsHost, vips)
	ic.addOrUpdateService(nil, mcsService, event)
}

func (ic *serviceImportCacheImpl) onServiceImportEvent(obj interface{}, event model.Event) error {
	si, ok := obj.(*mcs.ServiceImport)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %#v", obj)
		}
		si, ok = tombstone.Obj.(*mcs.ServiceImport)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a ServiceImport %#v", obj)
		}
	}

	// We need a full push if the cluster VIP changes.
	needsFullPush := false

	// Get the updated MCS service.
	mcsHost := serviceClusterSetLocalHostnameForKR(si)
	mcsService := ic.GetService(mcsHost)
	if mcsService == nil {
		if event == model.EventDelete || len(si.Spec.IPs) == 0 {
			// We never created the service. Nothing to delete.
			return nil
		}

		// The service didn't exist prior. Treat it as an add.
		event = model.EventAdd

		// Create the MCS service, based on the cluster.local service. We get the merged, mesh-wide service
		// from the aggregate controller so that we don't rely on the service existing in this cluster.
		realService := ic.opts.MeshServiceController.GetService(kube.ServiceHostnameForKR(si, ic.opts.DomainSuffix))
		if realService == nil {
			log.Warnf("failed processing %s event for ServiceImport %s/%s in cluster %s. No matching service found in cluster",
				event, si.Namespace, si.Name, ic.Cluster())
			return nil
		}

		// Create the MCS service from the cluster.local service.
		mcsService = ic.genMCSService(realService, mcsHost, si.Spec.IPs)
	} else {
		if event == model.EventDelete || len(si.Spec.IPs) == 0 {
			ic.deleteService(mcsService)
			return nil
		}

		// The service already existed. Treat it as an update.
		event = model.EventUpdate

		// Update the VIPs
		mcsService.ClusterVIPs.SetAddressesFor(ic.Cluster(), si.Spec.IPs)
		needsFullPush = true
	}

	ic.addOrUpdateService(nil, mcsService, event)

	if needsFullPush {
		pushReq := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      gvk.ServiceEntry,
				Name:      mcsHost.String(),
				Namespace: si.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ServiceUpdate},
		}
		ic.opts.XDSUpdater.ConfigUpdate(pushReq)
	}

	return nil
}

func (ic *serviceImportCacheImpl) genMCSService(realService *model.Service, mcsHost host.Name, vips []string) *model.Service {
	mcsService := realService.DeepCopy()
	mcsService.Hostname = mcsHost

	if len(vips) > 0 {
		mcsService.DefaultAddress = vips[0]
		mcsService.ClusterVIPs.SetAddresses(map[cluster.ID][]string{
			ic.Cluster(): vips,
		})
	} else {
		mcsService.DefaultAddress = ""
		mcsService.ClusterVIPs.SetAddresses(nil)
	}
	return mcsService
}

func (ic *serviceImportCacheImpl) GetClusterSetIPs(name types.NamespacedName) []string {
	if si, _ := ic.lister.ServiceImports(name.Namespace).Get(name.Name); si != nil {
		return si.Spec.IPs
	}
	return nil
}

func (ic *serviceImportCacheImpl) ImportedServices() []importedService {
	sis, err := ic.lister.List(klabels.Everything())
	if err != nil {
		return make([]importedService, 0)
	}

	// Iterate over the ServiceImport resources in this cluster.
	out := make([]importedService, 0, len(sis))

	ic.RLock()
	for _, si := range sis {
		info := importedService{
			namespacedName: kube.NamespacedNameForK8sObject(si),
		}

		// Lookup the synthetic MCS service.
		hostName := serviceClusterSetLocalHostnameForKR(si)
		svc := ic.servicesMap[hostName]
		if svc != nil {
			if vips := svc.ClusterVIPs.GetAddressesFor(ic.Cluster()); len(vips) > 0 {
				info.clusterSetVIP = vips[0]
			}
		}

		out = append(out, info)
	}
	ic.RUnlock()

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

func (c disabledServiceImportCache) ImportedServices() []importedService {
	// MCS is disabled - returning `nil`, which is semantically different here than an empty list.
	return nil
}
