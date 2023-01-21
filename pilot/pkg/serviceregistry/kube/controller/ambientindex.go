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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

// AmbientIndex maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndex struct {
	mu sync.RWMutex
	// byService indexes by network/Service (virtual) *IP address*. A given Service may have multiple IPs, thus
	// multiple entries in the map.
	// A given IP can map to many workloads associated, indexed by workload uid.
	byService map[networkAddress]map[string]*model.WorkloadInfo
	// byPod indexes by network/podIP address.
	byPod map[networkAddress]*model.WorkloadInfo
	// byWorkloadEntry indexes by WorkloadEntry IP address.
	byWorkloadEntry map[networkAddress]*model.WorkloadInfo
	// byUID indexes by workloads by their uid
	byUID map[string]*model.WorkloadInfo
	// serviceByAddr are indexed by the network/clusterIP
	serviceByAddr map[networkAddress]*model.ServiceInfo
	// serviceByHostname are indexed by the namespace/hostname
	serviceByHostname map[string]*model.ServiceInfo

	// Map of Scope -> address
	waypoints map[model.WaypointScope]*workloadapi.GatewayAddress

	// we handle service entry events internally instead of adding a service entry handler to the controller
	// because we need to support DNS auto allocation of IPs; calculating these IPs is an expensive operation
	// and already batched elsewhere in the repo. instead we just wait for those events and events propagated
	// from another service entry controller and act on those
	handleServiceEntry func(svc *model.Service, event model.Event)

	// map of service entry name/namespace to the derived service.
	// used on pod updates to add VIPs to pods from service entries.
	// also used on service entry updates to cleanup any old VIPs from pods/workloads maps.
	servicesMap map[types.NamespacedName]*model.Service
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
func (a *AmbientIndex) Lookup(key string) []*model.AddressInfo {
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
	res := []*model.AddressInfo{}
	if _, err := netip.ParseAddr(ip); err != nil {
		// this must be namespace/hostname format
		// lookup Service and any Workloads for that Service for each of the network addresses
		if svc, f := a.serviceByHostname[key]; f {
			res = append(res, serviceToAddressInfo(svc.Service))
			for _, addr := range svc.Addresses {
				ii, _ := netip.AddrFromSlice(addr.Address)
				networkAddr := networkAddress{network: addr.Network, ip: ii.String()}
				for _, wl := range a.byService[networkAddr] {
					res = append(res, workloadToAddressInfo(wl.Workload))
				}
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
	for _, wl := range a.byService[networkAddr] {
		res = append(res, workloadToAddressInfo(wl.Workload))
	}
	if s, exists := a.serviceByAddr[networkAddr]; exists {
		res = append(res, serviceToAddressInfo(s.Service))
	}

	return res
}

func (a *AmbientIndex) dropWorkloadFromService(svcAddress networkAddress, workloadUID string) {
	wls := a.byService[svcAddress]
	delete(wls, workloadUID)
}

func (a *AmbientIndex) insertWorkloadToService(svcAddress networkAddress, workload *model.WorkloadInfo) {
	if _, ok := a.byService[svcAddress]; !ok {
		a.byService[svcAddress] = map[string]*model.WorkloadInfo{}
	}
	a.byService[svcAddress][workload.Uid] = workload
}

func (a *AmbientIndex) updateWaypoint(sa model.WaypointScope, addr *workloadapi.GatewayAddress, isDelete bool) map[model.ConfigKey]struct{} {
	updates := sets.New[model.ConfigKey]()
	// Update Waypoints for Pods
	a.updateWaypointForWorkload(a.byPod, sa, addr, isDelete, updates)
	// Update Waypoints for WorkloadEntries
	a.updateWaypointForWorkload(a.byWorkloadEntry, sa, addr, isDelete, updates)
	return updates
}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndex) All() []*model.AddressInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := make([]*model.AddressInfo, 0, len(a.byPod)+len(a.serviceByAddr)+len(a.byWorkloadEntry))
	// byPod and byWorkloadEntry will not have any duplicates, so we can just iterate over that.
	for _, wl := range a.byPod {
		res = append(res, workloadToAddressInfo(wl.Workload))
	}
	for _, s := range a.serviceByAddr {
		res = append(res, serviceToAddressInfo(s.Service))
	}
	for _, wl := range a.byWorkloadEntry {
		res = append(res, workloadToAddressInfo(wl.Workload))
	}
	return res
}

func (c *Controller) WorkloadsForWaypoint(scope model.WaypointScope) []*model.WorkloadInfo {
	a := c.ambientIndex
	a.mu.RLock()
	defer a.mu.RUnlock()
	var res []*model.WorkloadInfo
	// TODO: try to precompute
	for _, w := range a.byPod {
		if a.matchesScope(scope, w) {
			res = append(res, w)
		}
	}
	for _, w := range a.byWorkloadEntry {
		if a.matchesScope(scope, w) {
			res = append(res, w)
		}
	}
	return res
}

// Waypoint finds all waypoint IP addresses for a given scope.  Performs first a Namespace+ServiceAccount
// then falls back to any Namespace wide waypoints
func (c *Controller) Waypoint(scope model.WaypointScope) []netip.Addr {
	a := c.ambientIndex
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

func (a *AmbientIndex) matchesScope(scope model.WaypointScope, w *model.WorkloadInfo) bool {
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
			Hostname:  string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)),
			Addresses: addrs,
			Ports:     ports,
		},
	}
}

