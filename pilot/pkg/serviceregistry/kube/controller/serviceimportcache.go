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
	"net"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/mcs"
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
	HasSynced() bool
	ImportedServices() []importedService
}

// newServiceImportCache creates a new cache of ServiceImport resources in the cluster.
func newServiceImportCache(c *Controller) serviceImportCache {
	if features.EnableMCSHost {
		dInformer := c.client.DynamicInformer().ForResource(mcs.ServiceImportGVR)
		_ = dInformer.Informer().SetTransform(kubelib.StripUnusedFields)
		sic := &serviceImportCacheImpl{
			Controller: c,
			informer:   dInformer.Informer(),
			lister:     dInformer.Lister(),
		}

		// Register callbacks for Service events anywhere in the mesh.
		c.opts.MeshServiceController.AppendServiceHandlerForCluster(c.Cluster(), sic.onServiceEvent)

		// Register callbacks for ServiceImport events in this cluster only.
		c.registerHandlers(sic.informer, "ServiceImports", sic.onServiceImportEvent, nil)
		return sic
	}

	// MCS Service discovery is disabled. Use a placeholder cache.
	return disabledServiceImportCache{}
}

// serviceImportCacheImpl reads ServiceImport resources for a single cluster.
type serviceImportCacheImpl struct {
	*Controller
	informer cache.SharedIndexInformer
	lister   cache.GenericLister
}

// onServiceEvent is called when the controller receives an event for the kube Service (i.e. cluster.local).
// When this happens, we need to update the state of the associated synthetic MCS service.
func (ic *serviceImportCacheImpl) onServiceEvent(svc *model.Service, event model.Event) {
	if strings.HasSuffix(svc.Hostname.String(), mcsDomainSuffix) {
		// Ignore events for MCS services that were triggered by this controller.
		return
	}

	// This method is called concurrently from each cluster's queue. Process it in `this` cluster's queue
	// in order to synchronize event processing.
	ic.queue.Push(func() error {
		namespacedName := namespacedNameForService(svc)

		// Lookup the previous MCS service if there was one.
		mcsHost := serviceClusterSetLocalHostname(namespacedName)
		prevMcsService := ic.GetService(mcsHost)

		// Get the ClusterSet VIPs for this service in this cluster. Will only be populated if the
		// service has a ServiceImport in this cluster.
		vips := ic.getClusterSetIPs(namespacedName)
		name := namespacedName.Name
		ns := namespacedName.Namespace

		if len(vips) == 0 || (event == model.EventDelete &&
			ic.opts.MeshServiceController.GetService(kube.ServiceHostname(name, ns, ic.opts.DomainSuffix)) == nil) {
			if prevMcsService != nil {
				// There are no vips in this cluster. Just delete the MCS service now.
				ic.deleteService(prevMcsService)
			}
			return nil
		}

		if prevMcsService != nil {
			event = model.EventUpdate
		} else {
			event = model.EventAdd
		}

		mcsService := ic.genMCSService(svc, mcsHost, vips)
		ic.addOrUpdateService(nil, mcsService, event, false)
		return nil
	})
}

func (ic *serviceImportCacheImpl) onServiceImportEvent(obj interface{}, event model.Event) error {
	si, ok := obj.(*unstructured.Unstructured)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %#v", obj)
		}
		si, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a ServiceImport %#v", obj)
		}
	}

	// We need a full push if the cluster VIP changes.
	needsFullPush := false

	// Get the updated MCS service.
	mcsHost := serviceClusterSetLocalHostnameForKR(si)
	mcsService := ic.GetService(mcsHost)

	ips := GetServiceImportIPs(si)
	if mcsService == nil {
		if event == model.EventDelete || len(ips) == 0 {
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
				event, si.GetNamespace(), si.GetName(), ic.Cluster())
			return nil
		}

		// Create the MCS service from the cluster.local service.
		mcsService = ic.genMCSService(realService, mcsHost, ips)
	} else {
		if event == model.EventDelete || len(ips) == 0 {
			ic.deleteService(mcsService)
			return nil
		}

		// The service already existed. Treat it as an update.
		event = model.EventUpdate

		if ic.updateIPs(mcsService, ips) {
			needsFullPush = true
		}
	}

	// Always force a rebuild of the endpoint cache in case this import caused
	// a change to the discoverability policy.
	ic.addOrUpdateService(nil, mcsService, event, true)

	if needsFullPush {
		ic.doFullPush(mcsHost, si.GetNamespace())
	}

	return nil
}

func (ic *serviceImportCacheImpl) updateIPs(mcsService *model.Service, ips []string) (updated bool) {
	prevIPs := mcsService.ClusterVIPs.GetAddressesFor(ic.Cluster())
	if !util.StringSliceEqual(prevIPs, ips) {
		// Update the VIPs
		mcsService.ClusterVIPs.SetAddressesFor(ic.Cluster(), ips)
		updated = true
	}
	return
}

func (ic *serviceImportCacheImpl) doFullPush(mcsHost host.Name, ns string) {
	pushReq := &model.PushRequest{
		Full: true,
		ConfigsUpdated: map[model.ConfigKey]struct{}{{
			Kind:      kind.ServiceEntry,
			Name:      mcsHost.String(),
			Namespace: ns,
		}: {}},
		Reason: []model.TriggerReason{model.ServiceUpdate},
	}
	ic.opts.XDSUpdater.ConfigUpdate(pushReq)
}

// GetServiceImportIPs returns the list of ClusterSet IPs for the ServiceImport.
// Exported for testing only.
func GetServiceImportIPs(si *unstructured.Unstructured) []string {
	var ips []string
	if spec, ok := si.Object["spec"].(map[string]interface{}); ok {
		if rawIPs, ok := spec["ips"].([]interface{}); ok {
			for _, rawIP := range rawIPs {
				ip := rawIP.(string)
				if net.ParseIP(ip) != nil {
					ips = append(ips, ip)
				}
			}
		}
	}
	sort.Strings(ips)
	return ips
}

// genMCSService generates an MCS service based on the given real k8s service. The list of vips must be non-empty.
func (ic *serviceImportCacheImpl) genMCSService(realService *model.Service, mcsHost host.Name, vips []string) *model.Service {
	mcsService := realService.DeepCopy()
	mcsService.Hostname = mcsHost
	mcsService.DefaultAddress = vips[0]
	mcsService.ClusterVIPs.Addresses = map[cluster.ID][]string{
		ic.Cluster(): vips,
	}

	return mcsService
}

func (ic *serviceImportCacheImpl) getClusterSetIPs(name types.NamespacedName) []string {
	if si, err := ic.lister.ByNamespace(name.Namespace).Get(name.Name); err == nil {
		return GetServiceImportIPs(si.(*unstructured.Unstructured))
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
		usi := si.(*unstructured.Unstructured)
		info := importedService{
			namespacedName: kube.NamespacedNameForK8sObject(usi),
		}

		// Lookup the synthetic MCS service.
		hostName := serviceClusterSetLocalHostnameForKR(usi)
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

func (c disabledServiceImportCache) HasSynced() bool {
	return true
}

func (c disabledServiceImportCache) ImportedServices() []importedService {
	// MCS is disabled - returning `nil`, which is semantically different here than an empty list.
	return nil
}
