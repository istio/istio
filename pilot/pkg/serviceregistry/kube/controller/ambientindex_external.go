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
	"net/netip"

	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/networking/v1alpha3"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func (a *AmbientIndexImpl) handleServiceEntry(svcEntry *apiv1alpha3.ServiceEntry, event model.Event, c *Controller) sets.Set[model.ConfigKey] {
	if len(svcEntry.Spec.Hosts) == 0 {
		log.Warnf("ServiceEntry %s/%s is invalid as it has no hosts", svcEntry.GetNamespace(), svcEntry.GetName())
		return sets.New[model.ConfigKey]()
	}

	// We will accrue updates as we update our internal state
	updates := sets.New[model.ConfigKey]()

	if event != model.EventAdd {
		a.cleanupOldWorkloadEntriesInlinedOnServiceEntry(svcEntry, updates, c)
	}

	serviceEntryNamespacedName := types.NamespacedName{
		Name:      svcEntry.GetName(),
		Namespace: svcEntry.GetNamespace(),
	}

	// Update indexes
	if event == model.EventDelete {
		// servicesMap is used when cleaning up old WEs inlined on a SE (i.e., `ServiceEntry.endpoints`)
		//
		// prefer this style to better enable us for future support for auto-allocated VIPs on ServiceEntries.
		// this is necessary because not all information is available in the ServiceEntry spec
		delete(a.servicesMap, serviceEntryNamespacedName)
	} else {
		// servicesMap is used when constructing workloads so it must be up to date
		a.servicesMap[serviceEntryNamespacedName] = svcEntry
	}

	sel := klabels.ValidatedSetSelector(klabels.Set(svcEntry.Spec.WorkloadSelector.GetLabels()))
	var pods []*v1.Pod
	if !sel.Empty() {
		pods = c.podsClient.List(svcEntry.GetNamespace(), sel)
	}
	wls := make(map[string]*model.WorkloadInfo, len(pods))
	for _, pod := range pods {
		newWl := a.extractWorkload(pod, c)
		if newWl != nil {
			// Update the pod, since it now has new VIP info
			networkAddrs := networkAddressFromWorkload(newWl)
			for _, networkAddr := range networkAddrs {
				a.byPod[networkAddr] = newWl
			}
			a.byUID[c.generatePodUID(pod)] = newWl
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: newWl.ResourceName()})
			wls[newWl.Uid] = newWl
		}
	}

	workloadEntries := c.getSelectedWorkloadEntries(svcEntry.GetNamespace(), svcEntry.Spec.GetWorkloadSelector().GetLabels())
	for _, w := range workloadEntries {
		wl := a.extractWorkloadEntry(w, c)
		// Can be nil if the WorkloadEntry IP has not been mapped yet
		//
		// Note: this is a defensive check that mimics the logic for
		// pods above. WorkloadEntries are mapped by their IP address
		// in the following cases:
		// 1. WorkloadEntry add/update
		// 2. AuthorizationPolicy add/update
		// 3. Namespace Ambient label add/update
		if wl != nil {
			// Update the WorkloadEntry, since it now has new VIP info
			for _, networkAddr := range networkAddressFromWorkload(wl) {
				a.byWorkloadEntry[networkAddr] = wl
			}
			a.byUID[c.generateWorkloadEntryUID(wl.GetNamespace(), wl.GetName())] = wl
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			wls[wl.Uid] = wl
		}
	}

	for _, we := range svcEntry.Spec.Endpoints {
		uid := c.generateServiceEntryUID(svcEntry.GetNamespace(), svcEntry.GetName(), we.GetAddress())
		oldWl := a.byUID[uid]
		var wl *model.WorkloadInfo
		if event != model.EventDelete {
			wl = a.extractWorkloadEntrySpec(we, svcEntry.GetNamespace(), svcEntry.GetName(), svcEntry, c)
		}
		a.updateWorkloadIndexes(oldWl, wl, updates)
	}

	vips := getVIPsFromServiceEntry(svcEntry)
	var addrs []*workloadapi.NetworkAddress
	for _, vip := range vips {
		addrs = append(addrs, &workloadapi.NetworkAddress{
			Network: c.network.String(),
			Address: parseIP(vip),
		})
	}
	var allPorts []*workloadapi.Port
	for _, port := range svcEntry.Spec.Ports {
		allPorts = append(allPorts, &workloadapi.Port{
			ServicePort: port.Number,
			TargetPort:  port.TargetPort,
		})
	}
	var allSubjAltNames []string
	allSubjAltNames = append(allSubjAltNames, svcEntry.Spec.SubjectAltNames...)

	// for each host make a service info
	var serviceInfos []*model.ServiceInfo
	for _, host := range svcEntry.Spec.Hosts {
		serviceInfos = append(serviceInfos, &model.ServiceInfo{
			Service: &workloadapi.Service{
				Name:            svcEntry.GetName(),
				Namespace:       svcEntry.GetNamespace(),
				Hostname:        host,
				Addresses:       addrs,
				Ports:           allPorts,
				SubjectAltNames: allSubjAltNames,
			},
		})
	}

	// we already validated that there is at least one host at the beginning of this function, so this is safe
	sampleSi := serviceInfos[0]

	networkAddrs := toInternalNetworkAddresses(sampleSi.GetAddresses())

	// We send an update for each *workload* IP address previously in the service; they may have changed
	for _, si := range serviceInfos {
		for _, wl := range a.byService[si.ResourceName()] {
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
		}
	}
	// Update indexes
	if event == model.EventDelete {
		for _, networkAddr := range networkAddrs {
			delete(a.serviceByAddr, networkAddr)
		}
		for _, si := range serviceInfos {
			delete(a.byService, si.ResourceName())
			delete(a.serviceByNamespacedHostname, si.ResourceName())
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
		}
	} else {
		for _, networkAddr := range networkAddrs {
			// in ambient, we only allow a network address to map to a single host. we dedup by just mapping the first one
			a.serviceByAddr[networkAddr] = sampleSi
		}
		for _, si := range serviceInfos {
			a.byService[si.ResourceName()] = wls
			a.serviceByNamespacedHostname[si.ResourceName()] = si
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
		}
	}
	// Fetch updates again, in case it changed from adding new workloads
	for _, si := range serviceInfos {
		for _, wl := range a.byService[si.ResourceName()] {
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
		}
	}

	return updates
}

