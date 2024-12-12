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
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	"istio.io/api/meta/v1alpha1"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue"
	"istio.io/istio/pkg/activenotifier"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
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
	SyncAll()
	NetworksSynced()
	Run(stop <-chan struct{})
	HasSynced() bool
	model.AmbientIndexes
}

var _ Index = &index{}

type NamespaceHostname struct {
	Namespace string
	Hostname  string
}

func (n NamespaceHostname) String() string {
	return n.Namespace + "/" + n.Hostname
}

type workloadsCollection struct {
	krt.Collection[model.WorkloadInfo]
	ByAddress                krt.Index[networkAddress, model.WorkloadInfo]
	ByServiceKey             krt.Index[string, model.WorkloadInfo]
	ByOwningWaypointHostname krt.Index[NamespaceHostname, model.WorkloadInfo]
	ByOwningWaypointIP       krt.Index[networkAddress, model.WorkloadInfo]
}

type waypointsCollection struct {
	krt.Collection[Waypoint]
}

type servicesCollection struct {
	krt.Collection[model.ServiceInfo]
	ByAddress                krt.Index[networkAddress, model.ServiceInfo]
	ByOwningWaypointHostname krt.Index[NamespaceHostname, model.ServiceInfo]
	ByOwningWaypointIP       krt.Index[networkAddress, model.ServiceInfo]
}

// index maintains an index of ambient WorkloadInfo objects by various keys.
// These are intentionally pre-computed based on events such that lookups are efficient.
type index struct {
	services  servicesCollection
	workloads workloadsCollection
	waypoints waypointsCollection

	authorizationPolicies krt.Collection[model.WorkloadAuthorization]
	networkUpdateTrigger  *krt.RecomputeTrigger

	statusQueue *statusqueue.StatusQueue

	SystemNamespace string
	DomainSuffix    string
	ClusterID       cluster.ID
	XDSUpdater      model.XDSUpdater
	// Network provides a way to lookup which network a given workload is running on
	Network LookupNetwork
	// LookupNetworkGateways provides a function to lookup all the known network gateways in the system.
	LookupNetworkGateways LookupNetworkGateways
	Flags                 FeatureFlags

	stop chan struct{}
}

type FeatureFlags struct {
	DefaultAllowFromWaypoint              bool
	EnableK8SServiceSelectWorkloadEntries bool
}

type Options struct {
	Client kubeclient.Client

	Revision              string
	SystemNamespace       string
	DomainSuffix          string
	ClusterID             cluster.ID
	XDSUpdater            model.XDSUpdater
	LookupNetwork         LookupNetwork
	LookupNetworkGateways LookupNetworkGateways
	StatusNotifier        *activenotifier.ActiveNotifier
	Flags                 FeatureFlags

	Debugger *krt.DebugHandler
}

// KrtOptions is a small wrapper around KRT options to make it easy to provide a common set of options to all collections
// without excessive duplication.
type KrtOptions struct {
	stop     chan struct{}
	debugger *krt.DebugHandler
}

func (k KrtOptions) WithName(n string) []krt.CollectionOption {
	return []krt.CollectionOption{krt.WithDebugging(k.debugger), krt.WithStop(k.stop), krt.WithName(n)}
}

