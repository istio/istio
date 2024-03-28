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

package ambient

import (
	"net/netip"
	"strings"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

type Index interface {
	Lookup(key string) []model.AddressInfo
	All() []model.AddressInfo
	WorkloadsForWaypoint(key model.WaypointKey) []model.WorkloadInfo
	ServicesForWaypoint(key model.WaypointKey) []model.ServiceInfo
	Waypoint(network, address string) []netip.Addr
	SyncAll()
	model.AmbientIndexes
}

var _ Index = &index{}

type workloadsCollection struct {
	krt.Collection[model.WorkloadInfo]
	ByAddress        *krt.Index[model.WorkloadInfo, networkAddress]
	ByServiceKey     *krt.Index[model.WorkloadInfo, string]
	ByOwningWaypoint *krt.Index[model.WorkloadInfo, networkAddress]
}

type waypointsCollection struct {
	krt.Collection[Waypoint]
}

type servicesCollection struct {
	krt.Collection[model.ServiceInfo]
	ByAddress        *krt.Index[model.ServiceInfo, networkAddress]
	ByOwningWaypoint *krt.Index[model.ServiceInfo, networkAddress]
}

// index maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type index struct {
	services  servicesCollection
	workloads workloadsCollection
	waypoints waypointsCollection

	authorizationPolicies krt.Collection[model.WorkloadAuthorization]
	networkUpdateTrigger  *krt.RecomputeTrigger

	SystemNamespace string
	DomainSuffix    string
	ClusterID       cluster.ID
	XDSUpdater      model.XDSUpdater
	Network         LookupNetwork
}

type Options struct {
	Client kubeclient.Client

	Revision        string
	SystemNamespace string
	DomainSuffix    string
	ClusterID       cluster.ID
	XDSUpdater      model.XDSUpdater
	LookupNetwork   LookupNetwork
}

