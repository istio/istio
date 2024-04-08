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
	"net/netip"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	v1 "k8s.io/api/core/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/networking/v1alpha3"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

type AmbientIndex interface {
	Lookup(key string) []*model.AddressInfo
	All() []*model.AddressInfo
	WorkloadsForWaypoint(scope model.WaypointScope) []*model.WorkloadInfo
	Waypoint(scope model.WaypointScope) []netip.Addr
	CalculateUpdatedWorkloads(pods map[string]*v1.Pod,
		workloadEntries map[networkAddress]*apiv1alpha3.WorkloadEntry,
		seEndpoints map[*apiv1alpha3.ServiceEntry]sets.Set[*v1alpha3.WorkloadEntry],
		c *Controller) map[model.ConfigKey]struct{}
	HandleSelectedNamespace(ns string, pods []*v1.Pod, services []*v1.Service, c *Controller)
}

// AmbientIndexImpl maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndexImpl struct {
	mu sync.RWMutex
	// byService indexes by Service namespaced hostname. A given Service can map to
	// many workloads associated, indexed by workload uid.
	byService map[string]map[string]*model.WorkloadInfo
	// byPod indexes by network/podIP address.
	// NOTE: prefer byUID to iterate over all workloads.
	byPod map[networkAddress]*model.WorkloadInfo
	// byWorkloadEntry indexes by WorkloadEntry IP address.
	// NOTE: avoid using this index for anything other than Lookup().
	// this map is incomplete and lacks workloads without an address
	// (i.e. multi network workloads proxying remote service)
	byWorkloadEntry map[networkAddress]*model.WorkloadInfo
	// byUID indexes all workloads by their uid
	byUID map[string]*model.WorkloadInfo
	// serviceByAddr are indexed by the network/clusterIP
	serviceByAddr map[networkAddress]*model.ServiceInfo
	// serviceByNamespacedHostname are indexed by the namespace/hostname
	serviceByNamespacedHostname map[string]*model.ServiceInfo
	// TODO(nmittler): Add serviceByHostname to support on-demand for DNS.

	// Map of Scope -> address
	waypoints map[model.WaypointScope]*workloadapi.GatewayAddress

	// map of service entry name/namespace to the service entry.
	// used on pod updates to add VIPs to pods from service entries.
	// also used on service entry updates to cleanup any old VIPs from pods/workloads maps.
	servicesMap map[types.NamespacedName]*apiv1alpha3.ServiceEntry
}

func workloadToAddressInfo(w *workloadapi.Workload) *model.AddressInfo {
	return &model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: w,
			},
		},
	}
}

func serviceToAddressInfo(s *workloadapi.Service) *model.AddressInfo {
	return &model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Service{
				Service: s,
			},
		},
	}
}

// name format: <cluster>/<group>/<kind>/<namespace>/<name></section-name>
func (c *Controller) generatePodUID(p *v1.Pod) string {
	return c.clusterID.String() + "//" + "Pod/" + p.Namespace + "/" + p.Name
}

