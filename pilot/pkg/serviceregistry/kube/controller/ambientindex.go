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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	meshapi "istio.io/api/mesh/v1alpha1"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/cv2"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/workloadapi/security"
)

type WorkloadsCollection struct {
	cv2.Collection[model.WorkloadInfo]
	ByAddress        *cv2.Index[model.WorkloadInfo, networkAddress]
	ByServiceVIP     *cv2.Index[model.WorkloadInfo, string]
	ByOwningWaypoint *cv2.Index[model.WorkloadInfo, model.WaypointScope]
}

type WaypointsCollection struct {
	cv2.Collection[Waypoint]
	ByScope *cv2.Index[Waypoint, model.WaypointScope]
}

type ServicesCollection struct {
	cv2.Collection[model.ServiceInfo]
}

// AmbientIndexImpl maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type AmbientIndexImpl struct {
	services  ServicesCollection
	workloads WorkloadsCollection
	waypoints WaypointsCollection

	authorizationPolicies cv2.Collection[model.WorkloadAuthorization]
}

func workloadToAddressInfo(w *workloadapi.Workload) model.AddressInfo {
	return model.AddressInfo{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: w,
			},
		},
	}
}

func modelWorkloadToAddressInfo(w model.WorkloadInfo) model.AddressInfo {
	return workloadToAddressInfo(w.Workload)
}