func (c *Controller) getWorkloadEntriesInPolicy(ns string, sel map[string]string) []*apiv1alpha3.WorkloadEntry {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}

	return c.getSelectedWorkloadEntries(ns, sel)
}

func (c *Controller) getServiceEntryEndpointsInPolicy(ns string, sel map[string]string) map[*apiv1alpha3.ServiceEntry]sets.Set[*v1alpha3.WorkloadEntry] {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}

	return c.getSelectedServiceEntries(ns, sel)
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) extractWorkloadEntry(w *apiv1alpha3.WorkloadEntry, c *Controller) *model.WorkloadInfo {
	if w == nil {
		return nil
	}
	return a.extractWorkloadEntrySpec(&w.Spec, w.Namespace, w.Name, nil, c)
}

func (a *AmbientIndexImpl) extractWorkloadEntrySpec(w *v1alpha3.WorkloadEntry, ns, name string,
	parentServiceEntry *apiv1alpha3.ServiceEntry, c *Controller,
) *model.WorkloadInfo {
	if w == nil {
		return nil
	}
	var waypoint *workloadapi.GatewayAddress
	if w.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		// Waypoints do not have waypoints
	} else {
		// First check for a waypoint for our SA explicit
		// TODO: this is not robust against temporary waypoint downtime. We also need the users intent (Gateway).
		found := false
		if waypoint, found = a.waypoints[model.WaypointScope{Namespace: ns, ServiceAccount: w.ServiceAccount}]; !found {
			// if there are none, check namespace wide waypoints
			waypoint = a.waypoints[model.WaypointScope{Namespace: ns}]
		}
	}
	policies := c.selectorAuthorizationPolicies(ns, w.Labels)
	wl := a.constructWorkloadFromWorkloadEntry(w, ns, name, parentServiceEntry, waypoint, policies, c)
	if wl == nil {
		return nil
	}
	source := model.WorkloadSourceWorkloadEntry
	if parentServiceEntry != nil {
		source = model.WorkloadSourceServiceEntry
	}
	return &model.WorkloadInfo{
		Workload: wl,
		Labels:   w.Labels,
		Source:   source,
	}
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) handleWorkloadEntry(oldWorkloadEntry, w *apiv1alpha3.WorkloadEntry, isDelete bool, c *Controller) sets.Set[model.ConfigKey] {
	if oldWorkloadEntry != nil {
		// compare only labels, annotations, and spec; which are what we care about
		if proto.Equal(&oldWorkloadEntry.Spec, &w.Spec) &&
			maps.Equal(oldWorkloadEntry.Annotations, w.Annotations) &&
			maps.Equal(oldWorkloadEntry.Labels, w.Labels) {
			return nil
		}
	}

	updates := sets.New[model.ConfigKey]()

	var wl *model.WorkloadInfo
	if !isDelete {
		wl = a.extractWorkloadEntry(w, c)
	}

	wlNetwork := c.Network(w.Spec.Address, w.Spec.Labels).String()
	var networkAddr *networkAddress
	if addr := w.Spec.Address; addr != "" {
		networkAddr = &networkAddress{network: wlNetwork, ip: addr}
	}
	uid := c.generateWorkloadEntryUID(w.GetNamespace(), w.GetName())
	oldWl := a.byUID[uid]
	if wl == nil {
		// This is an explicit delete event, or there is no longer a Workload to create (VM NotReady, etc)
		if networkAddr != nil {
			delete(a.byWorkloadEntry, *networkAddr)
		}
		delete(a.byUID, uid)
		if oldWl != nil {
			// If we already knew about this workload, we need to make sure we drop all service references as well
			for namespacedHostname := range oldWl.Services {
				a.dropWorkloadFromService(namespacedHostname, oldWl.ResourceName())
			}
			log.Debugf("%v: workload removed, pushing", oldWl.ResourceName())
			return map[model.ConfigKey]struct{}{
				// TODO: namespace for network?
				{Kind: kind.Address, Name: oldWl.ResourceName()}: {},
			}
		}
		// It was a 'delete' for a resource we didn't know yet, no need to send an event
		return updates
	}

	if oldWl != nil && proto.Equal(wl.Workload, oldWl.Workload) {
		log.Debugf("%v: no change, skipping", wl.ResourceName())
		return updates
	}
	if networkAddr != nil {
		a.byWorkloadEntry[*networkAddr] = wl
	}
	a.byUID[uid] = wl
	if oldWl != nil {
		// For updates, we will drop the services and then add the new ones back. This could be optimized
		for namespacedHostname := range oldWl.Services {
			a.dropWorkloadFromService(namespacedHostname, wl.ResourceName())
		}
	}
	// Update the service indexes as well, as needed
	for namespacedHostname := range wl.Services {
		a.insertWorkloadToService(namespacedHostname, wl)
	}

	log.Debugf("%v: workload updated, pushing", wl.ResourceName())
	updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
	return updates
}