// Lookup finds the list of AddressInfos for a given key.
// network/IP -> return associated pod Workload or the Service and its corresponding Workloads
// namespace/hostname -> return the Service and its corresponding Workloads
//
// NOTE: As an interface method of AmbientIndex, this locks the index.
func (a *AmbientIndexImpl) Lookup(key string) []*model.AddressInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// uid is primary key, attempt lookup first
	if wl, f := a.byUID[key]; f {
		return []*model.AddressInfo{workloadToAddressInfo(wl.Workload)}
	}

	network, ip, found := strings.Cut(key, "/")
	if !found {
		log.Warnf(`key (%v) did not contain the expected "/" character`, key)
		return nil
	}
	res := make([]*model.AddressInfo, 0)
	if _, err := netip.ParseAddr(ip); err != nil {
		// this must be namespace/hostname format
		// lookup Service and any Workloads for that Service for each of the network addresses
		if svc, f := a.serviceByNamespacedHostname[key]; f {
			res = append(res, serviceToAddressInfo(svc.Service))

			for _, wl := range a.byService[key] {
				res = append(res, workloadToAddressInfo(wl.Workload))
			}
		}
		return res
	}

	networkAddr := networkAddress{network: network, ip: ip}
	// First look at pod...
	if p, f := a.byPod[networkAddr]; f {
		return []*model.AddressInfo{workloadToAddressInfo(p.Workload)}
	}
	// Next, look at WorkloadEntries
	if w, f := a.byWorkloadEntry[networkAddr]; f {
		return []*model.AddressInfo{workloadToAddressInfo(w.Workload)}
	}
	// Fallback to service. Note: these IP ranges should be non-overlapping
	// When a Service lookup is performed, but it and its workloads are returned
	if s, exists := a.serviceByAddr[networkAddr]; exists {
		res = append(res, serviceToAddressInfo(s.Service))
		for _, wl := range a.byService[s.ResourceName()] {
			res = append(res, workloadToAddressInfo(wl.Workload))
		}
	}

	return res
}

func (a *AmbientIndexImpl) dropWorkloadFromService(namespacedHostname string, workloadUID string) {
	wls := a.byService[namespacedHostname]
	delete(wls, workloadUID)
}

func (a *AmbientIndexImpl) insertWorkloadToService(namespacedHostname string, workload *model.WorkloadInfo) {
	if _, ok := a.byService[namespacedHostname]; !ok {
		a.byService[namespacedHostname] = map[string]*model.WorkloadInfo{}
	}
	a.byService[namespacedHostname][workload.Uid] = workload
}

func (a *AmbientIndexImpl) updateWaypoint(sa model.WaypointScope, addr *workloadapi.GatewayAddress, isDelete bool) map[model.ConfigKey]struct{} {
	updates := sets.New[model.ConfigKey]()
	a.updateWaypointForWorkload(a.byUID, sa, addr, isDelete, updates)
	return updates
}

// All return all known addresses. Result is un-ordered
//
// NOTE: As an interface method of AmbientIndex, this locks the index.
func (a *AmbientIndexImpl) All() []*model.AddressInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := make([]*model.AddressInfo, 0, len(a.byUID)+len(a.serviceByNamespacedHostname))

	for _, wl := range a.byUID {
		netAddrs := networkAddressFromWorkload(wl)

		// Workload without an (optional) address is possible and should still be included
		if len(netAddrs) == 0 {
			res = append(res, workloadToAddressInfo(wl.Workload))
			continue
		}

		// We need to determine whether we encounter, while iterating over the Workloads, a WorkloadEntry that has
		// a network address similar to another Workload (Pod).
		// In such cases we don't include the WorkloadEntry in the result and warn the users about it.

		// WorkloadEntry will have a single network address
		netAddr := netAddrs[0]
		p := a.byPod[netAddr]
		we := a.byWorkloadEntry[netAddr]
		if p != nil && we != nil && wl.GetUid() != p.GetUid() {
			log.Warnf("Skipping WorkloadEntry %s as in Ambient it can't have the same address of another workload on the same network", wl.GetName())
			continue
		}

		res = append(res, workloadToAddressInfo(wl.Workload))
	}

	for _, s := range a.serviceByNamespacedHostname {
		res = append(res, serviceToAddressInfo(s.Service))
	}
	return res
}

// WorkloadsForWaypoint returns all workload information matching the scope.
//
// NOTE: As an interface method of AmbientIndex, this locks the index.
func (a *AmbientIndexImpl) WorkloadsForWaypoint(scope model.WaypointScope) []*model.WorkloadInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var res []*model.WorkloadInfo
	// TODO: try to precompute
	workloads := maps.Values(a.byUID)
	workloads = model.SortWorkloadsByCreationTime(workloads)
	for _, w := range workloads {
		if a.matchesScope(scope, w) {
			res = append(res, w)
		}
	}
	return res
}

