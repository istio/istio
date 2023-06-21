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

	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/networking/v1alpha3"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func (a *AmbientIndex) handleWorkloadEntries(workloadEntries []*apiv1alpha3.WorkloadEntry, c *Controller) {
	updates := sets.New[model.ConfigKey]()
	for _, w := range workloadEntries {
		updates = updates.Merge(c.handleWorkloadEntry(a, nil, w, false))
	}
	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (c *Controller) WorkloadEntryHandler(oldCfg config.Config, newCfg config.Config, ev model.Event) {
	var oldWkEntrySpec *v1alpha3.WorkloadEntry
	if ev == model.EventUpdate {
		oldWkEntrySpec = serviceentry.ConvertWorkloadEntry(oldCfg)
	}
	var oldWkEntry *apiv1alpha3.WorkloadEntry
	if oldWkEntrySpec != nil {
		oldWkEntry = &apiv1alpha3.WorkloadEntry{
			ObjectMeta: oldCfg.ToObjectMeta(),
			Spec:       *oldWkEntrySpec.DeepCopy(),
		}
	}
	newWkEntrySpec := serviceentry.ConvertWorkloadEntry(newCfg)
	var newWkEntry *apiv1alpha3.WorkloadEntry
	if newWkEntrySpec != nil {
		newWkEntry = &apiv1alpha3.WorkloadEntry{
			ObjectMeta: newCfg.ToObjectMeta(),
			Spec:       *newWkEntrySpec.DeepCopy(),
		}
	}

	updates := c.handleWorkloadEntry(c.ambientIndex, oldWkEntry, newWkEntry, ev == model.EventDelete)
	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (c *Controller) initWorkloadEntryHandler(idx *AmbientIndex) {
	if idx == nil {
		return
	}
	c.configController.RegisterEventHandler(gvk.WorkloadEntry, c.WorkloadEntryHandler)
}

func (c *Controller) getWorkloadEntriesInPolicy(ns string, sel map[string]string) []*apiv1alpha3.WorkloadEntry {
	if ns == c.meshWatcher.Mesh().GetRootNamespace() {
		ns = metav1.NamespaceAll
	}

	allWorkloadEntries := c.getControllerWorkloadEntries(ns)
	var workloadEntries []*apiv1alpha3.WorkloadEntry
	for _, w := range allWorkloadEntries {
		if labels.Instance(sel).SubsetOf(w.Spec.Labels) {
			workloadEntries = append(workloadEntries, w)
		}
	}

	return workloadEntries
}

func (c *Controller) extractWorkloadEntry(w *apiv1alpha3.WorkloadEntry) *model.WorkloadInfo {
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
		if waypoint, found = c.ambientIndex.waypoints[model.WaypointScope{Namespace: w.Namespace, ServiceAccount: w.Spec.ServiceAccount}]; !found {
			// if there are none, check namespace wide waypoints
			waypoint = c.ambientIndex.waypoints[model.WaypointScope{Namespace: w.Namespace}]
		}
	}
	policies := c.selectorAuthorizationPolicies(w.Namespace, w.Labels)
	wl := c.constructWorkloadFromWorkloadEntry(w, waypoint, policies)
	if wl == nil {
		return nil
	}
	return &model.WorkloadInfo{
		Workload: wl,
		Labels:   w.Labels,
	}
}