func (a *AmbientIndexImpl) constructWorkloadFromWorkloadEntry(workloadEntry *v1alpha3.WorkloadEntry, workloadEntryNamespace, workloadEntryName string,
	parentServiceEntry *apiv1alpha3.ServiceEntry, waypoint *workloadapi.GatewayAddress, policies []string, c *Controller,
) *workloadapi.Workload {
	if workloadEntry == nil {
		return nil
	}

	workloadServices := map[string]*workloadapi.PortList{}
	services := getWorkloadEntryServices(c.services.List(workloadEntryNamespace, klabels.Everything()), workloadEntry)
	if features.EnableK8SServiceSelectWorkloadEntries && len(services) > 0 {
		for _, svc := range services {
			ports := &workloadapi.PortList{}
			for _, port := range svc.Spec.Ports {
				if port.Protocol != v1.ProtocolTCP {
					continue
				}
				targetPort, err := findPortForWorkloadEntry(workloadEntry, &port)
				if err != nil {
					log.Errorf("error looking up port for WorkloadEntry %s/%s", workloadEntryNamespace, workloadEntryName)
					continue
				}
				ports.Ports = append(ports.Ports, &workloadapi.Port{
					ServicePort: uint32(port.Port),
					TargetPort:  targetPort,
				})
			}
			workloadServices[c.namespacedHostname(svc)] = ports
		}
	}

	// for constructing a workload from a standalone workload entry, which can be selected by many service entries
	if parentServiceEntry == nil {
		for nsName, ports := range a.getWorkloadServicesFromServiceEntries(workloadEntry, workloadEntryNamespace, workloadEntry.Labels) {
			workloadServices[nsName] = ports
		}
	}

	// for constructing workloads with a single parent (inlined on a SE)
	if parentServiceEntry != nil {
		ports := getPortsForServiceEntry(parentServiceEntry, workloadEntry)
		for _, host := range parentServiceEntry.Spec.Hosts {
			workloadServices[namespacedHostname(parentServiceEntry.GetNamespace(), host)] = ports
		}
	}

	var addrBytes []byte
	if workloadEntry.Address != "" {
		// this can fail if the address is DNS, e.g. "external.external-1-15569.svc.cluster.local"
		addr, err := netip.ParseAddr(workloadEntry.Address)
		if err != nil {
			log.Errorf("skipping ambient workload generation for workload entry %s/%s."+
				"client DNS address resolution is not implemented in ztunnel yet: requested address: %v",
				workloadEntryNamespace, workloadEntryName, workloadEntry.Address)
			return nil
		}
		addrBytes = addr.AsSlice()
	}

	uid := c.generateWorkloadEntryUID(workloadEntryNamespace, workloadEntryName)
	if parentServiceEntry != nil {
		uid = c.generateServiceEntryUID(parentServiceEntry.GetNamespace(), parentServiceEntry.GetName(), workloadEntry.Address)
	}

	network := c.Network(workloadEntry.Address, workloadEntry.Labels).String()
	if workloadEntry.Network != "" {
		network = workloadEntry.Network
	}

	var addresses [][]byte
	if addrBytes != nil {
		addresses = [][]byte{addrBytes}
	}

	wl := &workloadapi.Workload{
		Uid:                   uid,
		Name:                  workloadEntryName,
		Namespace:             workloadEntryNamespace,
		Addresses:             addresses,
		Network:               network,
		ServiceAccount:        workloadEntry.ServiceAccount,
		Services:              workloadServices,
		AuthorizationPolicies: policies,
		Waypoint:              waypoint,
		ClusterId:             c.Cluster().String(),
	}
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
		wl.TrustDomain = td
	}

	wl.WorkloadName, wl.WorkloadType = workloadEntryName, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
	wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(workloadEntry.Labels, wl.WorkloadName)

	isMeshExternal := parentServiceEntry != nil && parentServiceEntry.Spec.Location == v1alpha3.ServiceEntry_MESH_EXTERNAL

	// TODO(ambient): For VMs we use Labels instead of an Annotations since we don't
	// have access to the top level WorkloadEntry object. Maybe this is fine?
	if workloadEntry.Labels[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled && !isMeshExternal {
		// Configured for override
		wl.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
	}
	// Otherwise supports tunnel directly
	if model.SupportsTunnel(workloadEntry.Labels, model.TunnelHTTP) {
		wl.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		wl.NativeTunnel = true
	}
	return wl
}