func (c *Controller) WorkloadsForWaypoint(scope model.WaypointScope) []*model.WorkloadInfo {
	return c.ambientIndex.WorkloadsForWaypoint(scope)
}

// Waypoint returns the addresses of the waypoints matching the scope.
//
// NOTE: As an interface method of AmbientIndex, this locks the index.
func (a *AmbientIndexImpl) Waypoint(scope model.WaypointScope) []netip.Addr {
	a.mu.RLock()
	defer a.mu.RUnlock()
	// TODO need to handle case where waypoints are dualstack/have multiple addresses
	if addr, f := a.waypoints[scope]; f {
		switch address := addr.Destination.(type) {
		case *workloadapi.GatewayAddress_Address:
			if ip, ok := netip.AddrFromSlice(address.Address.GetAddress()); ok {
				return []netip.Addr{ip}
			}
		case *workloadapi.GatewayAddress_Hostname:
			// TODO
		}
	}

	// Now look for namespace-wide
	scope.ServiceAccount = ""
	if addr, f := a.waypoints[scope]; f {
		switch address := addr.Destination.(type) {
		case *workloadapi.GatewayAddress_Address:
			if ip, ok := netip.AddrFromSlice(address.Address.GetAddress()); ok {
				return []netip.Addr{ip}
			}
		case *workloadapi.GatewayAddress_Hostname:
			// TODO
		}
	}

	return nil
}

// Waypoint finds all waypoint IP addresses for a given scope.  Performs first a Namespace+ServiceAccount
// then falls back to any Namespace wide waypoints
func (c *Controller) Waypoint(scope model.WaypointScope) []netip.Addr {
	return c.ambientIndex.Waypoint(scope)
}

func (a *AmbientIndexImpl) matchesScope(scope model.WaypointScope, w *model.WorkloadInfo) bool {
	if w.Namespace != scope.Namespace {
		return false
	}
	// Filter out waypoints.
	if w.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		return false
	}
	if len(scope.ServiceAccount) == 0 {
		// We are a namespace wide waypoint. SA scope take precedence.
		// Check if there is one for this workloads service account
		if _, f := a.waypoints[model.WaypointScope{Namespace: scope.Namespace, ServiceAccount: w.ServiceAccount}]; f {
			return false
		}
		return true
	}
	return w.ServiceAccount == scope.ServiceAccount
}

func (c *Controller) constructService(svc *v1.Service) *model.ServiceInfo {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	vips := getVIPs(svc)
	addrs := make([]*workloadapi.NetworkAddress, 0, len(vips))
	for _, vip := range vips {
		addrs = append(addrs, &workloadapi.NetworkAddress{
			Network: c.Network(vip, make(labels.Instance, 0)).String(),
			Address: netip.MustParseAddr(vip).AsSlice(),
		})
	}

	return &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Hostname:  c.hostname(svc),
			Addresses: addrs,
			Ports:     ports,
		},
	}
}

func (c *Controller) hostname(svc *v1.Service) string {
	return string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix))
}

func (c *Controller) namespacedHostname(svc *v1.Service) string {
	return namespacedHostname(svc.Namespace, c.hostname(svc))
}

func namespacedHostname(namespace, hostname string) string {
	return namespace + "/" + hostname
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) extractWorkload(p *v1.Pod, c *Controller) *model.WorkloadInfo {
	// If the pod is pending, but has an IP, it should be a valid workload and we should extract it.
	// An example of this is a pod having an initContainer.
	// See https://github.com/istio/istio/issues/48854
	if p == nil || (!IsPodRunning(p) && !IsPodPending(p)) || p.Spec.HostNetwork {
		return nil
	}
	var waypoint *workloadapi.GatewayAddress
	if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		// Waypoints do not have waypoints
	} else {
		// First check for a waypoint for our SA explicit
		found := false
		if waypoint, found = a.waypoints[model.WaypointScope{Namespace: p.Namespace, ServiceAccount: p.Spec.ServiceAccountName}]; !found {
			// if there are none, check namespace wide waypoints
			waypoint = a.waypoints[model.WaypointScope{Namespace: p.Namespace}]
		}
	}

	policies := c.selectorAuthorizationPolicies(p.Namespace, p.Labels)
	policies = append(policies, c.convertedSelectorPeerAuthentications(p.Namespace, p.Labels)...)
	wl := a.constructWorkload(p, waypoint, policies, c)
	if wl == nil {
		return nil
	}
	return &model.WorkloadInfo{
		Workload:     wl,
		Labels:       p.Labels,
		Source:       model.WorkloadSourcePod,
		CreationTime: p.CreationTimestamp.Time,
	}
}