func (c *Controller) extractWorkload(p *v1.Pod) *model.WorkloadInfo {
	if p == nil || !IsPodRunning(p) || p.Spec.HostNetwork {
		return nil
	}
	var waypoint *workloadapi.GatewayAddress
	if p.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		// Waypoints do not have waypoints
	} else {
		// First check for a waypoint for our SA explicit
		found := false
		if waypoint, found = c.ambientIndex.waypoints[model.WaypointScope{Namespace: p.Namespace, ServiceAccount: p.Spec.ServiceAccountName}]; !found {
			// if there are none, check namespace wide waypoints
			waypoint = c.ambientIndex.waypoints[model.WaypointScope{Namespace: p.Namespace}]
		}
	}

	policies := c.selectorAuthorizationPolicies(p.Namespace, p.Labels)
	policies = append(policies, c.convertedSelectorPeerAuthentications(p.Namespace, p.Labels)...)
	wl := c.constructWorkload(p, waypoint, policies)
	if wl == nil {
		return nil
	}
	return &model.WorkloadInfo{
		Workload: wl,
		Labels:   p.Labels,
	}
}

func (c *Controller) setupIndex() *AmbientIndex {
	idx := AmbientIndex{
		byService:         map[networkAddress]map[string]*model.WorkloadInfo{},
		byPod:             map[networkAddress]*model.WorkloadInfo{},
		byWorkloadEntry:   map[networkAddress]*model.WorkloadInfo{},
		byUID:             map[string]*model.WorkloadInfo{},
		waypoints:         map[model.WaypointScope]*workloadapi.GatewayAddress{},
		serviceByAddr:     map[networkAddress]*model.ServiceInfo{},
		serviceByHostname: map[string]*model.ServiceInfo{},
	}

	podHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			updates := idx.handlePod(nil, obj, false, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			updates := idx.handlePod(oldObj, newObj, false, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		DeleteFunc: func(obj any) {
			updates := idx.handlePod(nil, obj, true, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
	}

	c.podsClient.AddEventHandler(podHandler)
	c.initWorkloadEntryHandler(&idx)

	serviceHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleService(obj, false, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleService(oldObj, true, c)
			updates2 := idx.handleService(newObj, false, c)
			if updates == nil {
				updates = updates2
			} else {
				for k, v := range updates2 {
					updates[k] = v
				}
			}

			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
		DeleteFunc: func(obj any) {
			idx.mu.Lock()
			defer idx.mu.Unlock()
			updates := idx.handleService(obj, true, c)
			if len(updates) > 0 {
				c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
					ConfigsUpdated: updates,
					Reason:         []model.TriggerReason{model.AmbientUpdate},
				})
			}
		},
	}

	idx.servicesMap = make(map[types.NamespacedName]*model.Service)
	idx.handleServiceEntry = func(svc *model.Service, event model.Event) {
		idx.mu.Lock()
		defer idx.mu.Unlock()

		if svc.Attributes.ServiceEntry == nil {
			// event for e.g. kube svc; ignore
			return
		}

		// We will accrue updates as we update our internal state
		updates := sets.New[model.ConfigKey]()

		if event != model.EventAdd {
			c.cleanupOldServiceEntryVips(svc, updates)
		}

		serviceEntryNamespacedName := types.NamespacedName{
			Name:      svc.Attributes.ServiceEntryName,
			Namespace: svc.Attributes.ServiceEntryNamespace,
		}

		// Update indexes
		if event == model.EventDelete {
			// servicesMap is used when cleaning up old VIPs (e.g. `ServiceEntry.endpoints`) so we must
			// delete this after we clean up VIPs
			delete(idx.servicesMap, serviceEntryNamespacedName)
		} else {
			// servicesMap is used when constructing workloads so it must be up to date
			c.ambientIndex.servicesMap[serviceEntryNamespacedName] = svc
		}

		pods := c.podsClient.List(metav1.NamespaceAll, klabels.Everything())
		wls := make(map[string]*model.WorkloadInfo, len(pods))
		for _, pod := range pods {
			newWl := c.extractWorkload(pod)
			if newWl != nil {
				// Update the pod, since it now has new VIP info
				networkAddrs := networkAddressFromWorkload(newWl)
				for _, networkAddr := range networkAddrs {
					c.ambientIndex.byPod[networkAddr] = newWl
				}
				c.ambientIndex.byUID[c.generatePodUID(pod)] = newWl
				updates[model.ConfigKey{Kind: kind.Address, Name: newWl.ResourceName()}] = struct{}{}
				wls[newWl.Uid] = newWl
			}
		}

		workloadEntries := c.getAllControllerWorkloadEntries()
		for _, w := range workloadEntries {
			wl := c.extractWorkloadEntry(w)
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
					idx.byWorkloadEntry[networkAddr] = wl
				}
				idx.byUID[c.generateServiceEntryUID(svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntryName, w.Spec.GetAddress())] = wl
				updates[model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()}] = struct{}{}
				wls[wl.Uid] = wl
			}
		}

		for _, we := range svc.Attributes.ServiceEntry.Endpoints {
			wli := c.extractWorkloadEntrySpec(we, svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntryName, svc)
			if wli != nil && event != model.EventDelete {
				for _, networkAddr := range networkAddressFromWorkload(wli) {
					idx.byWorkloadEntry[networkAddr] = wli
				}
				idx.byUID[c.generateServiceEntryUID(svc.Attributes.ServiceEntryNamespace, svc.Attributes.ServiceEntryName, we.GetAddress())] = wli
				updates[model.ConfigKey{Kind: kind.Address, Name: wli.ResourceName()}] = struct{}{}
				wls[wli.Uid] = wli
			}
		}

		vips := getVIPsFromServiceEntry(svc, nil)
		var addrs []*workloadapi.NetworkAddress
		var allPorts []*workloadapi.Port
		for vip := range vips {
			addrs = append(addrs, &workloadapi.NetworkAddress{
				Network: c.network.String(),
				Address: parseIP(vip),
			})
		}
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

		networkAddrs := make([]networkAddress, 0, len(si.Addresses))
		for _, addr := range si.Addresses {
			if vip, ok := netip.AddrFromSlice(addr.Address); ok {
				networkAddrs = append(networkAddrs, networkAddress{
					ip:      vip.String(),
					network: addr.Network,
				})
			}
		}

		// We send an update for each *workload* IP address previously in the service; they may have changed
		for _, networkAddr := range networkAddrs {
			for _, wl := range idx.byService[networkAddr] {
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			}
		}
		// Update indexes
		if event == model.EventDelete {
			for _, networkAddr := range networkAddrs {
				delete(idx.byService, networkAddr)
				delete(idx.serviceByAddr, networkAddr)
			}
			delete(idx.serviceByHostname, si.ResourceName())
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
		} else {
			for _, networkAddr := range networkAddrs {
				idx.byService[networkAddr] = wls
				idx.serviceByAddr[networkAddr] = si
			}
			idx.serviceByHostname[si.ResourceName()] = si
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
		}
		// Fetch updates again, in case it changed from adding new workloads
		for _, networkAddr := range networkAddrs {
			for _, wl := range idx.byService[networkAddr] {
				updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
			}
		}

		if len(updates) > 0 {
			c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
				Full:           false,
				ConfigsUpdated: updates,
				Reason:         []model.TriggerReason{model.AmbientUpdate},
			})
		}
	}

	c.services.AddEventHandler(serviceHandler)
	return &idx
}