func serviceToAddressInfo(s *workloadapi.Service) model.AddressInfo {
	return model.AddressInfo{
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

// Lookup finds a given IP address.
func (a *AmbientIndexImpl) Lookup(key string) []model.AddressInfo {
	// 1. Workload UID
	if w := a.workloads.GetKey(cv2.Key[model.WorkloadInfo](key)); w != nil {
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
		vips := sets.New[string]()
		for _, addr := range svc.Service.Addresses {
			vips.Insert(byteIPToString(addr.Address))
		}
		rn := svc.ResourceName()
		res := []model.AddressInfo{serviceToAddressInfo(svc.Service)}
		// TODO: avoid full scan
		for _, wl := range a.workloads.List(metav1.NamespaceAll) {
			if _, f := wl.Services[rn]; f {
				res = append(res, workloadToAddressInfo(wl.Workload))
			}
		}
		return res
	}
	return nil
}

func (a *AmbientIndexImpl) lookupService(key string) *model.ServiceInfo {
	// 1. namespace/hostname format
	s := a.services.GetKey(cv2.Key[model.ServiceInfo](key))
	if s != nil {
		return s
	}

	// 2. network/ip format
	network, ip, _ := strings.Cut(key, "/")
	// Maybe its a hostname..
	// TODO remove full scan
	for _, maybe := range a.services.List(metav1.NamespaceAll) {
		for _, addr := range maybe.Addresses {
			if network == addr.Network && ip == byteIPToString(addr.Address) {
				return &maybe
			}
		}
	}
	return nil
}

// All return all known workloads. Result is un-ordered
func (a *AmbientIndexImpl) All() []model.AddressInfo {
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

func (c *Controller) WorkloadsForWaypoint(scope model.WaypointScope) []model.WorkloadInfo {
	// Lookup scope. If its namespace wide, remove entries that are in SA scope
	workloads := c.ambientIndex.workloads.ByOwningWaypoint.Lookup(scope)
	if scope.ServiceAccount == "" {
		// TODO: find a way filter workloads that have a per-SA waypoint
	}
	return workloads
}

// Waypoint finds all waypoint IP addresses for a given scope.  Performs first a Namespace+ServiceAccount
// then falls back to any Namespace wide waypoints
func (c *Controller) Waypoint(scope model.WaypointScope) []netip.Addr {
	a := c.ambientIndex
	res := sets.Set[netip.Addr]{}
	waypoints := a.waypoints.ByScope.Lookup(scope)
	if len(waypoints) == 0 {
		// Now look for namespace-wide
		scope.ServiceAccount = ""
		waypoints = a.waypoints.ByScope.Lookup(scope)
	}
	for _, waypoint := range waypoints {
		res.Insert(waypoint.Addresses[0])
	}
	return res.UnsortedList()
}

func meshConfigMapData(cm *v1.ConfigMap) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data["mesh"]
	if !exists {
		return ""
	}

	return cfgYaml
}

type Waypoint struct {
	cv2.Named

	ForServiceAccount string
	Addresses         []netip.Addr
}

func (w Waypoint) ResourceName() string {
	return w.GetNamespace() + "/" + w.GetName()
}

func (c *Controller) setupIndex(options Options) *AmbientIndexImpl {
	ConfigMaps := cv2.NewInformer[*v1.ConfigMap](c.client)
	AuthzPolicies := cv2.NewInformer[*securityclient.AuthorizationPolicy](c.client)
	PeerAuths := cv2.NewInformer[*securityclient.PeerAuthentication](c.client)
	ServiceEntries := cv2.NewInformer[*networkingclient.ServiceEntry](c.client)
	WorkloadEntries := cv2.NewInformer[*networkingclient.WorkloadEntry](c.client)
	Services := cv2.WrapClient[*v1.Service](c.services)
	Pods := cv2.WrapClient[*v1.Pod](c.podsClient)

	MeshConfig := MeshConfigCollection(ConfigMaps, options)
	Waypoints := c.Waypointscollection(Services)

	AuthzDerivedPolicies := cv2.NewCollection(AuthzPolicies, func(ctx cv2.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WorkloadAuthorization {
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertAuthorizationPolicy(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{Authorization: pol, LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels())}
	})
	PeerAuthDerivedPolicies := cv2.NewCollection(PeerAuths, func(ctx cv2.HandlerContext, i *securityclient.PeerAuthentication) *model.WorkloadAuthorization {
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertPeerAuthentication(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{
			Authorization: pol,
			LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels()),
		}
	})
	DefaultPolicy := cv2.NewSingleton[model.WorkloadAuthorization](func(ctx cv2.HandlerContext) *model.WorkloadAuthorization {
		if len(cv2.Fetch(ctx, PeerAuths)) == 0 {
			return nil
		}
		// If there are any PeerAuthentications in our cache, send our static STRICT policy
		return &model.WorkloadAuthorization{
			LabelSelector: model.LabelSelector{},
			Authorization: &security.Authorization{
				Name:      staticStrictPolicyName,
				Namespace: c.meshWatcher.Mesh().GetRootNamespace(),
				Scope:     security.Scope_WORKLOAD_SELECTOR,
				Action:    security.Action_DENY,
				Groups: []*security.Group{
					{
						Rules: []*security.Rules{
							{
								Matches: []*security.Match{
									{
										NotPrincipals: []*security.StringMatch{
											{
												MatchType: &security.StringMatch_Presence{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	})
	// Policies contains all of the policies we will send down to clients
	Policies := cv2.JoinCollection(AuthzDerivedPolicies, PeerAuthDerivedPolicies, DefaultPolicy.AsCollection())
	Policies.RegisterBatch(func(events []cv2.Event[model.WorkloadAuthorization]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace})
			}
		}
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	})

	ServicesInfo := cv2.NewCollection(Services, func(ctx cv2.HandlerContext, s *v1.Service) *model.ServiceInfo {
		portNames := map[int32]model.ServicePortName{}
		for _, p := range s.Spec.Ports {
			portNames[p.Port] = model.ServicePortName{
				PortName:       p.Name,
				TargetPortName: p.TargetPort.StrVal,
			}
		}
		return &model.ServiceInfo{
			Service:       c.constructService(s),
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
		}
	})
	ServiceEntriesInfo := cv2.NewManyCollection(ServiceEntries, func(ctx cv2.HandlerContext, s *networkingclient.ServiceEntry) []model.ServiceInfo {
		sel := model.NewSelector(s.Spec.GetWorkloadSelector().GetLabels())
		portNames := map[int32]model.ServicePortName{}
		for _, p := range s.Spec.Ports {
			portNames[int32(p.Number)] = model.ServicePortName{
				PortName: p.Name,
			}
		}
		return slices.Map(c.constructServiceEntries(s), func(e *workloadapi.Service) model.ServiceInfo {
			return model.ServiceInfo{
				Service:       e,
				PortNames:     portNames,
				LabelSelector: sel,
			}
		})
	})
	WorkloadServices := cv2.JoinCollection(ServicesInfo, ServiceEntriesInfo)
	WorkloadServices.RegisterBatch(func(events []cv2.Event[model.ServiceInfo]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()})
			}
		}
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	})

	PodWorkloads := cv2.NewCollection(Pods, func(ctx cv2.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		if !IsPodRunning(p) || p.Spec.HostNetwork {
			return nil
		}
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones, we handle global ones through another mechanism.
		// In general we just take all ofthe policies
		basePolicies := cv2.Fetch(ctx, AuthzDerivedPolicies, cv2.FilterSelects(p.Labels), cv2.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		})
		// We could do a non-FilterGeneric but cv2 currently blows up if we depend on the same collection twice
		auths := cv2.Fetch(ctx, PeerAuths, cv2.FilterGeneric(func(a any) bool {
			pol := a.(*securityclient.PeerAuthentication)
			if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
				return true
			}
			if pol.Namespace != p.Namespace {
				return false
			}
			sel := pol.Spec.Selector
			if sel == nil {
				return true // No selector matches everything
			}
			return labels.Instance(sel.MatchLabels).SubsetOf(p.Labels)
		}))
		policies = append(policies, c.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		var waypoints []Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// Waypoints do not have waypoints, but anything else does
			waypoints = cv2.Fetch(ctx, Waypoints,
				cv2.FilterNamespace(p.Namespace), cv2.FilterGeneric(func(a any) bool {
					w := a.(Waypoint)
					return w.ForServiceAccount == "" || w.ForServiceAccount == p.Spec.ServiceAccountName
				}))
		}
		services := cv2.Fetch(ctx, WorkloadServices, cv2.FilterNamespace(p.Namespace), cv2.FilterSelectsNonEmpty(p.GetLabels()))
		status := workloadapi.WorkloadStatus_HEALTHY
		if !IsPodReady(p) {
			status = workloadapi.WorkloadStatus_UNHEALTHY
		}
		w := &workloadapi.Workload{
			Uid:                   c.generatePodUID(p),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               c.network.String(),
			ClusterId:             string(c.Cluster()),
			Addresses:             [][]byte{netip.MustParseAddr(p.Status.PodIP).AsSlice()},
			ServiceAccount:        p.Spec.ServiceAccountName,
			Node:                  p.Spec.NodeName,
			Services:              c.constructServices(p, services),
			AuthorizationPolicies: policies,
			Status:                status,
		}
		if len(waypoints) > 0 {
			wp := waypoints[0]
			w.Waypoint = &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					Address: c.toNetworkAddress(wp.Addresses[0].String()),
				},
				// TODO: look up the HBONE port instead of hardcoding it
				Port: 15008,
			}
		}

		if td := spiffe.GetTrustDomain(); td != "cluster.local" {
			w.TrustDomain = td
		}
		w.WorkloadName, w.WorkloadType = workloadNameAndType(p)
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		if p.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
			// Configured for override
			w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		}
		// Otherwise supports tunnel directly
		if model.SupportsTunnel(p.Labels, model.TunnelHTTP) {
			w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
			w.NativeTunnel = true
		}
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.Pod}
	})
	WorkloadEntryWorkloads := cv2.NewCollection(WorkloadEntries, func(ctx cv2.HandlerContext, p *networkingclient.WorkloadEntry) *model.WorkloadInfo {
		meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones, we handle global ones through another mechanism.
		// In general we just take all ofthe policies
		basePolicies := cv2.Fetch(ctx, AuthzDerivedPolicies, cv2.FilterSelects(p.Labels), cv2.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		})
		// We could do a non-FilterGeneric but cv2 currently blows up if we depend on the same collection twice
		auths := cv2.Fetch(ctx, PeerAuths, cv2.FilterGeneric(func(a any) bool {
			pol := a.(*securityclient.PeerAuthentication)
			if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
				return true
			}
			if pol.Namespace != p.Namespace {
				return false
			}
			sel := pol.Spec.Selector
			if sel == nil {
				return true // No selector matches everything
			}
			return labels.Instance(sel.MatchLabels).SubsetOf(p.Labels)
		}))
		policies = append(policies, c.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		var waypoints []Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// Waypoints do not have waypoints, but anything else does
			waypoints = cv2.Fetch(ctx, Waypoints,
				cv2.FilterNamespace(p.Namespace), cv2.FilterGeneric(func(a any) bool {
					w := a.(Waypoint)
					return w.ForServiceAccount == "" || w.ForServiceAccount == p.Spec.ServiceAccount
				}))
		}
		services := cv2.Fetch(ctx, WorkloadServices, cv2.FilterNamespace(p.Namespace), cv2.FilterSelectsNonEmpty(p.GetLabels()))
		network := c.Network(p.Spec.Address, p.Labels).String()
		if p.Spec.Network != "" {
			network = p.Spec.Network
		}
		w := &workloadapi.Workload{
			Uid:                   c.generateWorkloadEntryUID(p.Namespace, p.Name),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			ServiceAccount:        p.Spec.ServiceAccount,
			Services:              c.constructServicesFromWorkloadEntry(&p.Spec, services),
			AuthorizationPolicies: policies,
			Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
		}

		if addr, err := netip.ParseAddr(p.Spec.Address); err == nil {
			w.Addresses = [][]byte{addr.AsSlice()}
		} else {
			log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", p.Namespace, p.Name)
		}
		if len(waypoints) > 0 {
			wp := waypoints[0]
			w.Waypoint = &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					Address: c.toNetworkAddress(wp.Addresses[0].String()),
				},
				// TODO: look up the HBONE port instead of hardcoding it
				Port: 15008,
			}
		}

		if td := spiffe.GetTrustDomain(); td != "cluster.local" {
			w.TrustDomain = td
		}
		w.WorkloadName, w.WorkloadType = p.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		if p.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
			// Configured for override
			w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		}
		// Otherwise supports tunnel directly
		if model.SupportsTunnel(p.Labels, model.TunnelHTTP) {
			w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
			w.NativeTunnel = true
		}
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.WorkloadEntry}
	})
	ServiceEntryWorkloads := cv2.NewManyCollection(ServiceEntries, func(ctx cv2.HandlerContext, se *networkingclient.ServiceEntry) []model.WorkloadInfo {
		if len(se.Spec.Endpoints) == 0 {
			return nil
		}
		res := make([]model.WorkloadInfo, 0, len(se.Spec.Endpoints))

		svc := cv2.FetchOne(ctx, ServiceEntriesInfo, cv2.FilterKey(namespacedHostname(se.Namespace, se.Spec.Hosts[0])))
		if svc == nil {
			// Not ready yet
			return nil
		}
		for _, p := range se.Spec.Endpoints {
			meshCfg := cv2.FetchOne(ctx, MeshConfig.AsCollection())
			// We need to filter from the policies that are present, which apply to us.
			// We only want label selector ones, we handle global ones through another mechanism.
			// In general we just take all ofthe policies
			basePolicies := cv2.Fetch(ctx, AuthzDerivedPolicies, cv2.FilterSelects(se.Labels), cv2.FilterGeneric(func(a any) bool {
				return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
			}))
			policies := slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
				return t.ResourceName()
			})
			// We could do a non-FilterGeneric but cv2 currently blows up if we depend on the same collection twice
			auths := cv2.Fetch(ctx, PeerAuths, cv2.FilterGeneric(func(a any) bool {
				pol := a.(*securityclient.PeerAuthentication)
				if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
					return true
				}
				if pol.Namespace != se.Namespace {
					return false
				}
				sel := pol.Spec.Selector
				if sel == nil {
					return true // No selector matches everything
				}
				return labels.Instance(sel.MatchLabels).SubsetOf(p.Labels)
			}))
			policies = append(policies, c.convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
			var waypoints []Waypoint
			if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
				// Waypoints do not have waypoints, but anything else does
				waypoints = cv2.Fetch(ctx, Waypoints,
					cv2.FilterNamespace(se.Namespace), cv2.FilterGeneric(func(a any) bool {
						w := a.(Waypoint)
						return w.ForServiceAccount == "" || w.ForServiceAccount == p.ServiceAccount
					}))
			}
			network := c.Network(p.Address, p.Labels).String()
			if p.Network != "" {
				network = p.Network
			}
			w := &workloadapi.Workload{
				Uid:                   c.generateServiceEntryUID(se.Namespace, se.Name, p.Address),
				Name:                  se.Name,
				Namespace:             se.Namespace,
				Network:               network,
				ServiceAccount:        p.ServiceAccount,
				Services:              c.constructServicesFromWorkloadEntry(p, []model.ServiceInfo{*svc}),
				AuthorizationPolicies: policies,
				Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
			}

			if addr, err := netip.ParseAddr(p.Address); err == nil {
				w.Addresses = [][]byte{addr.AsSlice()}
			} else {
				log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", se.Namespace, se.Name)
			}
			if len(waypoints) > 0 {
				wp := waypoints[0]
				w.Waypoint = &workloadapi.GatewayAddress{
					Destination: &workloadapi.GatewayAddress_Address{
						Address: c.toNetworkAddress(wp.Addresses[0].String()),
					},
					// TODO: look up the HBONE port instead of hardcoding it
					Port: 15008,
				}
			}

			if td := spiffe.GetTrustDomain(); td != "cluster.local" {
				w.TrustDomain = td
			}
			w.WorkloadName, w.WorkloadType = se.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
			w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(se.Labels, w.WorkloadName)

			if se.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
				// Configured for override
				w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
			}
			// Otherwise supports tunnel directly
			if model.SupportsTunnel(se.Labels, model.TunnelHTTP) {
				w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
				w.NativeTunnel = true
			}
			res = append(res, model.WorkloadInfo{Workload: w, Labels: se.Labels, Source: kind.WorkloadEntry})
		}
		return res
	})
	Workloads := cv2.JoinCollection(PodWorkloads, WorkloadEntryWorkloads, ServiceEntryWorkloads)
	Workloads.RegisterBatch(func(events []cv2.Event[model.WorkloadInfo]) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				cu.Insert(model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()})
			}
		}
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	})

	WorkloadAddressIndex := cv2.CreateIndex[model.WorkloadInfo, networkAddress](Workloads, networkAddressFromWorkload)
	WorkloadServiceIndex := cv2.CreateIndex[model.WorkloadInfo, string](Workloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Services)
	})
	WorkloadWaypointIndex := cv2.CreateIndex[model.WorkloadInfo, model.WaypointScope](Workloads, func(w model.WorkloadInfo) []model.WaypointScope {
		// Filter out waypoints.
		if w.Labels[constants.ManagedGatewayLabel] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// We can be a part of a service account waypoint, or a namespace waypoint
		return []model.WaypointScope{
			{
				Namespace:      w.Namespace,
				ServiceAccount: w.ServiceAccount,
			},
			{
				Namespace: w.Namespace,
			},
		}
	})
	WaypointIndex := cv2.CreateIndex[Waypoint, model.WaypointScope](Waypoints, func(w Waypoint) []model.WaypointScope {
		// We can be a part of a service account waypoint, or a namespace waypoint
		return []model.WaypointScope{{Namespace: w.Namespace, ServiceAccount: w.ForServiceAccount}}
	})
	return &AmbientIndexImpl{
		workloads: WorkloadsCollection{
			Collection:       Workloads,
			ByAddress:        WorkloadAddressIndex,
			ByServiceVIP:     WorkloadServiceIndex,
			ByOwningWaypoint: WorkloadWaypointIndex,
		},
		services: ServicesCollection{Collection: WorkloadServices},
		waypoints: WaypointsCollection{
			Collection: Waypoints,
			ByScope:    WaypointIndex,
		},
		authorizationPolicies: Policies,
	}
}