func (c *Controller) setupIndex() *AmbientIndexImpl {
	idx := AmbientIndexImpl{
		byService:                   map[string]map[string]*model.WorkloadInfo{},
		byPod:                       map[networkAddress]*model.WorkloadInfo{},
		byWorkloadEntry:             map[networkAddress]*model.WorkloadInfo{},
		byUID:                       map[string]*model.WorkloadInfo{},
		waypoints:                   map[model.WaypointScope]*workloadapi.GatewayAddress{},
		serviceByAddr:               map[networkAddress]*model.ServiceInfo{},
		serviceByNamespacedHostname: map[string]*model.ServiceInfo{},
		servicesMap:                 map[types.NamespacedName]*apiv1alpha3.ServiceEntry{},
	}

	podHandler := func(old, pod *v1.Pod, ev model.Event) error {
		log.Debugf("ambient podHandler pod %s/%s, event %v", pod.Namespace, pod.Name, ev)
		idx.mu.Lock()
		defer idx.mu.Unlock()
		updates := idx.handlePod(old, pod, ev, c)
		if len(updates) > 0 {
			c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
				ConfigsUpdated: updates,
				Reason:         model.NewReasonStats(model.AmbientUpdate),
			})
		}
		return nil
	}

	registerHandlers[*v1.Pod](c, c.podsClient, "", podHandler, nil)

	// We only handle WLE and SE from config cluster, otherwise we could get duplicate workload from remote clusters.
	if c.configCluster {
		// Handle WorkloadEntries.
		c.configController.RegisterEventHandler(gvk.WorkloadEntry, func(oldCfg config.Config, newCfg config.Config, ev model.Event) {
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

			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleWorkloadEntry(oldWkEntry, newWkEntry, ev == model.EventDelete, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           false,
					ConfigsUpdated: updates,
					Reason:         model.NewReasonStats(model.AmbientUpdate),
				})
			}
		})

		// Handle ServiceEntries.
		c.configController.RegisterEventHandler(gvk.ServiceEntry, func(_ config.Config, newCfg config.Config, ev model.Event) {
			newSvcEntrySpec := serviceentry.ConvertServiceEntry(newCfg)
			var newSvcEntry *apiv1alpha3.ServiceEntry
			if newSvcEntrySpec != nil {
				newSvcEntry = &apiv1alpha3.ServiceEntry{
					ObjectMeta: newCfg.ToObjectMeta(),
					Spec:       *newSvcEntrySpec.DeepCopy(),
				}
			}

			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleServiceEntry(newSvcEntry, ev, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					Full:           false,
					ConfigsUpdated: updates,
					Reason:         model.NewReasonStats(model.AmbientUpdate),
				})
			}
		})
	}

	c.configController.RegisterEventHandler(gvk.AuthorizationPolicy, c.AuthorizationPolicyHandler)
	c.configController.RegisterEventHandler(gvk.PeerAuthentication, c.PeerAuthenticationHandler)

	serviceHandler := func(old, svc *v1.Service, ev model.Event) error {
		log.Debugf("ambient serviceHandler service %s/%s, event %v", svc.Namespace, svc.Name, ev)
		var updates sets.Set[model.ConfigKey]
		idx.mu.Lock()
		defer idx.mu.Unlock()
		switch ev {
		case model.EventAdd:
			updates = idx.handleService(svc, ev, c)
		case model.EventUpdate:
			// TODO(hzxuzhonghu): handle svc update within `handleService`, so that we donot need to check event type here.
			updates = idx.handleService(old, model.EventDelete, c)
			updates2 := idx.handleService(svc, ev, c)
			if updates == nil {
				updates = updates2
			} else {
				updates.Union(updates2)
			}
		case model.EventDelete:
			updates = idx.handleService(svc, ev, c)
		}
		if len(updates) > 0 {
			c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
				ConfigsUpdated: updates,
				Reason:         model.NewReasonStats(model.AmbientUpdate),
			})
		}
		return nil
	}

	registerHandlers[*v1.Service](c, c.services, "", serviceHandler, nil)

	kubeGatewayHandler := func(old, newGateway *k8sbeta.Gateway, ev model.Event) error {
		log.Debugf("ambient kubeGatewayHandler gateway %s/%s, event %v", newGateway.Namespace, newGateway.Name, ev)
		idx.handleKubeGateway(old, newGateway, ev, c)
		return nil
	}

	// initNetworkManager initializes the gatewayResourceClient, it should not be re-initialized in setupIndex
	registerHandlers[*k8sbeta.Gateway](c, c.gatewayResourceClient, "", kubeGatewayHandler, nil)

	return &idx
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) handlePod(old, p *v1.Pod, ev model.Event, c *Controller) sets.Set[model.ConfigKey] {
	if old != nil {
		// compare only labels and pod phase, which are what we care about
		if maps.Equal(old.Labels, p.Labels) &&
			maps.Equal(old.Annotations, p.Annotations) &&
			old.Status.Phase == p.Status.Phase &&
			IsPodReady(old) == IsPodReady(p) {
			return nil
		}
	}

	updates := sets.New[model.ConfigKey]()

	var wl *model.WorkloadInfo
	if ev != model.EventDelete {
		wl = a.extractWorkload(p, c)
	}
	uid := c.generatePodUID(p)
	oldWl := a.byUID[uid]
	if wl == nil {
		a.updateWorkloadIndexes(oldWl, nil, updates)
		return updates
	}
	if oldWl != nil && proto.Equal(wl.Workload, oldWl.Workload) {
		log.Debugf("%v: no change, skipping", wl.ResourceName())
		return updates
	}
	a.updateWorkloadIndexes(oldWl, wl, updates)

	return updates
}