func (a *AmbientIndex) handlePod(oldObj, newObj any, isDelete bool, c *Controller) sets.Set[model.ConfigKey] {
	p := controllers.Extract[*v1.Pod](newObj)
	old := controllers.Extract[*v1.Pod](oldObj)
	if old != nil {
		// compare only labels and pod phase, which are what we care about
		if maps.Equal(old.Labels, p.Labels) &&
			maps.Equal(old.Annotations, p.Annotations) &&
			old.Status.Phase == p.Status.Phase &&
			IsPodReady(old) == IsPodReady(p) {
			return nil
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	updates := sets.New[model.ConfigKey]()

	var wl *model.WorkloadInfo
	if !isDelete {
		wl = c.extractWorkload(p)
	}
	wlNetwork := c.Network(p.Status.PodIP, p.Labels).String()
	networkAddr := networkAddress{network: wlNetwork, ip: p.Status.PodIP}
	uid := c.generatePodUID(p)
	oldWl := a.byUID[uid]
	if wl == nil {
		// This is an explicit delete event, or there is no longer a Workload to create (pod NotReady, etc)
		delete(a.byPod, networkAddr)
		delete(a.byUID, uid)
		if oldWl != nil {
			// If we already knew about this workload, we need to make sure we drop all VIP references as well
			for vip := range oldWl.VirtualIps {
				a.dropWorkloadFromService(networkAddress{network: oldWl.Network, ip: vip}, oldWl.ResourceName())
			}
			log.Debugf("%v: workload removed, pushing", p.Status.PodIP)
			// TODO: namespace for network?
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: oldWl.ResourceName()})
			return updates
		}
		// It was a 'delete' for a resource we didn't know yet, no need to send an event

		return updates
	}
	if oldWl != nil && proto.Equal(wl.Workload, oldWl.Workload) {
		log.Debugf("%v: no change, skipping", wl.ResourceName())

		return updates
	}
	for _, networkAddr := range networkAddressFromWorkload(wl) {
		a.byPod[networkAddr] = wl
	}
	a.byUID[wl.Uid] = wl
	if oldWl != nil {
		// For updates, we will drop the VIPs and then add the new ones back. This could be optimized
		for vip := range oldWl.VirtualIps {
			a.dropWorkloadFromService(networkAddress{network: oldWl.Network, ip: vip}, oldWl.ResourceName())
		}
	}
	// Update the VIP indexes as well, as needed
	for vip := range wl.VirtualIps {
		a.insertWorkloadToService(networkAddress{network: wl.Network, ip: vip}, wl)
	}

	log.Debugf("%v: workload updated, pushing", wl.ResourceName())
	updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})

	return updates
}