func New(options Options) Index {
	a := &index{
		networkUpdateTrigger: krt.NewRecomputeTrigger(false, krt.WithName("NetworkTrigger")),

		SystemNamespace:       options.SystemNamespace,
		DomainSuffix:          options.DomainSuffix,
		ClusterID:             options.ClusterID,
		XDSUpdater:            options.XDSUpdater,
		Network:               options.LookupNetwork,
		LookupNetworkGateways: options.LookupNetworkGateways,
		Flags:                 options.Flags,
		stop:                  make(chan struct{}),
	}

	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	opts := KrtOptions{
		stop:     a.stop,
		debugger: options.Debugger,
	}
	ConfigMaps := krt.NewInformerFiltered[*v1.ConfigMap](options.Client, filter, opts.WithName("ConfigMaps")...)

	authzPolicies := kclient.NewDelayedInformer[*securityclient.AuthorizationPolicy](options.Client,
		gvr.AuthorizationPolicy, kubetypes.StandardInformer, filter)
	AuthzPolicies := krt.WrapClient[*securityclient.AuthorizationPolicy](authzPolicies, opts.WithName("AuthorizationPolicies")...)

	peerAuths := kclient.NewDelayedInformer[*securityclient.PeerAuthentication](options.Client,
		gvr.PeerAuthentication, kubetypes.StandardInformer, filter)
	PeerAuths := krt.WrapClient[*securityclient.PeerAuthentication](peerAuths, opts.WithName("PeerAuthentications")...)

	serviceEntries := kclient.NewDelayedInformer[*networkingclient.ServiceEntry](options.Client,
		gvr.ServiceEntry, kubetypes.StandardInformer, filter)
	ServiceEntries := krt.WrapClient[*networkingclient.ServiceEntry](serviceEntries, opts.WithName("ServiceEntries")...)

	workloadEntries := kclient.NewDelayedInformer[*networkingclient.WorkloadEntry](options.Client,
		gvr.WorkloadEntry, kubetypes.StandardInformer, filter)
	WorkloadEntries := krt.WrapClient[*networkingclient.WorkloadEntry](workloadEntries, opts.WithName("WorkloadEntries")...)

	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](options.Client, gvr.KubernetesGateway, kubetypes.StandardInformer, filter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, opts.WithName("Gateways")...)

	gatewayClassClient := kclient.NewDelayedInformer[*v1beta1.GatewayClass](options.Client, gvr.GatewayClass, kubetypes.StandardInformer, filter)
	GatewayClasses := krt.WrapClient[*v1beta1.GatewayClass](gatewayClassClient, opts.WithName("GatewayClasses")...)

	servicesClient := kclient.NewFiltered[*v1.Service](options.Client, filter)
	Services := krt.WrapClient[*v1.Service](servicesClient, opts.WithName("Services")...)
	Nodes := krt.NewInformerFiltered[*v1.Node](options.Client, kclient.Filter{
		ObjectFilter:    options.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripNodeUnusedFields,
	}, opts.WithName("Nodes")...)
	Pods := krt.NewInformerFiltered[*v1.Pod](options.Client, kclient.Filter{
		ObjectFilter:    options.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripPodUnusedFields,
	}, opts.WithName("Pods")...)

	// TODO: Should this go ahead and transform the full ns into some intermediary with just the details we care about?
	Namespaces := krt.NewInformer[*v1.Namespace](options.Client, opts.WithName("Namespaces")...)

	EndpointSlices := krt.NewInformerFiltered[*discovery.EndpointSlice](options.Client, kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}, opts.WithName("EndpointSlices")...)

	MeshConfig := MeshConfigCollection(ConfigMaps, options, opts)
	Waypoints := a.WaypointsCollection(Gateways, GatewayClasses, Pods, opts)

	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := PolicyCollections(AuthzPolicies, PeerAuths, MeshConfig, Waypoints, opts, a.Flags)
	AllPolicies.RegisterBatch(PushXds(a.XDSUpdater,
		func(i model.WorkloadAuthorization) model.ConfigKey {
			if i.Authorization == nil {
				return model.ConfigKey{} // nop, filter this out
			}
			return model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace}
		}), false)

	serviceEntriesWriter := kclient.NewWriteClient[*networkingclient.ServiceEntry](options.Client)
	servicesWriter := kclient.NewWriteClient[*v1.Service](options.Client)

	// these are workloadapi-style services combined from kube services and service entries
	WorkloadServices := a.ServicesCollection(Services, ServiceEntries, Waypoints, Namespaces, opts)

	WaypointPolicyStatus := WaypointPolicyStatusCollection(
		AuthzPolicies,
		Waypoints,
		Services,
		ServiceEntries,
		Namespaces,
		opts,
	)

	authorizationPoliciesWriter := kclient.NewWriteClient[*securityclient.AuthorizationPolicy](options.Client)

	if features.EnableAmbientStatus {
		statusQueue := statusqueue.NewQueue(options.StatusNotifier)
		statusqueue.Register(statusQueue, "istio-ambient-service", WorkloadServices, func(info model.ServiceInfo) (kclient.Patcher, []string) {
			// Since we have 1 collection for multiple types, we need to split these out
			if info.Source.Kind == kind.ServiceEntry {
				return kclient.ToPatcher(serviceEntriesWriter), getConditions(info.Source.NamespacedName, serviceEntries)
			}
			return kclient.ToPatcher(servicesWriter), getConditions(info.Source.NamespacedName, servicesClient)
		})
		statusqueue.Register(statusQueue, "istio-ambient-ztunnel-policy", AuthorizationPolicies, func(pol model.WorkloadAuthorization) (kclient.Patcher, []string) {
			return kclient.ToPatcher(authorizationPoliciesWriter), getConditions(pol.Source.NamespacedName, authzPolicies)
		})
		statusqueue.Register(statusQueue, "istio-ambient-waypoint-policy", WaypointPolicyStatus, func(pol model.WaypointPolicyStatus) (kclient.Patcher, []string) {
			return kclient.ToPatcher(authorizationPoliciesWriter), getConditions(pol.Source.NamespacedName, authzPolicies)
		})
		a.statusQueue = statusQueue
	}

	ServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](WorkloadServices, networkAddressFromService)
	ServiceInfosByOwningWaypointHostname := krt.NewIndex(WorkloadServices, func(s model.ServiceInfo) []NamespaceHostname {
		// Filter out waypoint services
		if s.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Service.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetHostname()
		if waypointAddress == nil {
			return nil
		}

		return []NamespaceHostname{{
			Namespace: waypointAddress.Namespace,
			Hostname:  waypointAddress.Hostname,
		}}
	})
	ServiceInfosByOwningWaypointIP := krt.NewIndex(WorkloadServices, func(s model.ServiceInfo) []networkAddress {
		// Filter out waypoint services
		if s.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := s.Service.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetAddress()
		if waypointAddress == nil {
			return nil
		}
		netip, _ := netip.AddrFromSlice(waypointAddress.Address)
		netaddr := networkAddress{
			network: waypointAddress.Network,
			ip:      netip.String(),
		}

		return []networkAddress{netaddr}
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
		Nodes,
		MeshConfig,
		AuthorizationPolicies,
		PeerAuths,
		Waypoints,
		WorkloadServices,
		WorkloadEntries,
		ServiceEntries,
		EndpointSlices,
		Namespaces,
		opts,
	)

	WorkloadAddressIndex := krt.NewIndex[networkAddress, model.WorkloadInfo](Workloads, networkAddressFromWorkload)
	WorkloadServiceIndex := krt.NewIndex[string, model.WorkloadInfo](Workloads, func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Services)
	})
	WorkloadWaypointIndexHostname := krt.NewIndex(Workloads, func(w model.WorkloadInfo) []NamespaceHostname {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		waypoint := w.Waypoint
		if waypoint == nil {
			return nil
		}
		waypointAddress := waypoint.GetHostname()
		if waypointAddress == nil {
			return nil
		}

		return []NamespaceHostname{{
			Namespace: waypointAddress.Namespace,
			Hostname:  waypointAddress.Hostname,
		}}
	})
	WorkloadWaypointIndexIP := krt.NewIndex(Workloads, func(w model.WorkloadInfo) []networkAddress {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
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
		netip, _ := netip.AddrFromSlice(waypointAddress.Address)
		netaddr := networkAddress{
			network: waypointAddress.Network,
			ip:      netip.String(),
		}

		return []networkAddress{netaddr}
	})
	Workloads.RegisterBatch(krt.BatchedEventFilter(
		func(a model.WorkloadInfo) *workloadapi.Workload {
			// Only trigger push if the XDS object changed; the rest is just for computation of others
			return a.Workload
		},
		PushXds(a.XDSUpdater, func(i model.WorkloadInfo) model.ConfigKey {
			return model.ConfigKey{Kind: kind.Address, Name: i.ResourceName()}
		})), false)

	if features.EnableIngressWaypointRouting {
		RegisterEdsShim(
			a.XDSUpdater,
			Workloads,
			WorkloadServiceIndex,
			WorkloadServices,
			ServiceAddressIndex,
			opts,
		)
	}

	a.workloads = workloadsCollection{
		Collection:               Workloads,
		ByAddress:                WorkloadAddressIndex,
		ByServiceKey:             WorkloadServiceIndex,
		ByOwningWaypointHostname: WorkloadWaypointIndexHostname,
		ByOwningWaypointIP:       WorkloadWaypointIndexIP,
	}
	a.services = servicesCollection{
		Collection:               WorkloadServices,
		ByAddress:                ServiceAddressIndex,
		ByOwningWaypointHostname: ServiceInfosByOwningWaypointHostname,
		ByOwningWaypointIP:       ServiceInfosByOwningWaypointIP,
	}
	a.waypoints = waypointsCollection{
		Collection: Waypoints,
	}
	a.authorizationPolicies = AllPolicies

	return a
}