// updateWorkloadIndexes, given and old and new instance, updates the various indexes for workloads.
// Any changes are reported in `updates`.
func (a *AmbientIndexImpl) updateWorkloadIndexes(oldWl *model.WorkloadInfo, newWl *model.WorkloadInfo, updates sets.Set[model.ConfigKey]) {
	if newWl == nil {
		if oldWl == nil {
			// No change needed
			return
		}
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: oldWl.ResourceName()})
		for _, addr := range networkAddressFromWorkload(oldWl) {
			if oldWl.Source == model.WorkloadSourcePod {
				delete(a.byPod, addr)
			} else {
				delete(a.byWorkloadEntry, addr)
			}
		}
		delete(a.byUID, oldWl.Uid)
		// If we already knew about this workload, we need to make sure we drop all service references as well
		for namespacedHostname := range oldWl.Services {
			a.dropWorkloadFromService(namespacedHostname, oldWl.ResourceName())
		}
		return
	}
	updates.Insert(model.ConfigKey{Kind: kind.Address, Name: newWl.ResourceName()})
	for _, networkAddr := range networkAddressFromWorkload(newWl) {
		if newWl.Source == model.WorkloadSourcePod {
			a.byPod[networkAddr] = newWl
		} else {
			a.byWorkloadEntry[networkAddr] = newWl
		}
	}
	a.byUID[newWl.Uid] = newWl
	if oldWl != nil {
		// For updates, we will drop the service and then add the new ones back. This could be optimized
		for namespacedHostname := range oldWl.Services {
			a.dropWorkloadFromService(namespacedHostname, oldWl.ResourceName())
		}
	}
	// Update the service indexes as well, as needed
	for namespacedHostname := range newWl.Services {
		a.insertWorkloadToService(namespacedHostname, newWl)
	}
}

