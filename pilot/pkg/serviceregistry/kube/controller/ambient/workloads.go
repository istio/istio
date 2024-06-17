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

// nolint: gocritic
package ambient

import (
	"net/netip"
	"strconv"

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) WorkloadsCollection(
	Pods krt.Collection[*v1.Pod],
	Nodes krt.Collection[*v1.Node],
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	WorkloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	EndpointSlices krt.Collection[*discovery.EndpointSlice],
	Namespaces krt.Collection[*v1.Namespace],
) krt.Collection[model.WorkloadInfo] {
	WorkloadServicesNamespaceIndex := krt.NewNamespaceIndex(WorkloadServices)
	PodWorkloads := krt.NewCollection(
		Pods,
		a.podWorkloadBuilder(MeshConfig, AuthorizationPolicies, PeerAuths, Waypoints, WorkloadServices, WorkloadServicesNamespaceIndex, Namespaces, Nodes),
		krt.WithName("PodWorkloads"),
	)
	WorkloadEntryWorkloads := krt.NewCollection(
		WorkloadEntries,
		a.workloadEntryWorkloadBuilder(MeshConfig, AuthorizationPolicies, PeerAuths, Waypoints, WorkloadServices, WorkloadServicesNamespaceIndex, Namespaces),
		krt.WithName("WorkloadEntryWorkloads"),
	)
	ServiceEntryWorkloads := krt.NewManyCollection(
		ServiceEntries,
		a.serviceEntryWorkloadBuilder(MeshConfig, AuthorizationPolicies, PeerAuths, Waypoints, Namespaces),
		krt.WithName("ServiceEntryWorkloads"),
	)
	EndpointSliceWorkloads := krt.NewManyCollection(
		EndpointSlices,
		a.endpointSlicesBuilder(MeshConfig, WorkloadServices),
		krt.WithName("EndpointSliceWorkloads"))

	NetworkGatewayWorkloads := krt.NewManyFromNothing[model.WorkloadInfo](func(ctx krt.HandlerContext) []model.WorkloadInfo {
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		return slices.Map(a.LookupNetworkGateways(), convertGateway)
	}, krt.WithName("NetworkGatewayWorkloads"))

	Workloads := krt.JoinCollection([]krt.Collection[model.WorkloadInfo]{
		PodWorkloads,
		WorkloadEntryWorkloads,
		ServiceEntryWorkloads,
		EndpointSliceWorkloads,
		NetworkGatewayWorkloads,
	}, krt.WithName("Workloads"))
	return Workloads
}

func (a *index) workloadEntryWorkloadBuilder(
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	WorkloadServicesNamespaceIndex krt.Index[string, model.ServiceInfo],
	Namespaces krt.Collection[*v1.Namespace],
) krt.TransformationSingle[*networkingclient.WorkloadEntry, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, wle *networkingclient.WorkloadEntry) *model.WorkloadInfo {
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		policies := a.buildWorkloadPolicies(ctx, AuthorizationPolicies, PeerAuths, meshCfg, wle.Labels, wle.Namespace)
		var waypoint *Waypoint
		if wle.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			waypoint = fetchWaypointForWorkload(ctx, Waypoints, Namespaces, wle.ObjectMeta)
		}
		var waypointAddress *workloadapi.GatewayAddress
		if waypoint != nil {
			waypointAddress = a.getWaypointAddress(waypoint)
		}
		fo := []krt.FetchOption{krt.FilterIndex(WorkloadServicesNamespaceIndex, wle.Namespace), krt.FilterSelectsNonEmpty(wle.GetLabels())}
		if !features.EnableK8SServiceSelectWorkloadEntries {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source == kind.ServiceEntry
			}))
		}
		services := krt.Fetch(ctx, WorkloadServices, fo...)
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		network := a.Network(wle.Spec.Address, wle.Labels).String()
		if wle.Spec.Network != "" {
			network = wle.Spec.Network
		}

		// enforce traversing waypoints
		policies = append(policies, implicitWaypointPolicies(ctx, Waypoints, waypoint, services)...)

		w := &workloadapi.Workload{
			Uid:                   a.generateWorkloadEntryUID(wle.Namespace, wle.Name),
			Name:                  wle.Name,
			Namespace:             wle.Namespace,
			Network:               network,
			NetworkGateway:        a.getNetworkGatewayAddress(network),
			ClusterId:             string(a.ClusterID),
			ServiceAccount:        wle.Spec.ServiceAccount,
			Services:              constructServicesFromWorkloadEntry(&wle.Spec, services),
			AuthorizationPolicies: policies,
			Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
			Waypoint:              waypointAddress,
			TrustDomain:           pickTrustDomain(meshCfg),
			Locality:              getWorkloadEntryLocality(&wle.Spec),
		}

		if addr, err := netip.ParseAddr(wle.Spec.Address); err == nil {
			w.Addresses = [][]byte{addr.AsSlice()}
		} else {
			log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", wle.Namespace, wle.Name)
		}

		w.WorkloadName, w.WorkloadType = wle.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(wle.Labels, w.WorkloadName)

		setTunnelProtocol(wle.Labels, wle.Annotations, w)
		return &model.WorkloadInfo{Workload: w, Labels: wle.Labels, Source: kind.WorkloadEntry, CreationTime: wle.CreationTimestamp.Time}
	}
}