func (c *Controller) Waypointscollection(Services cv2.Collection[*v1.Service]) cv2.Collection[Waypoint] {
	return cv2.NewCollection(Services, func(ctx cv2.HandlerContext, svc *v1.Service) *Waypoint {
		if svc.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// not a waypoint
			return nil
		}
		sa := svc.Annotations[constants.WaypointServiceAccount]
		return &Waypoint{
			Named:             cv2.NewNamed(svc.ObjectMeta),
			ForServiceAccount: sa,
			Addresses:         getVIPAddrs(svc),
		}
	})
}

func MeshConfigCollection(ConfigMaps cv2.Collection[*v1.ConfigMap], options Options) cv2.Singleton[meshapi.MeshConfig] {
	return cv2.NewSingleton[meshapi.MeshConfig](
		func(ctx cv2.HandlerContext) *meshapi.MeshConfig {
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*v1.ConfigMap{}
			cms = cv2.AppendNonNil(cms, cv2.FetchOne(ctx, ConfigMaps, cv2.FilterName("istio-user", options.SystemNamespace)))
			cms = cv2.AppendNonNil(cms, cv2.FetchOne(ctx, ConfigMaps, cv2.FilterName("istio", options.SystemNamespace)))

			for _, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					continue
				}
				meshCfg = n
			}
			return meshCfg
		},
	)
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
func (c *Controller) AddressInformation(addresses sets.String) ([]model.AddressInfo, []string) {
	if len(addresses) == 0 {
		// Full update
		return c.ambientIndex.All(), nil
	}
	var res []model.AddressInfo
	var removed []string
	for wname := range addresses {
		wl := c.ambientIndex.Lookup(wname)
		if len(wl) == 0 {
			removed = append(removed, wname)
		} else {
			res = append(res, wl...)
		}
	}
	return res, removed
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

func (c *Controller) constructServices(p *v1.Pod, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Namespace, svc.Hostname)
		pl := &workloadapi.PortList{}
		res[n] = pl
		for _, port := range svc.Ports {
			targetPort := port.TargetPort
			if named, f := svc.PortNames[int32(port.ServicePort)]; f && named.TargetPortName != "" {
				// Pods only match on TargetPort names
				tp, ok := FindPortName(p, named.TargetPortName)
				if !ok {
					// Port not present for this workload
					continue
				}
				targetPort = uint32(tp)
			}

			pl.Ports = append(pl.Ports, &workloadapi.Port{
				ServicePort: port.ServicePort,
				TargetPort:  targetPort,
			})
		}
	}
	return res
}