// updateWaypointForWorkload updates the Waypoint configuration for the given Workload(Pod/WorkloadEntry)
func (a *AmbientIndexImpl) updateWaypointForWorkload(byWorkload map[string]*model.WorkloadInfo, scope model.WaypointScope,
	addr *workloadapi.GatewayAddress, isDelete bool, updates sets.Set[model.ConfigKey],
) {
	for _, wl := range byWorkload {
		if wl.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
			continue
		}
		if wl.Namespace != scope.Namespace || (scope.ServiceAccount != "" && wl.ServiceAccount != scope.ServiceAccount) {
			continue
		}
		if isDelete {
			if wl.Waypoint != nil && proto.Equal(wl.Waypoint, addr) {
				wl.Waypoint = nil
				// If there was a change, also update the VIPs and record for a push
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			}
		} else {
			if wl.Waypoint == nil || !proto.Equal(wl.Waypoint, addr) {
				wl.Waypoint = addr
				// If there was a change, also update the VIPs and record for a push
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			}
		}
	}
}

func getWorkloadEntryServices(services []*v1.Service, workloadEntry *v1alpha3.WorkloadEntry) []*v1.Service {
	var filteredServices []*v1.Service
	for _, service := range services {
		if service.Spec.Selector == nil {
			// services with nil selectors match nothing, not everything.
			continue
		}
		if labels.Instance(service.Spec.Selector).SubsetOf(workloadEntry.Labels) {
			filteredServices = append(filteredServices, service)
		}
	}
	return filteredServices
}