func (a *index) podWorkloadBuilder(
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	WorkloadServicesNamespaceIndex krt.Index[string, model.ServiceInfo],
	Namespaces krt.Collection[*v1.Namespace],
	Nodes krt.Collection[*v1.Node],
) krt.TransformationSingle[*v1.Pod, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		// Pod Is Pending but have a pod IP should be a valid workload, we should build it ,
		// Such as the pod have initContainer which is initialing.
		// See https://github.com/istio/istio/issues/48854
		if (!IsPodRunning(p) && !IsPodPending(p)) || p.Spec.HostNetwork {
			return nil
		}
		k8sPodIPs := p.Status.PodIPs
		if len(k8sPodIPs) == 0 && p.Status.PodIP != "" {
			k8sPodIPs = []v1.PodIP{{IP: p.Status.PodIP}}
		}
		if len(k8sPodIPs) == 0 {
			return nil
		}
		podIPs, err := slices.MapErr(k8sPodIPs, func(e v1.PodIP) ([]byte, error) {
			n, err := netip.ParseAddr(e.IP)
			if err != nil {
				return nil, err
			}
			return n.AsSlice(), nil
		})
		if err != nil {
			// Is this possible? Probably not in typical case, but anyone could put garbage there.
			return nil
		}
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		policies := a.buildWorkloadPolicies(ctx, AuthorizationPolicies, PeerAuths, meshCfg, p.Labels, p.Namespace)
		fo := []krt.FetchOption{krt.FilterIndex(WorkloadServicesNamespaceIndex, p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels())}
		if !features.EnableServiceEntrySelectPods {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source == kind.Service
			}))
		}
		services := krt.Fetch(ctx, WorkloadServices, fo...)
		// Logic from https://github.com/howardjohn/kubernetes/blob/7c873327b679a70337288da62b96dd610858181d/staging/src/k8s.io/endpointslice/utils.go#L37
		// Kubernetes has Ready, Serving, and Terminating. We only have a boolean, which is sufficient for our cases
		status := workloadapi.WorkloadStatus_HEALTHY
		if !IsPodReady(p) || p.DeletionTimestamp != nil {
			status = workloadapi.WorkloadStatus_UNHEALTHY
		}
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		// We only check the network of the first IP. This should be fine; it is not supported for a single pod to span multiple networks
		network := a.Network(p.Status.PodIP, p.Labels).String()

		var appTunnel *workloadapi.ApplicationTunnel
		var targetWaypoint *Waypoint
		if instancedWaypoint := fetchWaypointForInstance(ctx, Waypoints, p.ObjectMeta); instancedWaypoint != nil {
			// we're an instance of a waypoint, set inbound tunnel info
			appTunnel = &workloadapi.ApplicationTunnel{
				Protocol: instancedWaypoint.DefaultBinding.Protocol,
				Port:     instancedWaypoint.DefaultBinding.Port,
			}
		} else if waypoint := fetchWaypointForWorkload(ctx, Waypoints, Namespaces, p.ObjectMeta); waypoint != nil {
			// there is a workload-attached waypoint, point there with a GatewayAddress
			targetWaypoint = waypoint
		}

		// enforce traversing waypoints
		policies = append(policies, implicitWaypointPolicies(ctx, Waypoints, targetWaypoint, services)...)

		w := &workloadapi.Workload{
			Uid:                   a.generatePodUID(p),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			NetworkGateway:        a.getNetworkGatewayAddress(network),
			ClusterId:             string(a.ClusterID),
			Addresses:             podIPs,
			ServiceAccount:        p.Spec.ServiceAccountName,
			Waypoint:              a.getWaypointAddress(targetWaypoint),
			Node:                  p.Spec.NodeName,
			ApplicationTunnel:     appTunnel,
			Services:              constructServices(p, services),
			AuthorizationPolicies: policies,
			Status:                status,
			TrustDomain:           pickTrustDomain(meshCfg),
			Locality:              getPodLocality(ctx, Nodes, p),
		}

		w.WorkloadName, w.WorkloadType = workloadNameAndType(p)
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		setTunnelProtocol(p.Labels, p.Annotations, w)
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.Pod, CreationTime: p.CreationTimestamp.Time}
	}
}

