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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/kind"
	kubeutil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	kubelabels "istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/workloadapi"
)

func (a *index) WorkloadsCollection(
	Pods krt.Collection[*v1.Pod],
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	WorkloadEntries krt.Collection[*networkingclient.WorkloadEntry],
	ServiceEntries krt.Collection[*networkingclient.ServiceEntry],
	AllPolicies krt.Collection[model.WorkloadAuthorization],
	Namespaces krt.Collection[*v1.Namespace],
) krt.Collection[model.WorkloadInfo] {
	PodWorkloads := krt.NewCollection(
		Pods,
		a.podWorkloadBuilder(MeshConfig, AuthorizationPolicies, PeerAuths, Waypoints, WorkloadServices, Namespaces),
		krt.WithName("PodWorkloads"),
	)
	WorkloadEntryWorkloads := krt.NewCollection(
		WorkloadEntries,
		a.workloadEntryWorkloadBuilder(MeshConfig, AuthorizationPolicies, PeerAuths, Waypoints, WorkloadServices, Namespaces),
		krt.WithName("WorkloadEntryWorkloads"),
	)
	ServiceEntryWorkloads := krt.NewManyCollection(ServiceEntries, func(ctx krt.HandlerContext, se *networkingclient.ServiceEntry) []model.WorkloadInfo {
		if len(se.Spec.Endpoints) == 0 {
			return nil
		}
		res := make([]model.WorkloadInfo, 0, len(se.Spec.Endpoints))

		wp := fetchWaypoint(ctx, Waypoints, Namespaces, se.ObjectMeta)

		// this is some partial object meta we can pass through so that WL found in the Endpoints
		// may inherit the namespace scope waypoint from the SE... the Endpoints do not have real object meta
		// and therefore they can't be annotated with wl scope waypoints right now
		someObjectMeta := metav1.ObjectMeta{
			Namespace: se.Namespace,
		}

		svc := slices.First(a.serviceEntriesInfo(se, wp))
		if svc == nil {
			// Not ready yet
			return nil
		}
		for _, p := range se.Spec.Endpoints {
			meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
			// We need to filter from the policies that are present, which apply to us.
			// We only want label selector ones; global ones are not attached to the final WorkloadInfo
			// In general we just take all of the policies
			basePolicies := krt.Fetch(ctx, AllPolicies, krt.FilterSelects(se.Labels), krt.FilterGeneric(func(a any) bool {
				return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
			}))
			policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
				return t.ResourceName()
			}))
			// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
			auths := fetchPeerAuthentications(ctx, PeerAuths, meshCfg, se.Namespace, p.Labels)
			policies = append(policies, convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
			var waypoint *Waypoint
			if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
				// Waypoints do not have waypoints, but anything else does

				// this is using object meta which simply defines the namespace since the endpoint doesn't have it's own object meta
				waypoint = fetchWaypoint(ctx, Waypoints, Namespaces, someObjectMeta)
			}
			var waypointAddress *workloadapi.GatewayAddress
			if waypoint != nil {
				waypointAddress = a.getWaypointAddress(waypoint)
			}
			a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
			network := a.Network(p.Address, p.Labels).String()
			if p.Network != "" {
				network = p.Network
			}
			w := &workloadapi.Workload{
				Uid:                   a.generateServiceEntryUID(se.Namespace, se.Name, p.Address),
				Name:                  se.Name,
				Namespace:             se.Namespace,
				Network:               network,
				ClusterId:             string(a.ClusterID),
				ServiceAccount:        p.ServiceAccount,
				Services:              constructServicesFromWorkloadEntry(p, []model.ServiceInfo{*svc}),
				AuthorizationPolicies: policies,
				Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
				Waypoint:              waypointAddress,
				TrustDomain:           pickTrustDomain(),
			}

			if addr, err := netip.ParseAddr(p.Address); err == nil {
				w.Addresses = [][]byte{addr.AsSlice()}
			} else {
				log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", se.Namespace, se.Name)
			}

			w.WorkloadName, w.WorkloadType = se.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
			w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(se.Labels, w.WorkloadName)

			setTunnelProtocol(se.Labels, se.Annotations, w)
			res = append(res, model.WorkloadInfo{Workload: w, Labels: se.Labels, Source: kind.WorkloadEntry, CreationTime: se.CreationTimestamp.Time})
		}
		return res
	}, krt.WithName("ServiceEntryWorkloads"))
	Workloads := krt.JoinCollection([]krt.Collection[model.WorkloadInfo]{PodWorkloads, WorkloadEntryWorkloads, ServiceEntryWorkloads}, krt.WithName("Workloads"))
	return Workloads
}