func New(options Options) Index {
	a := &index{
		networkUpdateTrigger: krt.NewRecomputeTrigger(),

		SystemNamespace: options.SystemNamespace,
		DomainSuffix:    options.DomainSuffix,
		ClusterID:       options.ClusterID,
		XDSUpdater:      options.XDSUpdater,
		Network:         options.LookupNetwork,
	}

	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	ConfigMaps := krt.NewInformerFiltered[*v1.ConfigMap](options.Client, filter, krt.WithName("ConfigMaps"))

	authzPolicies := kclient.NewDelayedInformer[*securityclient.AuthorizationPolicy](options.Client,
		gvr.AuthorizationPolicy, kubetypes.StandardInformer, filter)
	AuthzPolicies := krt.WrapClient[*securityclient.AuthorizationPolicy](authzPolicies, krt.WithName("AuthorizationPolicies"))

	peerAuths := kclient.NewDelayedInformer[*securityclient.PeerAuthentication](options.Client,
		gvr.PeerAuthentication, kubetypes.StandardInformer, filter)
	PeerAuths := krt.WrapClient[*securityclient.PeerAuthentication](peerAuths, krt.WithName("PeerAuthentications"))

	serviceEntries := kclient.NewDelayedInformer[*networkingclient.ServiceEntry](options.Client,
		gvr.ServiceEntry, kubetypes.StandardInformer, filter)
	ServiceEntries := krt.WrapClient[*networkingclient.ServiceEntry](serviceEntries, krt.WithName("ServiceEntries"))

	workloadEntries := kclient.NewDelayedInformer[*networkingclient.WorkloadEntry](options.Client,
		gvr.WorkloadEntry, kubetypes.StandardInformer, filter)
	WorkloadEntries := krt.WrapClient[*networkingclient.WorkloadEntry](workloadEntries, krt.WithName("WorkloadEntries"))

	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](options.Client, gvr.KubernetesGateway, kubetypes.StandardInformer, filter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, krt.WithName("Gateways"))

	Services := krt.NewInformerFiltered[*v1.Service](options.Client, filter, krt.WithName("Services"))
	Pods := krt.NewInformerFiltered[*v1.Pod](options.Client, kclient.Filter{
		ObjectFilter:    options.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripPodUnusedFields,
	}, krt.WithName("Pods"))

	// TODO: Should this go ahead and transform the full ns into some intermediary with just the details we care about?
	Namespaces := krt.NewInformer[*v1.Namespace](options.Client, krt.WithName("Namespaces"))

	MeshConfig := MeshConfigCollection(ConfigMaps, options)
	Waypoints := WaypointsCollection(Gateways)

	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := PolicyCollections(AuthzPolicies, PeerAuths, MeshConfig)
	AllPolicies.RegisterBatch(PushXds(a.XDSUpdater, func(i model.WorkloadAuthorization) model.ConfigKey {
		return model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace}
	}), false)

	// these are workloadapi-style services combined from kube services and service entries
	WorkloadServices := a.ServicesCollection(Services, ServiceEntries, Waypoints, Namespaces)
	ServiceAddressIndex := krt.CreateIndex[model.ServiceInfo, networkAddress](WorkloadServices, networkAddressFromService)
	ServiceInfosByOwningWaypoint := krt.CreateIndex[model.ServiceInfo, networkAddress](WorkloadServices, func(s model.ServiceInfo) []networkAddress {
		// Filter out waypoint services
		if s.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}

		ip := waypointAddress.GetAddress()
		netip, _ := netip.AddrFromSlice(ip)
		netaddr := networkAddress{
			network: waypointAddress.GetNetwork(),
			ip:      netip.String(),
		}
		return append(make([]networkAddress, 1), netaddr)
	})
	WorkloadServices.RegisterBatch(krt.BatchedEventFilter(
		func(a model.ServiceInfo) *workloadapi.Service {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Service
		},
		PushXds(a.XDSUpdater, func(i model.ServiceInfo) model.ConfigKey {
			return model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()}
		})), false)

	Workloads := a.WorkloadsCollection(
		Pods,
		MeshConfig,
		AuthorizationPolicies,
		PeerAuths,
		Waypoints,
		WorkloadServices,
		WorkloadEntries,
		ServiceEntries,
		AllPolicies,
		Namespaces,
	)
	WorkloadAddressIndex := krt.CreateIndex[model.WorkloadInfo, networkAddress](Workloads, networkAddressFromWorkload)
	WorkloadServiceIndex := krt.CreateIndex[model.WorkloadInfo, string](Workloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Services)
	})
	WorkloadWaypointIndex := krt.CreateIndex[model.WorkloadInfo, networkAddress](Workloads, func(w model.WorkloadInfo) []networkAddress {
		// Filter out waypoints.
		if w.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := w.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}

		ip := waypointAddress.GetAddress()
		netip, _ := netip.AddrFromSlice(ip)
		netaddr := networkAddress{
			network: waypointAddress.GetNetwork(),
			ip:      netip.String(),
		}
		return append(make([]networkAddress, 1), netaddr)
	})
	// Subtle: make sure we register the event after the Index are created. This ensures when we get the event, the index is populated.
	Workloads.RegisterBatch(krt.BatchedEventFilter(
		func(a model.WorkloadInfo) *workloadapi.Workload {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Workload
		},
		PushXds(a.XDSUpdater, func(i model.WorkloadInfo) model.ConfigKey {
			return model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()}
		})), false)

	a.workloads = workloadsCollection{
		Collection:       Workloads,
		ByAddress:        WorkloadAddressIndex,
		ByServiceKey:     WorkloadServiceIndex,
		ByOwningWaypoint: WorkloadWaypointIndex,
	}
	a.services = servicesCollection{
		Collection:       WorkloadServices,
		ByAddress:        ServiceAddressIndex,
		ByOwningWaypoint: ServiceInfosByOwningWaypoint,
	}
	a.waypoints = waypointsCollection{
		Collection: Waypoints,
	}
	a.authorizationPolicies = AllPolicies

	return a
}

// Lookup finds all addresses associated with a given key. Many different key formats are supported; see inline comments.
func (a *index) Lookup(key string) []model.AddressInfo {
	// 1. Workload UID
	if w := a.workloads.GetKey(krt.Key[model.WorkloadInfo](key)); w != nil {
		return []model.AddressInfo{workloadToAddressInfo(w.Workload)}
	}

	network, ip, found := strings.Cut(key, "/")
	if !found {
		log.Warnf(`key (%v) did not contain the expected "/" character`, key)
		return nil
	}
	networkAddr := networkAddress{network: network, ip: ip}

	// 2. Workload by IP
	if wls := a.workloads.ByAddress.Lookup(networkAddr); len(wls) > 0 {
		// If there is just one, return it
		if len(wls) == 1 {
			return []model.AddressInfo{modelWorkloadToAddressInfo(wls[0])}
		}
		// Otherwise, try to find a pod - pods have precedence
		pod := slices.FindFunc(wls, func(info model.WorkloadInfo) bool {
			return info.Source == kind.Pod
		})
		if pod != nil {
			return []model.AddressInfo{modelWorkloadToAddressInfo(*pod)}
		}
		// Otherwise just return the first one; all WorkloadEntry have the same weight
		return []model.AddressInfo{modelWorkloadToAddressInfo(wls[0])}
	}

	// 3. Service
	if svc := a.lookupService(key); svc != nil {
		res := []model.AddressInfo{serviceToAddressInfo(svc.Service)}
		for _, w := range a.workloads.ByServiceKey.Lookup(svc.ResourceName()) {
			res = append(res, workloadToAddressInfo(w.Workload))
		}
		return res
	}
	return nil
}