func (a *index) buildWorkloadPolicies(
	ctx krt.HandlerContext,
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshCfg *MeshConfig,
	workloadLabels map[string]string,
	workloadNamespace string,
) []string {
	// We need to filter from the policies that are present, which apply to us.
	// We only want label selector ones; global ones are not attached to the final WorkloadInfo
	// In general we just take all of the policies
	basePolicies := krt.Fetch(ctx, AuthorizationPolicies, krt.FilterSelects(workloadLabels), krt.FilterGeneric(func(a any) bool {
		wa := a.(model.WorkloadAuthorization)
		nsMatch := wa.Authorization.Namespace == meshCfg.RootNamespace || wa.Authorization.Namespace == workloadNamespace
		return nsMatch && wa.GetLabelSelector() != nil
	}))
	policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
		return t.ResourceName()
	}))
	// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
	auths := fetchPeerAuthentications(ctx, PeerAuths, meshCfg, workloadNamespace, workloadLabels)
	policies = append(policies, convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
	return policies
}

func (a *index) serviceEntryWorkloadBuilder(
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	Namespaces krt.Collection[*v1.Namespace],
) krt.TransformationMulti[*networkingclient.ServiceEntry, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, se *networkingclient.ServiceEntry) []model.WorkloadInfo {
		eps := se.Spec.Endpoints
		// If we have a DNS service, endpoints are not required
		implicitEndpoints := len(eps) == 0 &&
			(se.Spec.Resolution == networkingv1alpha3.ServiceEntry_DNS || se.Spec.Resolution == networkingv1alpha3.ServiceEntry_DNS_ROUND_ROBIN) &&
			se.Spec.WorkloadSelector == nil
		if len(eps) == 0 && !implicitEndpoints {
			return nil
		}
		// here we don't care about the *service* waypoint (hence it is nil); we are only going to use a subset of the info in
		// `allServices` (since we are building workloads here, not services).
		allServices := a.serviceEntriesInfo(se, nil)
		if implicitEndpoints {
			eps = slices.Map(allServices, func(si model.ServiceInfo) *networkingv1alpha3.WorkloadEntry {
				return &networkingv1alpha3.WorkloadEntry{Address: si.Hostname}
			})
		}
		if len(eps) == 0 {
			return nil
		}
		res := make([]model.WorkloadInfo, 0, len(eps))

		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())

		for i, wle := range eps {
			services := allServices
			if implicitEndpoints {
				// For implicit endpoints, we generate each one from the hostname it was from.
				// Otherwise, use all.
				// [i] is safe here since we these are constructed to mirror each other
				services = []model.ServiceInfo{allServices[i]}
			}

			policies := a.buildWorkloadPolicies(ctx, AuthorizationPolicies, PeerAuths, meshCfg, se.Labels, se.Namespace)

			var waypointAddress *workloadapi.GatewayAddress
			// Endpoint does not have a real ObjectMeta, so make one
			if !implicitEndpoints {
				if waypoint := fetchWaypointForWorkload(ctx, Waypoints, Namespaces, metav1.ObjectMeta{
					Name:      se.Name,
					Namespace: se.Namespace,
					Labels:    wle.Labels,
				}); waypoint != nil {
					waypointAddress = a.getWaypointAddress(waypoint)
					// enforce traversing waypoints
					policies = append(policies, implicitWaypointPolicies(ctx, Waypoints, waypoint, services)...)
				}
			}

			a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
			network := a.Network(wle.Address, wle.Labels).String()
			if wle.Network != "" {
				network = wle.Network
			}
			w := &workloadapi.Workload{
				Uid:                   a.generateServiceEntryUID(se.Namespace, se.Name, wle.Address),
				Name:                  se.Name,
				Namespace:             se.Namespace,
				Network:               network,
				NetworkGateway:        a.getNetworkGatewayAddress(network),
				ClusterId:             string(a.ClusterID),
				ServiceAccount:        wle.ServiceAccount,
				Services:              constructServicesFromWorkloadEntry(wle, services),
				AuthorizationPolicies: policies,
				Status:                workloadapi.WorkloadStatus_HEALTHY,
				Waypoint:              waypointAddress,
				TrustDomain:           pickTrustDomain(meshCfg),
				Locality:              getWorkloadEntryLocality(wle),
			}

			if addr, err := netip.ParseAddr(wle.Address); err == nil {
				w.Addresses = [][]byte{addr.AsSlice()}
			} else {
				w.Hostname = wle.Address
			}

			w.WorkloadName, w.WorkloadType = se.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
			w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(se.Labels, w.WorkloadName)

			setTunnelProtocol(se.Labels, se.Annotations, w)
			res = append(res, model.WorkloadInfo{Workload: w, Labels: se.Labels, Source: kind.WorkloadEntry, CreationTime: se.CreationTimestamp.Time})
		}
		return res
	}
}