func getConditions[T controllers.ComparableObject](name types.NamespacedName, i kclient.Informer[T]) []string {
	o := i.Get(name.Name, name.Namespace)
	if controllers.IsNil(o) {
		return nil
	}
	switch t := any(o).(type) {
	case *v1.Service:
		return slices.Map(t.Status.Conditions, func(c metav1.Condition) string { return c.Type })
	case *networkingclient.ServiceEntry:
		return slices.Map(t.Status.Conditions, (*v1alpha1.IstioCondition).GetType)
	case *securityclient.AuthorizationPolicy:
		return slices.Map(t.Status.Conditions, (*v1alpha1.IstioCondition).GetType)
	default:
		log.Fatalf("unknown type %T; cannot write status", o)
	}
	return nil
}

// Lookup finds all addresses associated with a given key. Many different key formats are supported; see inline comments.
func (a *index) Lookup(key string) []model.AddressInfo {
	// 1. Workload UID
	if w := a.workloads.GetKey(key); w != nil {
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
		return dedupeWorkloads(wls)
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
	s := a.services.GetKey(key)
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
	res := dedupeWorkloads(a.workloads.List())
	for _, s := range a.services.List() {
		res = append(res, serviceToAddressInfo(s.Service))
	}
	return res
}

func dedupeWorkloads(workloads []model.WorkloadInfo) []model.AddressInfo {
	if len(workloads) <= 1 {
		return slices.Map(workloads, modelWorkloadToAddressInfo)
	}
	res := []model.AddressInfo{}
	seenAddresses := sets.New[netip.Addr]()
	for _, wl := range workloads {
		write := true
		// HostNetwork mode is expected to have overlapping IPs, and tells the data plane to avoid relying on the IP as a unique
		// identifier.
		// For anything else, exclude duplicates.
		if wl.NetworkMode != workloadapi.NetworkMode_HOST_NETWORK {
			for _, addr := range wl.Addresses {
				a := byteIPToAddr(addr)
				if seenAddresses.InsertContains(a) {
					// We have already seen this address. We don't want to include it.
					// We do want to prefer Pods > WorkloadEntry to give precedence to Kubernetes. However, the underlying `a.workloads`
					// already guarantees this, so no need to handle it here.
					write = false
					break
				}
			}
		}
		if write {
			res = append(res, workloadToAddressInfo(wl.Workload))
		}
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
	out := map[string]model.ServiceInfo{}
	for _, host := range key.Hostnames {
		for _, res := range a.services.ByOwningWaypointHostname.Lookup(NamespaceHostname{
			Namespace: key.Namespace,
			Hostname:  host,
		}) {
			name := res.ResourceName()
			if _, f := out[name]; !f {
				out[name] = res
			}
		}
	}

	for _, addr := range key.Addresses {
		for _, res := range a.services.ByOwningWaypointIP.Lookup(networkAddress{
			network: key.Network,
			ip:      addr,
		}) {
			name := res.ResourceName()
			if _, f := out[name]; !f {
				out[name] = res
			}
		}
	}
	// Response is unsorted; it is up to the caller to sort
	return maps.Values(out)
}

func (a *index) WorkloadsForWaypoint(key model.WaypointKey) []model.WorkloadInfo {
	out := map[string]model.WorkloadInfo{}
	for _, host := range key.Hostnames {
		for _, res := range a.workloads.ByOwningWaypointHostname.Lookup(NamespaceHostname{
			Namespace: key.Namespace,
			Hostname:  host,
		}) {
			name := res.ResourceName()
			if _, f := out[name]; !f {
				out[name] = res
			}
		}
	}

	for _, addr := range key.Addresses {
		for _, res := range a.workloads.ByOwningWaypointIP.Lookup(networkAddress{
			network: key.Network,
			ip:      addr,
		}) {
			name := res.ResourceName()
			if _, f := out[name]; !f {
				out[name] = res
			}
		}
	}
	return model.SortWorkloadsByCreationTime(maps.Values(out))
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

func (a *index) NetworksSynced() {
	a.networkUpdateTrigger.MarkSynced()
}

func (a *index) Run(stop <-chan struct{}) {
	if a.statusQueue != nil {
		go func() {
			kubeclient.WaitForCacheSync("ambient-status-queue", stop, a.HasSynced)
			a.statusQueue.Run(stop)
		}()
	}
	<-stop
	close(a.stop)
}

func (a *index) HasSynced() bool {
	return a.services.Synced().HasSynced() &&
		a.workloads.Synced().HasSynced() &&
		a.waypoints.Synced().HasSynced() &&
		a.authorizationPolicies.Synced().HasSynced()
}

type (
	LookupNetwork         func(endpointIP string, labels labels.Instance) network.ID
	LookupNetworkGateways func() []model.NetworkGateway
)

func PushXds[T any](xds model.XDSUpdater, f func(T) model.ConfigKey) func(events []krt.Event[T], initialSync bool) {
	return func(events []krt.Event[T], initialSync bool) {
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, i := range e.Items() {
				c := f(i)
				if c != (model.ConfigKey{}) {
					cu.Insert(c)
				}
			}
		}
		if len(cu) == 0 {
			return
		}
		xds.ConfigUpdate(&model.PushRequest{
			Full:           false,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.AmbientUpdate),
		})
	}
}