func networkAddressFromWorkload(wl *model.WorkloadInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(wl.Addresses))
	for _, addr := range wl.Addresses {
		ip, _ := netip.AddrFromSlice(addr)
		networkAddrs = append(networkAddrs, networkAddress{network: wl.Network, ip: ip.String()})
	}
	return networkAddrs
}

func toInternalNetworkAddresses(nwAddrs []*workloadapi.NetworkAddress) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(nwAddrs))
	for _, addr := range nwAddrs {
		if ip, ok := netip.AddrFromSlice(addr.Address); ok {
			networkAddrs = append(networkAddrs, networkAddress{
				ip:      ip.String(),
				network: addr.Network,
			})
		}
	}
	return networkAddrs
}

// NOTE: Mutex is locked prior to being called.
func (a *AmbientIndexImpl) handleService(svc *v1.Service, ev model.Event, c *Controller) sets.Set[model.ConfigKey] {
	updates := sets.New[model.ConfigKey]()

	si := c.constructService(svc)
	networkAddrs := toInternalNetworkAddresses(si.GetAddresses())
	pods := c.getPodsInService(svc)
	for _, p := range pods {
		// Can be nil if it's not ready, hostNetwork, etc
		uid := c.generatePodUID(p)
		oldWl := a.byUID[uid]
		wl := a.extractWorkload(p, c)
		a.updateWorkloadIndexes(oldWl, wl, updates)
	}

	if features.EnableK8SServiceSelectWorkloadEntries {
		workloadEntries := c.getSelectedWorkloadEntries(svc.GetNamespace(), svc.Spec.Selector)
		for _, w := range workloadEntries {
			uid := c.generateWorkloadEntryUID(w.Namespace, w.Name)
			oldWl := a.byUID[uid]
			wl := a.extractWorkloadEntry(w, c)
			a.updateWorkloadIndexes(oldWl, wl, updates)
		}
	}
	namespacedName := si.ResourceName()
	if ev == model.EventDelete {
		for _, networkAddr := range networkAddrs {
			delete(a.serviceByAddr, networkAddr)
		}
		delete(a.serviceByNamespacedHostname, si.ResourceName())
		// Cleanup byService fully here. We don't use DeleteCleanupLast so we can distinguish between an empty service and missing service.
		delete(a.byService, namespacedName)
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: namespacedName})
	} else {
		for _, networkAddr := range networkAddrs {
			a.serviceByAddr[networkAddr] = si
		}
		a.serviceByNamespacedHostname[namespacedName] = si
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: namespacedName})
	}

	return updates
}

func (a *AmbientIndexImpl) handleKubeGateway(_, gateway *k8sbeta.Gateway, event model.Event, c *Controller) {
	// gateway.Status.Addresses should only be populated once the Waypoint's deployment has at least 1 ready pod, it should never be removed after going ready
	// ignore Kubernetes Gateways which aren't waypoints
	// TODO: should this be WaypointGatewayClass or matches a label?
	if gateway.Spec.GatewayClassName == constants.WaypointGatewayClassName && len(gateway.Status.Addresses) > 0 {
		scope := model.WaypointScope{Namespace: gateway.Namespace, ServiceAccount: gateway.Annotations[constants.WaypointServiceAccount]}

		waypointPort := uint32(15008)
		for _, l := range gateway.Spec.Listeners {
			if l.Protocol == k8sbeta.ProtocolType(protocol.HBONE) {
				waypointPort = uint32(l.Port)
			}
		}

		ip, err := netip.ParseAddr(gateway.Status.Addresses[0].Value)
		if err != nil {
			// This should be a transient error when upgrading, when the Kube Gateway status is updated it should write an IP address
			log.Errorf("Unable to parse IP address in status of %v/%v/%v", gvk.KubernetesGateway, gateway.Namespace, gateway.Name)
			return
		}
		addr := &workloadapi.GatewayAddress{
			Destination: &workloadapi.GatewayAddress_Address{
				Address: &workloadapi.NetworkAddress{
					Network: c.Network(ip.String(), make(labels.Instance, 0)).String(),
					Address: ip.AsSlice(),
				},
			},
			HboneMtlsPort: waypointPort,
		}

		updates := sets.New[model.ConfigKey]()
		a.mu.Lock()
		defer a.mu.Unlock()
		if event == model.EventDelete {
			delete(a.waypoints, scope)
			updates.Merge(a.updateWaypoint(scope, addr, true))
		} else if !proto.Equal(a.waypoints[scope], addr) {
			a.waypoints[scope] = addr
			updates.Merge(a.updateWaypoint(scope, addr, false))
		}

		if len(updates) > 0 {
			log.Debug("Waypoint ready: Pushing Updates")
			c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
				ConfigsUpdated: updates,
				Reason:         model.NewReasonStats(model.AmbientUpdate),
			})
		}
	}
}