func (a *index) endpointSlicesBuilder(
	MeshConfig krt.Singleton[MeshConfig],
	WorkloadServices krt.Collection[model.ServiceInfo],
) krt.TransformationMulti[*discovery.EndpointSlice, model.WorkloadInfo] {
	return func(ctx krt.HandlerContext, es *discovery.EndpointSlice) []model.WorkloadInfo {
		// EndpointSlices carry port information and a list of IPs.
		// We only care about EndpointSlices that are for a Service.
		// Otherwise, it is just an arbitrary bag of IP addresses for some user-specific purpose, which doesn't have a clear
		// usage for us (if it had some additional info like service account, etc, then perhaps it would be useful).
		serviceName, f := es.Labels[discovery.LabelServiceName]
		if !f {
			return nil
		}
		if es.AddressType == discovery.AddressTypeFQDN {
			// Currently we do not support FQDN. In theory, we could, but its' support in Kubernetes entirely is questionable and
			// may be removed in the near future.
			return nil
		}
		var res []model.WorkloadInfo
		seen := sets.New[string]()
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())

		// The slice must be for a single service, based on the label above.
		serviceKey := es.Namespace + "/" + string(kube.ServiceHostname(serviceName, es.Namespace, a.DomainSuffix))
		svcs := krt.Fetch(ctx, WorkloadServices, krt.FilterKey(serviceKey), krt.FilterGeneric(func(a any) bool {
			// Only find Service, not Service Entry
			return a.(model.ServiceInfo).Source == kind.Service
		}))
		if len(svcs) == 0 {
			// no service found
			return nil
		}
		// There SHOULD only be one. This is only Service which has unique hostnames.
		svc := svcs[0]

		// Translate slice ports to our port.
		pl := &workloadapi.PortList{Ports: make([]*workloadapi.Port, 0, len(es.Ports))}
		for _, p := range es.Ports {
			// We must have name and port (Kubernetes should always set these)
			if p.Name == nil {
				continue
			}
			if p.Port == nil {
				continue
			}
			// We only support TCP for now
			if p.Protocol == nil || *p.Protocol != v1.ProtocolTCP {
				continue
			}
			// Endpoint slice port has name (service port name, not containerPort) and port (targetPort)
			// We need to join with the Service port list to translate the port name to
			for _, svcPort := range svc.Ports {
				portName := svc.PortNames[int32(svcPort.ServicePort)]
				if portName.PortName != *p.Name {
					continue
				}
				pl.Ports = append(pl.Ports, &workloadapi.Port{
					ServicePort: svcPort.ServicePort,
					TargetPort:  uint32(*p.Port),
				})
				break
			}
		}
		services := map[string]*workloadapi.PortList{
			serviceKey: pl,
		}

		// Each endpoint in the slice is going to create a Workload
		for _, ep := range es.Endpoints {
			if ep.TargetRef != nil && ep.TargetRef.Kind == gvk.Pod.Kind {
				// Normal case; this is a slice for a pod. We already handle pods, with much more information, so we can skip them
				continue
			}
			// This should not be possible
			if len(ep.Addresses) == 0 {
				continue
			}
			// We currently only support 1 address. Kubernetes will never set more (IPv4 and IPv6 will be two slices), so its mostly undefined.
			key := ep.Addresses[0]
			if seen.InsertContains(key) {
				// Shouldn't happen. Make sure our UID is actually unique
				log.Warnf("IP address %v seen twice in %v/%v", key, es.Namespace, es.Name)
				continue
			}
			health := workloadapi.WorkloadStatus_UNHEALTHY
			if ep.Conditions.Ready == nil || *ep.Conditions.Ready {
				health = workloadapi.WorkloadStatus_HEALTHY
			}
			// Translate our addresses.
			// Note: users may put arbitrary addresses here. It is recommended by Kubernetes to not
			// give untrusted users EndpointSlice write access.
			addresses, err := slices.MapErr(ep.Addresses, func(e string) ([]byte, error) {
				n, err := netip.ParseAddr(e)
				if err != nil {
					log.Warnf("invalid address in endpointslice %v: %v", e, err)
					return nil, err
				}
				return n.AsSlice(), nil
			})
			if err != nil {
				// If any invalid, skip
				continue
			}
			w := &workloadapi.Workload{
				Uid:                   a.ClusterID.String() + "/discovery.k8s.io/EndpointSlice/" + es.Namespace + "/" + es.Name + "/" + key,
				Name:                  es.Name,
				Namespace:             es.Namespace,
				Addresses:             addresses,
				Hostname:              "",
				Network:               a.Network(key, nil).String(),
				TrustDomain:           pickTrustDomain(meshCfg),
				Services:              services,
				Status:                health,
				ClusterId:             string(a.ClusterID),
				AuthorizationPolicies: nil, // Not support. This can only be used for outbound, so not relevant
				ServiceAccount:        "",  // Unknown. TODO: make this possible to express in ztunnel
				Waypoint:              nil, // Not supported. In theory, we could allow it as an EndpointSlice label, but there is no real use case.
				Locality:              nil, // Not supported. We could maybe, there is a "zone", but it doesn't seem to be well supported
			}
			res = append(res, model.WorkloadInfo{
				Workload:     w,
				Labels:       nil,
				Source:       kind.EndpointSlice,
				CreationTime: es.CreationTimestamp.Time,
			})
		}

		return res
	}
}