func networkAddressFromWorkload(wl *model.WorkloadInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(wl.Addresses))
	for _, addr := range wl.Addresses {
		ip, _ := netip.AddrFromSlice(addr)
		networkAddrs = append(networkAddrs, networkAddress{network: wl.Network, ip: ip.String()})
	}
	return networkAddrs
}

func (a *AmbientIndex) handlePods(pods []*v1.Pod, c *Controller) {
	updates := sets.New[model.ConfigKey]()
	for _, p := range pods {
		updates = updates.Merge(a.handlePod(nil, p, false, c))
	}
	if len(updates) > 0 {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: updates,
			Reason:         []model.TriggerReason{model.AmbientUpdate},
		})
	}
}

func (a *AmbientIndex) handleService(obj any, isDelete bool, c *Controller) sets.Set[model.ConfigKey] {
	svc := controllers.Extract[*v1.Service](obj)
	updates := sets.New[model.ConfigKey]()

	if svc.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
		scope := model.WaypointScope{Namespace: svc.Namespace, ServiceAccount: svc.Annotations[constants.WaypointServiceAccount]}

		// TODO get IP+Port from the Gateway CRD
		// https://github.com/istio/istio/issues/44230
		if svc.Spec.ClusterIP == v1.ClusterIPNone {
			// TODO handle headless Service
			log.Warn("headless service currently not supported as a waypoint")
			return updates
		}
		waypointPort := uint32(15008)
		for _, p := range svc.Spec.Ports {
			if strings.Contains(p.Name, "hbone") {
				waypointPort = uint32(p.Port)
			}
		}
		svcIP := netip.MustParseAddr(svc.Spec.ClusterIP)
		addr := &workloadapi.GatewayAddress{
			Destination: &workloadapi.GatewayAddress_Address{
				Address: &workloadapi.NetworkAddress{
					Network: c.Network(svcIP.String(), make(labels.Instance, 0)).String(),
					Address: svcIP.AsSlice(),
				},
			},
			Port: waypointPort,
		}

		if isDelete {
			if proto.Equal(a.waypoints[scope], addr) {
				delete(a.waypoints, scope)
				updates.Merge(a.updateWaypoint(scope, addr, true))
			}
		} else {
			if !proto.Equal(a.waypoints[scope], addr) {
				a.waypoints[scope] = addr
				updates.Merge(a.updateWaypoint(scope, addr, false))
			}
		}
	}

	si := c.constructService(svc)
	networkAddrs := make([]networkAddress, 0, len(si.Addresses))
	for _, addr := range si.Addresses {
		if vip, ok := netip.AddrFromSlice(addr.Address); ok {
			networkAddrs = append(networkAddrs, networkAddress{
				ip:      vip.String(),
				network: addr.Network,
			})
		}
	}
	pods := c.getPodsInService(svc)
	wls := make(map[string]*model.WorkloadInfo, len(pods))
	for _, p := range pods {
		// Can be nil if it's not ready, hostNetwork, etc
		wl := c.extractWorkload(p)
		if wl != nil {
			// Update the pod, since it now has new VIP info
			for _, networkAddr := range networkAddressFromWorkload(wl) {
				a.byPod[networkAddr] = wl
			}
			a.byUID[wl.Uid] = wl
			wls[wl.Uid] = wl
		}
	}

	workloadEntries := c.getWorkloadEntriesInService(svc)
	for _, w := range workloadEntries {
		wl := c.extractWorkloadEntry(w)
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
			a.byUID[wl.Uid] = wl
			wls[wl.Uid] = wl
		}
	}

	// We send an update for each *workload* IP address previously in the service; they may have changed
	for _, networkAddr := range networkAddrs {
		for _, wl := range a.byService[networkAddr] {
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
		}
	}
	// Update indexes
	if isDelete {
		for _, networkAddr := range networkAddrs {
			delete(a.byService, networkAddr)
			delete(a.serviceByAddr, networkAddr)
		}
		delete(a.serviceByHostname, si.ResourceName())
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
	} else {
		for _, networkAddr := range networkAddrs {
			a.byService[networkAddr] = wls
			a.serviceByAddr[networkAddr] = si
		}
		a.serviceByHostname[si.ResourceName()] = si
		updates.Insert(model.ConfigKey{Kind: kind.Address, Name: si.ResourceName()})
	}
	// Fetch updates again, in case it changed from adding new workloads
	for _, networkAddr := range networkAddrs {
		for _, wl := range a.byService[networkAddr] {
			updates.Insert(model.ConfigKey{Kind: kind.Address, Name: wl.ResourceName()})
		}
	}

	return updates
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
func (c *Controller) AddressInformation(addresses sets.String) ([]*model.AddressInfo, []string) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var wls []*model.AddressInfo
	var removed []string
	for addr := range addresses {
		wl := c.ambientIndex.Lookup(addr)
		if len(wl) == 0 {
			removed = append(removed, addr)
		} else {
			wls = append(wls, wl...)
		}
	}
	return wls, removed
}