func (c *Controller) getPodsInService(svc *v1.Service) []*v1.Pod {
	if svc.Spec.Selector == nil {
		// services with nil selectors match nothing, not everything.
		return nil
	}
	return c.podsClient.List(svc.Namespace, klabels.ValidatedSetSelector(svc.Spec.Selector))
}

// AddressInformation returns all AddressInfo's in the cluster.
// This may be scoped to specific subsets by specifying a non-empty addresses field
func (c *Controller) AddressInformation(addresses sets.String) ([]*model.AddressInfo, sets.String) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var wls []*model.AddressInfo
	removed := sets.New[string]()
	got := sets.String{}
	for addr := range addresses {
		wl := c.ambientIndex.Lookup(addr)
		if len(wl) == 0 {
			removed.Insert(addr)
		} else {
			for _, addr := range wl {
				if got.Contains(addr.ResourceName()) {
					continue
				}
				got.Insert(addr.ResourceName())
				wls = append(wls, addr)
			}
		}
	}
	return wls, removed
}

func (a *AmbientIndexImpl) constructWorkload(pod *v1.Pod, waypoint *workloadapi.GatewayAddress, policies []string,
	c *Controller,
) *workloadapi.Workload {
	workloadServices := map[string]*workloadapi.PortList{}
	allServices := c.services.List(pod.Namespace, klabels.Everything())
	if services := getPodServices(allServices, pod); len(services) > 0 {
		for _, svc := range services {
			// Build the ports for the service.
			ports := &workloadapi.PortList{}
			for _, port := range svc.Spec.Ports {
				if port.Protocol != v1.ProtocolTCP {
					continue
				}
				targetPort, err := FindPort(pod, &port)
				if err != nil {
					log.Debug(err)
					continue
				}
				ports.Ports = append(ports.Ports, &workloadapi.Port{
					ServicePort: uint32(port.Port),
					TargetPort:  uint32(targetPort),
				})
			}

			workloadServices[c.namespacedHostname(svc)] = ports
		}
	}

	addresses := make([][]byte, 0, len(pod.Status.PodIPs))
	for _, podIP := range pod.Status.PodIPs {
		addresses = append(addresses, parseIP(podIP.IP))
	}
	for nsName, ports := range a.getWorkloadServicesFromServiceEntries(nil, pod.GetNamespace(), pod.Labels) {
		workloadServices[nsName] = ports
	}

	wl := &workloadapi.Workload{
		Uid:                   c.generatePodUID(pod),
		Name:                  pod.Name,
		Addresses:             addresses,
		Hostname:              pod.Spec.Hostname,
		Network:               c.Network(pod.Status.PodIP, pod.Labels).String(),
		Namespace:             pod.Namespace,
		ServiceAccount:        pod.Spec.ServiceAccountName,
		Node:                  pod.Spec.NodeName,
		Services:              workloadServices,
		AuthorizationPolicies: policies,
		Status:                workloadapi.WorkloadStatus_HEALTHY,
		ClusterId:             c.Cluster().String(),
		Waypoint:              waypoint,
	}
	if !IsPodReady(pod) {
		wl.Status = workloadapi.WorkloadStatus_UNHEALTHY
	}
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
		wl.TrustDomain = td
	}

	wl.WorkloadName, wl.WorkloadType = workloadNameAndType(pod)
	wl.CanonicalName, wl.CanonicalRevision = kubelabels.CanonicalService(pod.Labels, wl.WorkloadName)

	if pod.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
		// Configured for override
		wl.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
	}
	// Otherwise supports tunnel directly
	if model.SupportsTunnel(pod.Labels, model.TunnelHTTP) {
		wl.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		wl.NativeTunnel = true
	}
	return wl
}