func setTunnelProtocol(labels, annotations map[string]string, w *workloadapi.Workload) {
	if annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled {
		// Configured for override
		w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
	}
	// Otherwise supports tunnel directly
	if model.SupportsTunnel(labels, model.TunnelHTTP) {
		w.TunnelProtocol = workloadapi.TunnelProtocol_HBONE
		w.NativeTunnel = true
	}
}

func pickTrustDomain(mesh *MeshConfig) string {
	if td := mesh.GetTrustDomain(); td != "cluster.local" {
		return td
	}
	return ""
}

func fetchPeerAuthentications(
	ctx krt.HandlerContext,
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshCfg *MeshConfig,
	ns string,
	matchLabels map[string]string,
) []*securityclient.PeerAuthentication {
	return krt.Fetch(ctx, PeerAuths, krt.FilterGeneric(func(a any) bool {
		pol := a.(*securityclient.PeerAuthentication)
		if pol.Namespace == meshCfg.GetRootNamespace() && pol.Spec.Selector == nil {
			return true
		}
		if pol.Namespace != ns {
			return false
		}
		sel := pol.Spec.Selector
		if sel == nil {
			return true // No selector matches everything
		}
		return labels.Instance(sel.MatchLabels).SubsetOf(matchLabels)
	}))
}