func (c *Controller) constructServicesFromWorkloadEntry(p *networkingv1alpha3.WorkloadEntry, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Namespace, svc.Hostname)
		pl := &workloadapi.PortList{}
		res[n] = pl
		for _, port := range svc.Ports {
			targetPort := port.TargetPort
			if named, f := svc.PortNames[int32(port.ServicePort)]; f {
				// get port name or target port
				nv, nf := p.Ports[named.PortName]
				tv, tf := p.Ports[named.TargetPortName]
				// TODO: is this logic/order correct?
				if tf {
					targetPort = tv
				} else if nf {
					targetPort = nv
				} else if named.TargetPortName != "" {
					// We needed an explicit port, but didn't find one - skip this port
					continue
				}
			}
			pl.Ports = append(pl.Ports, &workloadapi.Port{
				ServicePort: port.ServicePort,
				TargetPort:  targetPort,
			})
		}
	}
	return res
}

func networkAddressFromWorkload(wl model.WorkloadInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(wl.Addresses))
	for _, addr := range wl.Addresses {
		ip, _ := netip.AddrFromSlice(addr)
		networkAddrs = append(networkAddrs, networkAddress{network: wl.Network, ip: ip.String()})
	}
	return networkAddrs
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
		// IPs are strictly optional for loadbalancers - they may just have a hostname.
		if ing.IP != "" {
			res = append(res, ing.IP)
		}
	}
	return res
}

