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

	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/networking/v1alpha3"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func (a *AmbientIndexImpl) HandleServiceEntry(svc *model.Service, event model.Event, c *Controller) {
	if svc.Attributes.ServiceEntry == nil {
		// event for e.g. kube svc; ignore
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// We will accrue updates as we update our internal state
	updates := sets.New[model.ConfigKey]()

	if event != model.EventAdd {
		a.cleanupOldWorkloadEntriesInlinedOnServiceEntry(svc, updates, c)
	}

	serviceEntryNamespacedName := types.NamespacedName{
		Name:      svc.Attributes.ServiceEntryName,
		Namespace: svc.Attributes.ServiceEntryNamespace,
	}

	// Update indexes
	if event == model.EventDelete {
		// servicesMap is used when cleaning up old WEs inlined on a SE (i.e., `ServiceEntry.endpoints`)
		// so we must delete this after we clean up the old WEs. That way we don't miss any auto-allocated
		// VIPs during cleanup on the idx.byWorkloadEntry[networkAddr] map
		delete(a.servicesMap, serviceEntryNamespacedName)
	} else {
		// servicesMap is used when constructing workloads so it must be up to date
		a.servicesMap[serviceEntryNamespacedName] = svc
	}

	sel := klabels.Set(svc.Attributes.ServiceEntry.WorkloadSelector.GetLabels()).AsSelectorPreValidated()
	var pods []*v1.Pod
	if !sel.Empty() {
		pods = c.podsClient.List(svc.Attributes.ServiceEntryNamespace, sel)
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

	workloadEntries := c.getSelectedWorkloadEntries(svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntry.GetWorkloadSelector().GetLabels())
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
			a.byUID[c.generateServiceEntryUID(svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntryName, w.Spec.GetAddress())] = wl
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			wls[wl.Uid] = wl
		}
	}

	for _, we := range svc.Attributes.ServiceEntry.Endpoints {
		wli := a.extractWorkloadEntrySpec(we, svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntryName, svc, c)
		if wli != nil && event != model.EventDelete {
			for _, networkAddr := range networkAddressFromWorkload(wli) {
				a.byWorkloadEntry[networkAddr] = wli
			}
			a.byUID[c.generateServiceEntryUID(svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntryName, we.GetAddress())] = wli
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wli.ResourceName()})
			wls[wli.Uid] = wli
		}
	}

	vips := getVIPsFromServiceEntry(svc)
	var addrs []*workloadapi.NetworkAddress
	for _, vip := range vips {
		addrs = append(addrs, &workloadapi.NetworkAddress{
			Network: c.network.String(),
			Address: parseIP(vip),
		})
	}
	var allPorts []*workloadapi.Port
	for _, port := range svc.Attributes.ServiceEntry.Ports {
		allPorts = append(allPorts, &workloadapi.Port{
			ServicePort: port.Number,
			TargetPort:  port.TargetPort,
		})
	}
	var allSubjAltNames []string
	allSubjAltNames = append(allSubjAltNames, svc.Attributes.ServiceEntry.SubjectAltNames...)

	si := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:            svc.Attributes.ServiceEntryName,
			Namespace:       svc.Attributes.ServiceEntryNamespace,
			Hostname:        string(svc.Hostname),
			Addresses:       addrs,
			Ports:           allPorts,
			SubjectAltNames: allSubjAltNames,
		},
	}

	networkAddrs := toInternalNetworkAddresses(si.GetAddresses())

	// We send an update for each *workload* IP address previously in the service; they may have changed
	for _, wl := range a.byService[si.ResourceName()] {
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
	}
	// Update indexes
	if event == model.EventDelete {
		for _, networkAddr := range networkAddrs {
			delete(a.serviceByAddr, networkAddr)
		}
		delete(a.byService, si.ResourceName())
		delete(a.serviceByNamespacedHostname, si.ResourceName())
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
	} else {
		for _, networkAddr := range networkAddrs {
			a.serviceByAddr[networkAddr] = si
		}
		a.byService[si.ResourceName()] = wls
		a.serviceByNamespacedHostname[si.ResourceName()] = si
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
	}
	// Fetch updates again, in case it changed from adding new workloads
	for _, wl := range a.byService[si.ResourceName()] {
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
	}

	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (c *Controller) getWorkloadEntriesInPolicy(ns string, sel map[string]string) []*apiv1alpha3.WorkloadEntry {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}

	return c.getSelectedWorkloadEntries(ns, sel)
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) extractWorkloadEntry(w *apiv1alpha3.WorkloadEntry, c *Controller) *model.WorkloadInfo {
	if w == nil {
		return nil
	}
	return a.extractWorkloadEntrySpec(&w.Spec, w.Namespace, w.Name, nil, c)
}