func (a *index) lookupService(key string) *model.ServiceInfo {
	// 1. namespace/hostname format
	s := a.services.GetKey(krt.Key[model.ServiceInfo](key))
	if s != nil {
		return s
	}

	// 2. network/ip format
	network, ip, _ := strings.Cut(key, "/")
	services := a.services.ByAddress.Lookup(networkAddress{
		network: network,
		ip:      ip,
	})
	return slices.First(services)
}

// All return all known workloads. Result is un-ordered
func (a *index) All() []model.AddressInfo {
	res := []model.AddressInfo{}
	type kindindex struct {
		k     kind.Kind
		index int
	}
	addrm := map[netip.Addr]kindindex{}
	for _, wl := range a.workloads.List("") {
		overwrite := -1
		write := true
		for _, addr := range wl.Addresses {
			a := byteIPToAddr(addr)
			if existing, f := addrm[a]; f {
				// This address was already found. We want unique addresses in the result.
				// Pod > WorkloadEntry
				if wl.Source == kind.Pod && existing.k != kind.Pod {
					overwrite = existing.index
					addrm[a] = kindindex{
						k:     wl.Source,
						index: overwrite,
					}
				} else {
					write = false
				}
			}
		}
		if overwrite >= 0 {
			res[overwrite] = workloadToAddressInfo(wl.Workload)
		} else if write {
			res = append(res, workloadToAddressInfo(wl.Workload))
			for _, addr := range wl.Addresses {
				a := byteIPToAddr(addr)
				addrm[a] = kindindex{
					k:     wl.Source,
					index: overwrite,
				}
			}
		}
	}
	for _, s := range a.services.List("") {
		res = append(res, serviceToAddressInfo(s.Service))
	}
	return res
}

// AddressInformation returns all AddressInfo's in the cluster.
// This may be scoped to specific subsets by specifying a non-empty addresses field
func (a *index) AddressInformation(addresses sets.String) ([]model.AddressInfo, sets.String) {
	if len(addresses) == 0 {
		// Full update
		return a.All(), nil
	}
	var res []model.AddressInfo
	var removed []string
	got := sets.New[string]()
	for wname := range addresses {
		wl := a.Lookup(wname)
		if len(wl) == 0 {
			removed = append(removed, wname)
		} else {
			for _, addr := range wl {
				if !got.InsertContains(addr.ResourceName()) {
					res = append(res, addr)
				}
			}
		}
	}
	return res, sets.New(removed...)
}

func (a *index) ServicesForWaypoint(key model.WaypointKey) []model.ServiceInfo {
	return a.services.ByOwningWaypoint.Lookup(networkAddress{
		network: key.Network,
		ip:      key.Addresses[0],
	})
}

func (a *index) WorkloadsForWaypoint(key model.WaypointKey) []model.WorkloadInfo {
	// TODO: we should be able to handle multiple IPs or a hostname
	if len(key.Addresses) == 0 {
		return nil
	}
	workloads := a.workloads.ByOwningWaypoint.Lookup(networkAddress{
		network: key.Network,
		ip:      key.Addresses[0],
	})
	workloads = model.SortWorkloadsByCreationTime(workloads)
	return workloads
}

func (a *index) Waypoint(network, address string) []netip.Addr {
	res := sets.Set[netip.Addr]{}
	networkAddr := networkAddress{
		network: network,
		ip:      address,
	}
	addressInfos := a.Lookup(networkAddr.String())
	for _, addressInfo := range addressInfos {
		waypointAddress := addressInfo.GetService().GetWaypoint().GetAddress().GetAddress()
		if a, ok := netip.AddrFromSlice(waypointAddress); ok {
			res.Insert(a)
			// This was a service, therefore it is not a workload and we can just move on
			continue
		}

		waypointAddress = addressInfo.GetWorkload().GetWaypoint().GetAddress().GetAddress()
		if a, ok := netip.AddrFromSlice(waypointAddress); ok {
			res.Insert(a)
		}
	}
	return res.UnsortedList()
}

func (a *index) AdditionalPodSubscriptions(
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
		for _, wl := range model.ExtractWorkloadsFromAddresses(a.Lookup(addr)) {
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
		for _, wl := range model.ExtractWorkloadsFromAddresses(a.All()) {
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

func (a *index) SyncAll() {
	a.networkUpdateTrigger.TriggerRecomputation()
}

type LookupNetwork func(endpointIP string, labels labels.Instance) network.ID

func PushXds[T any](xds model.XDSUpdater, f func(T) model.ConfigKey) func(events []krt.Event[T]) {
	return func(events []krt.Event[T]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(f(i))
			}
		}
		xds.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	}
}