func (c *Controller) constructWorkload(pod *v1.Pod, waypoint *workloadapi.GatewayAddress, policies []string) *workloadapi.Workload {
	vips := map[string]*workloadapi.PortList{}
	allServices := c.services.List(pod.Namespace, klabels.Everything())
	if services := getPodServices(allServices, pod); len(services) > 0 {
		for _, svc := range services {
			for _, vip := range getVIPs(svc) {
				if vips[vip] == nil {
					vips[vip] = &workloadapi.PortList{}
				}
				for _, port := range svc.Spec.Ports {
					if port.Protocol != v1.ProtocolTCP {
						continue
					}
					targetPort, err := FindPort(pod, &port)
					if err != nil {
						log.Debug(err)
						continue
					}
					vips[vip].Ports = append(vips[vip].Ports, &workloadapi.Port{
						ServicePort: uint32(port.Port),
						TargetPort:  uint32(targetPort),
					})
				}
			}
		}
	}

	addresses := make([][]byte, 0, len(pod.Status.PodIPs))
	for _, podIP := range pod.Status.PodIPs {
		addresses = append(addresses, parseIP(podIP.IP))
	}
	for _, svc := range c.ambientIndex.servicesMap {

		if svc.Attributes.ServiceEntry == nil {
			// if we are here then this is dev error
			log.Warn("dev error: service entry spec is nil; it should have been populated by the service entry handler")
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
		if !labels.Instance(sel).SubsetOf(pod.Labels) {
			continue
		}

		vipsToPorts := getVIPsFromServiceEntry(svc, nil)
		for vip, ports := range vipsToPorts {
			vips[vip] = ports
		}
	}

	wl := &workloadapi.Workload{
		Uid:                   c.generatePodUID(pod),
		Name:                  pod.Name,
		Addresses:             addresses,
		Network:               c.Network(pod.Status.PodIP, pod.Labels).String(),
		Namespace:             pod.Namespace,
		ServiceAccount:        pod.Spec.ServiceAccountName,
		Node:                  pod.Spec.NodeName,
		VirtualIps:            vips,
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
	res := []string{}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		res = append(res, svc.Spec.ClusterIP)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		res = append(res, ing.IP)
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
			// We may have gotten an update for Pod, but are subscribe to a Service.
			// We need to force a subscription on the Pod as well
			for vip := range wl.VirtualIps {
				if currentSubs.Contains(vip) {
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