func findPortForWorkloadEntry(workloadEntry *v1alpha3.WorkloadEntry, svcPort *v1.ServicePort) (uint32, error) {
	if workloadEntry == nil {
		return 0, fmt.Errorf("invalid input, got nil WorkloadEntry")
	}
	if svcPort == nil {
		return 0, fmt.Errorf("invalid input, got nil ServicePort")
	}

	for portName, portVal := range workloadEntry.Ports {
		if portName == svcPort.Name {
			return portVal, nil
		}
	}

	if svcPort.TargetPort.Type == intstr.Int {
		return uint32(svcPort.TargetPort.IntValue()), nil
	}

	return uint32(svcPort.Port), nil
}

func (c *Controller) getSelectedWorkloadEntries(ns string, selector map[string]string) []*apiv1alpha3.WorkloadEntry {
	// skip WLE for non config clusters
	if !c.configCluster {
		return nil
	}
	if len(selector) == 0 {
		// k8s services and service entry workloadSelector with empty selectors match nothing, not everything.
		return nil
	}
	allWorkloadEntries := c.getControllerWorkloadEntries(ns)
	var workloadEntries []*apiv1alpha3.WorkloadEntry
	for _, wl := range allWorkloadEntries {
		if labels.Instance(selector).SubsetOf(wl.Spec.Labels) {
			workloadEntries = append(workloadEntries, wl)
		}
	}
	return workloadEntries
}

func (c *Controller) getControllerWorkloadEntries(ns string) []*apiv1alpha3.WorkloadEntry {
	var allWorkloadEntries []*apiv1alpha3.WorkloadEntry
	allUnstructuredWorkloadEntries := c.configController.List(gvk.WorkloadEntry, ns)
	for _, w := range allUnstructuredWorkloadEntries {
		conv := serviceentry.ConvertWorkloadEntry(w)
		if conv == nil {
			continue
		}
		c := &apiv1alpha3.WorkloadEntry{
			ObjectMeta: w.ToObjectMeta(),
			Spec:       *conv.DeepCopy(),
		}
		allWorkloadEntries = append(allWorkloadEntries, c)
	}
	return allWorkloadEntries
}

func (c *Controller) getSelectedServiceEntries(ns string, selector map[string]string) map[*apiv1alpha3.ServiceEntry]sets.Set[*v1alpha3.WorkloadEntry] {
	// skip WLE for non config clusters
	if !c.configCluster {
		return nil
	}
	if len(selector) == 0 {
		// k8s services and service entry workloadSelector with empty selectors match nothing, not everything.
		return nil
	}
	allServiceEntries := c.getControllerServiceEntries(ns)
	seEndpoints := map[*apiv1alpha3.ServiceEntry]sets.Set[*v1alpha3.WorkloadEntry]{}
	for _, se := range allServiceEntries {
		for _, we := range se.Spec.Endpoints {
			if labels.Instance(selector).SubsetOf(we.Labels) {
				if seEndpoints[se] == nil {
					seEndpoints[se] = sets.New[*v1alpha3.WorkloadEntry]()
				}
				seEndpoints[se].Insert(we)
			}
		}
	}
	return seEndpoints
}

func (c *Controller) getControllerServiceEntries(ns string) []*apiv1alpha3.ServiceEntry {
	var allServiceEntries []*apiv1alpha3.ServiceEntry
	allUnstructuredServiceEntries := c.configController.List(gvk.ServiceEntry, ns)
	for _, se := range allUnstructuredServiceEntries {
		conv := serviceentry.ConvertServiceEntry(se)
		if conv == nil {
			continue
		}
		c := &apiv1alpha3.ServiceEntry{
			ObjectMeta: se.ToObjectMeta(),
			Spec:       *conv.DeepCopy(),
		}
		allServiceEntries = append(allServiceEntries, c)
	}
	return allServiceEntries
}