func constructServicesFromWorkloadEntry(p *networkingv1alpha3.WorkloadEntry, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Namespace, svc.Hostname)
		pl := &workloadapi.PortList{}
		res[n] = pl
		for _, port := range svc.Ports {
			targetPort := port.TargetPort
			// Named targetPort has different semantics from Service vs ServiceEntry
			if svc.Source == kind.Service {
				// Service has explicit named targetPorts.
				if named, f := svc.PortNames[int32(port.ServicePort)]; f && named.TargetPortName != "" {
					// This port is a named target port, look it up
					tv, ok := p.Ports[named.TargetPortName]
					if !ok {
						// We needed an explicit port, but didn't find one - skip this port
						continue
					}
					targetPort = tv
				}
			} else {
				// ServiceEntry has no explicit named targetPorts; targetPort only allows a number
				// Instead, there is name matching between the port names
				if named, f := svc.PortNames[int32(port.ServicePort)]; f {
					// get port name or target port
					tv, ok := p.Ports[named.PortName]
					if ok {
						// if we match one, override it. Otherwise, use the service port
						targetPort = tv
					} else if targetPort == 0 {
						targetPort = port.ServicePort
					}
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

func constructServices(p *v1.Pod, services []model.ServiceInfo) map[string]*workloadapi.PortList {
	res := map[string]*workloadapi.PortList{}
	for _, svc := range services {
		n := namespacedHostname(svc.Namespace, svc.Hostname)
		pl := &workloadapi.PortList{
			Ports: make([]*workloadapi.Port, 0, len(svc.Ports)),
		}
		res[n] = pl
		for _, port := range svc.Ports {
			targetPort := port.TargetPort
			// The svc.Ports represents the workloadapi.Service, which drops the port name info and just has numeric target Port.
			// TargetPort can be 0 which indicates its a named port. Check if its a named port and replace with the real targetPort if so.
			if named, f := svc.PortNames[int32(port.ServicePort)]; f && named.TargetPortName != "" {
				// Pods only match on TargetPort names
				tp, ok := FindPortName(p, named.TargetPortName)
				if !ok {
					// Port not present for this workload. Exclude the port entirely
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

func getPodLocality(ctx krt.HandlerContext, Nodes krt.Collection[*v1.Node], pod *v1.Pod) *workloadapi.Locality {
	// NodeName is set by the scheduler after the pod is created
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#late-initialization
	node := ptr.Flatten(krt.FetchOne(ctx, Nodes, krt.FilterKey(pod.Spec.NodeName)))
	if node == nil {
		if pod.Spec.NodeName != "" {
			log.Warnf("unable to get node %q for pod %q/%q", pod.Spec.NodeName, pod.Namespace, pod.Name)
		}
		return nil
	}

	region := node.GetLabels()[v1.LabelTopologyRegion]
	zone := node.GetLabels()[v1.LabelTopologyZone]
	subzone := node.GetLabels()[label.TopologySubzone.Name]

	if region == "" && zone == "" && subzone == "" {
		return nil
	}

	return &workloadapi.Locality{
		Region:  region,
		Zone:    zone,
		Subzone: subzone,
	}
}

func getWorkloadEntryLocality(p *networkingv1alpha3.WorkloadEntry) *workloadapi.Locality {
	region, zone, subzone := labelutil.SplitLocalityLabel(p.GetLocality())
	if region == "" && zone == "" && subzone == "" {
		return nil
	}
	return &workloadapi.Locality{
		Region:  region,
		Zone:    zone,
		Subzone: subzone,
	}
}

func implicitWaypointPolicies(ctx krt.HandlerContext, Waypoints krt.Collection[Waypoint], waypoint *Waypoint, services []model.ServiceInfo) []string {
	if !features.DefaultAllowFromWaypoint {
		return nil
	}
	serviceWaypointKeys := slices.MapFilter(services, func(si model.ServiceInfo) *string {
		if si.Waypoint == "" || (waypoint != nil && waypoint.ResourceName() == si.Waypoint) {
			return nil
		}
		return ptr.Of(si.Waypoint)
	})
	if len(serviceWaypointKeys) == 0 {
		if waypoint != nil {
			n := implicitWaypointPolicyName(waypoint)
			if n != "" {
				return []string{waypoint.Namespace + "/" + n}
			}
		}
		return nil
	}
	waypoints := krt.Fetch(ctx, Waypoints, krt.FilterKeys(serviceWaypointKeys...))
	if waypoint != nil {
		waypoints = append(waypoints, *waypoint)
	}

	return slices.MapFilter(waypoints, func(w Waypoint) *string {
		policy := implicitWaypointPolicyName(&w)
		if policy == "" {
			return nil
		}
		return ptr.Of(w.Namespace + "/" + policy)
	})
}

func gatewayUID(gw model.NetworkGateway) string {
	return "NetworkGateway/" + string(gw.Network) + "/" + gw.Addr + "/" + strconv.Itoa(int(gw.HBONEPort))
}

// convertGateway always converts a NetworkGateway into a Workload.
// Workloads have a NetworkGateway field, which is effectively a pointer to another object (Service or Workload); in order
// to facilitate this we need to translate our Gateway model down into a WorkloadInfo ztunnel can understand.
func convertGateway(gw model.NetworkGateway) model.WorkloadInfo {
	wl := &workloadapi.Workload{
		Uid:            gatewayUID(gw),
		ServiceAccount: gw.ServiceAccount.Name,
		Namespace:      gw.ServiceAccount.Namespace,
		Network:        gw.Network.String(),
	}

	if ip, err := netip.ParseAddr(gw.Addr); err == nil {
		wl.Addresses = append(wl.Addresses, ip.AsSlice())
	} else {
		wl.Hostname = gw.Addr
	}

	return model.WorkloadInfo{Workload: wl}
}

func (a *index) getNetworkGateway(id string) []model.NetworkGateway {
	gtws := a.LookupNetworkGateways()
	slices.FilterInPlace(gtws, func(gateway model.NetworkGateway) bool {
		return gateway.Network == network.ID(id)
	})
	return gtws
}

func (a *index) getNetworkGatewayAddress(network string) *workloadapi.GatewayAddress {
	if networks := a.getNetworkGateway(network); len(networks) > 0 {
		// Currently only support one, so find the first one that is valid
		for _, net := range networks {
			if net.HBONEPort == 0 {
				continue
			}
			ip, err := netip.ParseAddr(net.Addr)
			if err != nil {
				continue
			}
			return &workloadapi.GatewayAddress{
				Destination: &workloadapi.GatewayAddress_Address{
					// probably use from Cidr instead?
					Address: &workloadapi.NetworkAddress{
						Network: net.Network.String(),
						Address: ip.AsSlice(),
					},
				},
				HboneMtlsPort: net.HBONEPort,
			}
		}
	}
	return nil
}