func (a *AmbientIndexImpl) extractWorkloadEntrySpec(w *v1alpha3.WorkloadEntry, ns, name string,
	parentServiceEntry *model.Service, c *Controller,
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
	wl := c.constructWorkloadFromWorkloadEntry(w, ns, name, parentServiceEntry, waypoint, policies, a.servicesMap)
	if wl == nil {
		return nil
	}
	return &model.WorkloadInfo{
		Workload: wl,
		Labels:   w.Labels,
	}
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) handleWorkloadEntry(oldWorkloadEntry, w *apiv1alpha3.WorkloadEntry, isDelete bool, c *Controller) sets.Set[model.ConfigKey] {
	if oldWorkloadEntry != nil {
		// compare only labels and annotations, which are what we care about
		if maps.Equal(oldWorkloadEntry.Labels, w.Labels) &&
			maps.Equal(oldWorkloadEntry.Annotations, w.Annotations) {
			return nil
		}
	}

	updates := sets.New[model.ConfigKey]()

	var wl *model.WorkloadInfo
	if !isDelete {
		wl = a.extractWorkloadEntry(w, c)
	}

	wlNetwork := c.Network(w.Spec.Address, w.Spec.Labels).String()
	networkAddr := networkAddress{network: wlNetwork, ip: w.Spec.Address}
	uid := c.generateWorkloadEntryUID(w.GetNamespace(), w.GetName())
	oldWl := a.byUID[uid]
	if wl == nil {
		// This is an explicit delete event, or there is no longer a Workload to create (VM NotReady, etc)
		delete(a.byWorkloadEntry, networkAddr)
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
	a.byWorkloadEntry[networkAddr] = wl
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

func (c *Controller) constructWorkloadFromWorkloadEntry(workloadEntry *v1alpha3.WorkloadEntry, workloadEntryNamespace, workloadEntryName string,
	parentServiceEntry *model.Service, waypoint *workloadapi.GatewayAddress, policies []string, serviceEntries map[types.NamespacedName]*model.Service,
) *workloadapi.Workload {
	if workloadEntry == nil {
		return nil
	}

	// TODO support WorkloadEntry's with empty address
	// only add if waypoint exists?
	if workloadEntry.Address == "" {
		log.Warnf("workloadentry %s/%s does not have an address", workloadEntryNamespace, workloadEntryName)
		return nil
	}

	workloadServices := map[string]*workloadapi.PortList{}
	if services := getWorkloadEntryServices(c.services.List(workloadEntryNamespace, klabels.Everything()), workloadEntry); len(services) > 0 {
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
		for nsName, ports := range getWorkloadServices(serviceEntries, workloadEntry, workloadEntryNamespace, workloadEntry.Labels) {
			workloadServices[nsName] = ports
		}
	}

	// for constructing workloads with a single parent (inlined on a SE)
	if parentServiceEntry != nil {
		ports := getPortsForServiceEntry(parentServiceEntry, workloadEntry)
		workloadServices[namespacedHostname(parentServiceEntry.Attributes.ServiceEntryNamespace, parentServiceEntry.Hostname.String())] = ports
	}

	// this can fail if the address is DNS, e.g. "external.external-1-15569.svc.cluster.local"
	addr, err := netip.ParseAddr(workloadEntry.Address)
	if err != nil {
		log.Errorf("skipping ambient workload generation for workload entry %s/%s."+
			"client DNS address resolution is not implemented in ztunnel yet: requested address %v",
			workloadEntryNamespace, workloadEntryName, workloadEntry.Address)
		return nil
	}

	uid := c.generateWorkloadEntryUID(workloadEntryNamespace, workloadEntryName)
	if parentServiceEntry != nil {
		uid = c.generateServiceEntryUID(parentServiceEntry.Attributes.ServiceEntryNamespace, parentServiceEntry.Attributes.ServiceEntryName, addr.String())
	}

	network := c.Network(workloadEntry.Address, workloadEntry.Labels).String()
	if workloadEntry.Network != "" {
		network = workloadEntry.Network
	}

	wl := &workloadapi.Workload{
		Uid:                   uid,
		Name:                  workloadEntryName,
		Namespace:             workloadEntryNamespace,
		Addresses:             [][]byte{addr.AsSlice()},
		Network:               network,
		ServiceAccount:        workloadEntry.ServiceAccount,
		Services:              workloadServices,
		AuthorizationPolicies: policies,
		Waypoint:              waypoint,
	}
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
		wl.TrustDomain = td
	}

	wl.WorkloadName, wl.WorkloadType = workloadEntryName, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
	wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(workloadEntry.Labels, wl.WorkloadName)

	isMeshExternal := parentServiceEntry != nil && parentServiceEntry.Attributes.ServiceEntry.Location == v1alpha3.ServiceEntry_MESH_EXTERNAL

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
func (a *AmbientIndexImpl) updateWaypointForWorkload(byWorkload map[networkAddress]*model.WorkloadInfo, scope model.WaypointScope,
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

func (c *Controller) getWorkloadEntriesInService(svc *v1.Service) []*apiv1alpha3.WorkloadEntry {
	return c.getSelectedWorkloadEntries(svc.GetNamespace(), svc.Spec.Selector)
}

func (c *Controller) getSelectedWorkloadEntries(ns string, selector map[string]string) []*apiv1alpha3.WorkloadEntry {
	allWorkloadEntries := c.getControllerWorkloadEntries(ns)
	if len(selector) == 0 {
		// k8s services and service entry workloadSelector with empty selectors match nothing, not everything.
		return nil
	}
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

func (a *AmbientIndexImpl) cleanupOldWorkloadEntriesInlinedOnServiceEntry(svc *model.Service, updates sets.Set[model.ConfigKey], c *Controller) {
	nsName := types.NamespacedName{
		Name:      svc.Attributes.ServiceEntryName,
		Namespace: svc.Attributes.ServiceEntryNamespace,
	}

	// cleanup any old WorkloadEntries generated from this ServiceEntry (`ServiceEntry.endpoints`)
	if oldServiceEntry, f := a.servicesMap[nsName]; f && oldServiceEntry.Attributes.ServiceEntry != nil {
		for _, oldWe := range oldServiceEntry.Attributes.ServiceEntry.Endpoints {
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

func getWorkloadServices(serviceEntries map[types.NamespacedName]*model.Service, workloadEntry *v1alpha3.WorkloadEntry,
	workloadNamespace string, workloadLabels map[string]string,
) map[string]*workloadapi.PortList {
	workloadServices := map[string]*workloadapi.PortList{}
	for _, svc := range serviceEntries {

		if svc.Attributes.ServiceEntry == nil {
			// if we are here then this is dev error
			log.Warn("dev error: service entry spec is nil; it should have been populated by the service entry handler")
			continue
		}

		if svc.Attributes.ServiceEntryNamespace != workloadNamespace {
			// service entry can only select workloads in the same namespace
			continue
		}

		if svc.Attributes.ServiceEntry.WorkloadSelector == nil {
			// nothing to do. we construct the ztunnel config if `endpoints` are provided in the service entry handler
			continue
		}

		if svc.Attributes.ServiceEntry.Endpoints != nil {
			// it is an error to provide both `endpoints` and `workloadSelector` in a service entry
			continue
		}

		sel := svc.Attributes.ServiceEntry.WorkloadSelector.Labels
		if !labels.Instance(sel).SubsetOf(workloadLabels) {
			continue
		}

		ports := getPortsForServiceEntry(svc, workloadEntry)
		workloadServices[namespacedHostname(svc.Attributes.ServiceEntryNamespace, svc.Hostname.String())] = ports
	}

	return workloadServices
}

func getVIPsFromServiceEntry(svc *model.Service) []string {
	var vips []string
	vips = append(vips, svc.Attributes.ServiceEntry.Addresses...)
	if len(svc.AutoAllocatedIPv4Address) > 0 {
		vips = append(vips, svc.AutoAllocatedIPv4Address)
	}
	if len(svc.AutoAllocatedIPv6Address) > 0 {
		vips = append(vips, svc.AutoAllocatedIPv4Address)
	}
	// in the future we may want to include cluster VIPs from svc.ClusterVIPs
	return vips
}

func getPortsForServiceEntry(svc *model.Service, we *v1alpha3.WorkloadEntry) *workloadapi.PortList {
	if svc == nil {
		return nil
	}
	var ports *workloadapi.PortList
	for _, port := range svc.Attributes.ServiceEntry.Ports {

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