func parseIP(ip string) []byte {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return addr.AsSlice()
}

// internal object used for indexing in ambientindex maps
type networkAddress struct {
	network string
	ip      string
}

func (n *networkAddress) String() string {
	return n.network + "/" + n.ip
}

func getVIPs(svc *v1.Service) []string {
	res := make([]string, 0)
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		// IPs are strictly optional for loadbalancers - they may just have a hostname.
		if ing.IP != "" {
			res = append(res, ing.IP)
		}
	}
	return res
}

func (c *Controller) AdditionalPodSubscriptions(
	proxy *model.Proxy,
	allAddresses sets.String,
	currentSubs sets.String,
) sets.String {
	shouldSubscribe := sets.New[string]()

	// First, we want to handle VIP subscriptions. Example:
	// Client subscribes to VIP1. Pod1, part of VIP1, is sent.
	// The client wouldn't be explicitly subscribed to Pod1, so it would normally ignore it.
	// Since it is a part of VIP1 which we are subscribe to, add it to the subscriptions
	for addr := range allAddresses {
		for _, wl := range model.ExtractWorkloadsFromAddresses(c.ambientIndex.Lookup(addr)) {
			// We may have gotten an update for Pod, but are subscribed to a Service.
			// We need to force a subscription on the Pod as well
			for namespacedHostname := range wl.Services {
				if currentSubs.Contains(namespacedHostname) {
					shouldSubscribe.Insert(wl.ResourceName())
					break
				}
			}
		}
	}

	// Next, as an optimization, we will send all node-local endpoints
	if nodeName := proxy.Metadata.NodeName; nodeName != "" {
		for _, wl := range model.ExtractWorkloadsFromAddresses(c.ambientIndex.All()) {
			if wl.Node == nodeName {
				n := wl.ResourceName()
				if currentSubs.Contains(n) {
					continue
				}
				shouldSubscribe.Insert(n)
			}
		}
	}

	return shouldSubscribe
}

// syncAllWorkloadsForAmbient refreshes all ambient workloads.
func (c *Controller) syncAllWorkloadsForAmbient() {
	if c.ambientIndex != nil {
		var namespaces []string
		if c.opts.DiscoveryNamespacesFilter != nil {
			namespaces = c.opts.DiscoveryNamespacesFilter.GetMembers().UnsortedList()
		}
		for _, ns := range namespaces {
			pods := c.podsClient.List(ns, klabels.Everything())
			services := c.services.List(ns, klabels.Everything())
			c.ambientIndex.HandleSelectedNamespace(ns, pods, services, c)
		}
	}
}

func workloadNameAndType(pod *v1.Pod) (string, workloadapi.WorkloadType) {
	objMeta, typeMeta := kubeutil.GetDeployMetaFromPod(pod)
	switch typeMeta.Kind {
	case "Deployment":
		return objMeta.Name, workloadapi.WorkloadType_DEPLOYMENT
	case "Job":
		return objMeta.Name, workloadapi.WorkloadType_JOB
	case "CronJob":
		return objMeta.Name, workloadapi.WorkloadType_CRONJOB
	default:
		return pod.Name, workloadapi.WorkloadType_POD
	}
}