func (a *index) workloadEntryWorkloadBuilder(
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	Namespaces krt.Collection[*v1.Namespace],
) func(ctx krt.HandlerContext, p *networkingclient.WorkloadEntry) *model.WorkloadInfo {
	return func(ctx krt.HandlerContext, p *networkingclient.WorkloadEntry) *model.WorkloadInfo {
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones; global ones are not attached to the final WorkloadInfo
		// In general we just take all of the policies
		basePolicies := krt.Fetch(ctx, AuthorizationPolicies, krt.FilterSelects(p.Labels), krt.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		}))
		// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
		auths := fetchPeerAuthentications(ctx, PeerAuths, meshCfg, p.Namespace, p.Labels)
		policies = append(policies, convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		var waypoint *Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			waypoint = fetchWaypoint(ctx, Waypoints, Namespaces, p.ObjectMeta)
		}
		var waypointAddress *workloadapi.GatewayAddress
		if waypoint != nil {
			waypointAddress = a.getWaypointAddress(waypoint)
		}
		fo := []krt.FetchOption{krt.FilterNamespace(p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels())}
		if !features.EnableK8SServiceSelectWorkloadEntries {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source == kind.ServiceEntry
			}))
		}
		services := krt.Fetch(ctx, WorkloadServices, fo...)
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		network := a.Network(p.Spec.Address, p.Labels).String()
		if p.Spec.Network != "" {
			network = p.Spec.Network
		}
		w := &workloadapi.Workload{
			Uid:                   a.generateWorkloadEntryUID(p.Namespace, p.Name),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			ClusterId:             string(a.ClusterID),
			ServiceAccount:        p.Spec.ServiceAccount,
			Services:              constructServicesFromWorkloadEntry(&p.Spec, services),
			AuthorizationPolicies: policies,
			Status:                workloadapi.WorkloadStatus_HEALTHY, // TODO: WE can be unhealthy
			Waypoint:              waypointAddress,
			TrustDomain:           pickTrustDomain(),
		}

		if addr, err := netip.ParseAddr(p.Spec.Address); err == nil {
			w.Addresses = [][]byte{addr.AsSlice()}
		} else {
			log.Warnf("skipping workload entry %s/%s; DNS Address resolution is not yet implemented", p.Namespace, p.Name)
		}

		w.WorkloadName, w.WorkloadType = p.Name, workloadapi.WorkloadType_POD // XXX(shashankram): HACK to impersonate pod
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		setTunnelProtocol(p.Labels, p.Annotations, w)
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.WorkloadEntry, CreationTime: p.CreationTimestamp.Time}
	}
}

func (a *index) podWorkloadBuilder(
	MeshConfig krt.Singleton[MeshConfig],
	AuthorizationPolicies krt.Collection[model.WorkloadAuthorization],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	Waypoints krt.Collection[Waypoint],
	WorkloadServices krt.Collection[model.ServiceInfo],
	Namespaces krt.Collection[*v1.Namespace],
) func(ctx krt.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
	return func(ctx krt.HandlerContext, p *v1.Pod) *model.WorkloadInfo {
		// Pod Is Pending but have a pod IP should be a valid workload, we should build it ,
		// Such as the pod have initContainer which is initialing.
		// See https://github.com/istio/istio/issues/48854
		if (!IsPodRunning(p) && !IsPodPending(p)) || p.Spec.HostNetwork {
			return nil
		}
		podIP, err := netip.ParseAddr(p.Status.PodIP)
		if err != nil {
			// Is this possible? Probably not in typical case, but anyone could put garbage there.
			return nil
		}
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		// We need to filter from the policies that are present, which apply to us.
		// We only want label selector ones; global ones are not attached to the final WorkloadInfo
		// In general we just take all of the policies
		basePolicies := krt.Fetch(ctx, AuthorizationPolicies, krt.FilterSelects(p.Labels), krt.FilterGeneric(func(a any) bool {
			return a.(model.WorkloadAuthorization).GetLabelSelector() != nil
		}))
		policies := slices.Sort(slices.Map(basePolicies, func(t model.WorkloadAuthorization) string {
			return t.ResourceName()
		}))
		// We could do a non-FilterGeneric but krt currently blows up if we depend on the same collection twice
		auths := fetchPeerAuthentications(ctx, PeerAuths, meshCfg, p.Namespace, p.Labels)
		policies = append(policies, convertedSelectorPeerAuthentications(meshCfg.GetRootNamespace(), auths)...)
		var waypoint *Waypoint
		if p.Labels[constants.ManagedGatewayLabel] != constants.ManagedGatewayMeshControllerLabel {
			// Waypoints do not have waypoints, but anything else does
			waypoint = fetchWaypoint(ctx, Waypoints, Namespaces, p.ObjectMeta)
		}
		var waypointAddress *workloadapi.GatewayAddress
		if waypoint != nil {
			waypointAddress = a.getWaypointAddress(waypoint)
		}
		fo := []krt.FetchOption{krt.FilterNamespace(p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels())}
		if !features.EnableServiceEntrySelectPods {
			fo = append(fo, krt.FilterGeneric(func(a any) bool {
				return a.(model.ServiceInfo).Source == kind.Service
			}))
		}
		services := krt.Fetch(ctx, WorkloadServices, fo...)
		status := workloadapi.WorkloadStatus_HEALTHY
		if !IsPodReady(p) {
			status = workloadapi.WorkloadStatus_UNHEALTHY
		}
		a.networkUpdateTrigger.MarkDependant(ctx) // Mark we depend on out of band a.Network
		network := a.Network(p.Status.PodIP, p.Labels).String()
		w := &workloadapi.Workload{
			Uid:                   a.generatePodUID(p),
			Name:                  p.Name,
			Namespace:             p.Namespace,
			Network:               network,
			ClusterId:             string(a.ClusterID),
			Addresses:             [][]byte{podIP.AsSlice()},
			ServiceAccount:        p.Spec.ServiceAccountName,
			Node:                  p.Spec.NodeName,
			Services:              constructServices(p, services),
			AuthorizationPolicies: policies,
			Status:                status,
			Waypoint:              waypointAddress,
			TrustDomain:           pickTrustDomain(),
		}

		w.WorkloadName, w.WorkloadType = workloadNameAndType(p)
		w.CanonicalName, w.CanonicalRevision = kubelabels.CanonicalService(p.Labels, w.WorkloadName)

		setTunnelProtocol(p.Labels, p.Annotations, w)
		return &model.WorkloadInfo{Workload: w, Labels: p.Labels, Source: kind.Pod, CreationTime: p.CreationTimestamp.Time}
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

func pickTrustDomain() string {
	if td := spiffe.GetTrustDomain(); td != "cluster.local" {
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
		pl := &workloadapi.PortList{}
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