// name format: <cluster>/<group>/<kind>/<namespace>/<name></section-name>
// if the WorkloadEntry is inlined in the ServiceEntry, we may need section name. caller should use generateServiceEntryUID
func (c *Controller) generateWorkloadEntryUID(wkEntryNamespace, wkEntryName string) string {
	return c.clusterID.String() + "/networking.istio.io/WorkloadEntry/" + wkEntryNamespace + "/" + wkEntryName
}

// name format: <cluster>/<group>/<kind>/<namespace>/<name></section-name>
// section name should be the WE address, which needs to be stable across SE updates (it is assumed WE addresses are unique)
func (c *Controller) generateServiceEntryUID(svcEntryNamespace, svcEntryName, addr string) string {
	return c.clusterID.String() + "/networking.istio.io/ServiceEntry/" + svcEntryNamespace + "/" + svcEntryName + "/" + addr
}

// prefer this style to better enable us for future support for auto-allocated VIPs on ServiceEntries.
// this is necessary because not all information is available in the ServiceEntry spec
func (a *AmbientIndexImpl) cleanupOldWorkloadEntriesInlinedOnServiceEntry(svcEntry *apiv1alpha3.ServiceEntry,
	updates sets.Set[model.ConfigKey], c *Controller,
) {
	nsName := types.NamespacedName{
		Name:      svcEntry.GetName(),
		Namespace: svcEntry.GetNamespace(),
	}

	// cleanup any old WorkloadEntries generated from this ServiceEntry (`ServiceEntry.endpoints`)
	// we have to do this now before the a.servicesMap is updated to account for vip auto allocation
	if oldServiceEntry, f := a.servicesMap[nsName]; f {
		for _, oldWe := range oldServiceEntry.Spec.Endpoints {
			oldUID := c.generateServiceEntryUID(nsName.Namespace, nsName.Name, oldWe.Address)
			we, found := a.byUID[oldUID]
			if found {
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: we.ResourceName()})
				for _, networkAddr := range networkAddressFromWorkload(we) {
					delete(a.byWorkloadEntry, networkAddr)
				}
				delete(a.byUID, oldUID)
			}
		}
	}
}

func (a *AmbientIndexImpl) getWorkloadServicesFromServiceEntries(workloadEntry *v1alpha3.WorkloadEntry,
	workloadNamespace string, workloadLabels map[string]string,
) map[string]*workloadapi.PortList {
	workloadServices := map[string]*workloadapi.PortList{}
	for _, se := range a.servicesMap {

		if se.GetNamespace() != workloadNamespace {
			// service entry can only select workloads in the same namespace
			continue
		}

		if se.Spec.WorkloadSelector == nil {
			// nothing to do. we construct the ztunnel config if `endpoints` are provided in the service entry handler
			continue
		}

		if se.Spec.Endpoints != nil {
			// it is an error to provide both `endpoints` and `workloadSelector` in a service entry
			continue
		}

		sel := se.Spec.WorkloadSelector.Labels
		if !labels.Instance(sel).SubsetOf(workloadLabels) {
			continue
		}

		ports := getPortsForServiceEntry(se, workloadEntry)
		for _, host := range se.Spec.Hosts {
			workloadServices[namespacedHostname(se.GetNamespace(), host)] = ports
		}
	}

	return workloadServices
}

func getVIPsFromServiceEntry(svc *apiv1alpha3.ServiceEntry) []string {
	var vips []string
	vips = append(vips, svc.Spec.Addresses...)
	// in the future we may want to include cluster VIPs from svc.ClusterVIPs, auto allocated VIPs
	return vips
}

func getPortsForServiceEntry(svcEntry *apiv1alpha3.ServiceEntry, we *v1alpha3.WorkloadEntry) *workloadapi.PortList {
	var ports *workloadapi.PortList
	for _, port := range svcEntry.Spec.Ports {

		if ports == nil {
			ports = &workloadapi.PortList{}
		}

		newPort := &workloadapi.Port{
			ServicePort: port.GetNumber(),
			TargetPort:  port.GetTargetPort(),
		}

		// take WE override port if necessary
		if we != nil {
			for wePortName, wePort := range we.Ports {
				if wePortName == port.Name {
					newPort.TargetPort = wePort
				}
			}
		}
		ports.Ports = append(ports.Ports, newPort)
	}
	return ports
}