// handleWorkloadEntry handles a WorkloadEntry event. Returned is XDS events to trigger, if any.
func (c *Controller) handleWorkloadEntry(idx *AmbientIndex, oldWorkloadEntry, w *apiv1alpha3.WorkloadEntry, isDelete bool) sets.Set[model.ConfigKey] {
	if idx == nil {
		return nil
	}

	if oldWorkloadEntry != nil {
		// compare only labels and annotations, which are what we care about
		if maps.Equal(oldWorkloadEntry.Labels, w.Labels) &&
			maps.Equal(oldWorkloadEntry.Annotations, w.Annotations) {
			return nil
		}
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	updates := sets.New[model.ConfigKey]()

	var wl *model.WorkloadInfo
	if !isDelete {
		wl = c.extractWorkloadEntry(w)
	}

	wlNetwork := c.Network(w.Spec.Address, w.Spec.Labels).String()
	networkAddr := networkAddress{network: wlNetwork, ip: w.Spec.Address}
	uid := c.generateWorkloadEntryUID(w.GetNamespace(), w.GetName())
	oldWl := idx.byUID[uid]
	if wl == nil {
		// This is an explicit delete event, or there is no longer a Workload to create (VM NotReady, etc)
		delete(idx.byWorkloadEntry, networkAddr)
		delete(idx.byUID, uid)
		if oldWl != nil {
			// If we already knew about this workload, we need to make sure we drop all VIP references as well
			for vip := range oldWl.VirtualIps {
				idx.dropWorkloadFromService(networkAddress{network: oldWl.Network, ip: vip}, oldWl.ResourceName())
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
	idx.byWorkloadEntry[networkAddr] = wl
	idx.byUID[uid] = wl
	if oldWl != nil {
		// For updates, we will drop the VIPs and then add the new ones back. This could be optimized
		for vip := range oldWl.VirtualIps {
			idx.dropWorkloadFromService(networkAddress{network: oldWl.Network, ip: vip}, wl.ResourceName())
		}
	}
	// Update the VIP indexes as well, as needed
	for vip := range wl.VirtualIps {
		idx.insertWorkloadToService(networkAddress{network: wl.Network, ip: vip}, wl)
	}

	log.Debugf("%v: workload updated, pushing", wl.ResourceName())
	updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
	return updates
}

func (c *Controller) constructWorkloadFromWorkloadEntry(workloadEntry *apiv1alpha3.WorkloadEntry,
	waypoint *workloadapi.GatewayAddress, policies []string,
) *workloadapi.Workload {
	if workloadEntry == nil {
		return nil
	}

	// TODO support WorkloadEntry's with empty address
	// only add if waypoint exists?
	if workloadEntry.Spec.Address == "" {
		log.Warnf("workloadentry %s/%s does not have an address", workloadEntry.Namespace, workloadEntry.Name)
		return nil
	}

	vips := map[string]*workloadapi.PortList{}
	if services := getWorkloadEntryServices(c.services.List(workloadEntry.Namespace, klabels.Everything()), workloadEntry); len(services) > 0 {
		for _, svc := range services {
			for _, vip := range getVIPs(svc) {
				if vips[vip] == nil {
					vips[vip] = &workloadapi.PortList{}
				}
				for _, port := range svc.Spec.Ports {
					if port.Protocol != v1.ProtocolTCP {
						continue
					}
					targetPort, err := findPortForWorkloadEntry(workloadEntry, &port)
					if err != nil {
						log.Errorf("error looking up port for WorkloadEntry %s/%s", workloadEntry.Namespace, workloadEntry.Name)
						continue
					}
					vips[vip].Ports = append(vips[vip].Ports, &workloadapi.Port{
						ServicePort: uint32(port.Port),
						TargetPort:  targetPort,
					})
				}
			}
		}
	}

	network := c.Network(workloadEntry.Spec.Address, workloadEntry.Spec.Labels).String()
	if workloadEntry.Spec.Network != "" {
		network = workloadEntry.Spec.Network
	}

	wl := &workloadapi.Workload{
		Uid:                   c.generateWorkloadEntryUID(workloadEntry.Namespace, workloadEntry.Name),
		Name:                  workloadEntry.Name,
		Namespace:             workloadEntry.Namespace,
		Addresses:             [][]byte{parseIP(workloadEntry.Spec.Address)},
		Network:               network,
		ServiceAccount:        workloadEntry.Spec.ServiceAccount,
		VirtualIps:            vips,
		AuthorizationPolicies: policies,
		Waypoint:              waypoint,
	}
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
		wl.TrustDomain = td
	}

	wl.WorkloadName, wl.WorkloadType = workloadEntry.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
	wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(workloadEntry.Labels, wl.WorkloadName)

	if workloadEntry.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
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
func (a *AmbientIndex) updateWaypointForWorkload(byWorkload map[networkAddress]*model.WorkloadInfo, scope model.WaypointScope,
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

func getWorkloadEntryServices(services []*v1.Service, workloadEntry *apiv1alpha3.WorkloadEntry) []*v1.Service {
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

func findPortForWorkloadEntry(workloadEntry *apiv1alpha3.WorkloadEntry, svcPort *v1.ServicePort) (uint32, error) {
	if workloadEntry == nil {
		return 0, fmt.Errorf("invalid input, got nil WorkloadEntry")
	}
	if svcPort == nil {
		return 0, fmt.Errorf("invalid input, got nil ServicePort")
	}

	for portName, portVal := range workloadEntry.Spec.Ports {
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
	allWorkloadEntries := c.getControllerWorkloadEntries(svc.GetNamespace())
	if svc.Spec.Selector == nil {
		// services with nil selectors match nothing, not everything.
		return nil
	}
	var workloadEntries []*apiv1alpha3.WorkloadEntry
	for _, wl := range allWorkloadEntries {
		if labels.Instance(svc.Spec.Selector).SubsetOf(wl.Spec.Labels) {
			workloadEntries = append(workloadEntries, wl)
		}
	}
	return workloadEntries
}

// name format: <cluster>/<group>/<kind>/<namespace>/<name></section-name>
// if the WorkloadEntry is inlined in the ServiceEntry, we may need section name. caller should use generateServiceEntryUID
func (c *Controller) generateWorkloadEntryUID(wkEntryNamespace, wkEntryName string) string {
	return c.clusterID.String() + "/networking.istio.io/WorkloadEntry/" + wkEntryNamespace + "/" + wkEntryName
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