func getVIPAddrs(svc *v1.Service) []netip.Addr {
	return slices.Map(getVIPs(svc), func(e string) netip.Addr {
		return netip.MustParseAddr(e)
	})
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

func byteIPToString(b []byte) string {
	ip, _ := netip.AddrFromSlice(b)
	return ip.String()
}

func byteIPToAddr(b []byte) netip.Addr {
	ip, _ := netip.AddrFromSlice(b)
	return ip
}

func (c *Controller) constructService(svc *v1.Service) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	return &workloadapi.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Hostname:  string(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)),
		Addresses: slices.Map(getVIPs(svc), c.toNetworkAddress),
		Ports:     ports,
	}
}

func (c *Controller) constructServiceEntries(svc *networkingclient.ServiceEntry) []*workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: p.Number,
			TargetPort:  p.TargetPort,
		})
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	res := make([]*workloadapi.Service, 0, len(svc.Spec.Hosts))
	for _, h := range svc.Spec.Hosts {
		res = append(res, &workloadapi.Service{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Hostname:  h,
			Addresses: slices.Map(svc.Spec.Addresses, c.toNetworkAddress),
			Ports:     ports,
		})
	}
	return res
}

func (c *Controller) toNetworkAddress(vip string) *workloadapi.NetworkAddress {
	return &workloadapi.NetworkAddress{
		Network: c.Network(vip, make(labels.Instance, 0)).String(),
		Address: netip.MustParseAddr(vip).AsSlice(),
	}
}
