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

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	"istio.io/api/meta/v1alpha1"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue"
	"istio.io/istio/pkg/activenotifier"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
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
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

type Index interface {
	Lookup(key string) []model.AddressInfo
	All() []model.AddressInfo
	WorkloadsForWaypoint(key model.WaypointKey) []model.WorkloadInfo
	ServicesForWaypoint(key model.WaypointKey) []model.ServiceInfo
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
	networks  networkCollections

	namespaces krt.Collection[model.NamespaceInfo]

	authorizationPolicies krt.Collection[model.WorkloadAuthorization]

	statusQueue *statusqueue.StatusQueue

	SystemNamespace string
	DomainSuffix    string
	ClusterID       cluster.ID
	XDSUpdater      model.XDSUpdater
	Flags           FeatureFlags
	revision        string
	Debugger        *krt.DebugHandler

	stop chan struct{}

	cs                          *multicluster.ClusterStore
	clientBuilder               multicluster.ClientBuilder
	secrets                     krt.Collection[*corev1.Secret]
	remoteClusters              krt.Collection[*multicluster.Cluster]
	meshConfig                  meshwatcher.WatcherCollection
	remoteClientConfigOverrides []func(*rest.Config)
}

type FeatureFlags struct {
	DefaultAllowFromWaypoint              bool
	EnableK8SServiceSelectWorkloadEntries bool
}

type Options struct {
	Client kubeclient.Client

	Revision        string
	SystemNamespace string
	DomainSuffix    string
	ClusterID       cluster.ID
	IsConfigCluster bool
	XDSUpdater      model.XDSUpdater
	StatusNotifier  *activenotifier.ActiveNotifier
	Flags           FeatureFlags

	MeshConfig meshwatcher.WatcherCollection

	Debugger                    *krt.DebugHandler
	ClientBuilder               multicluster.ClientBuilder
	RemoteClientConfigOverrides []func(*rest.Config)
}

func New(options Options) Index {
	a := &index{
		SystemNamespace:             options.SystemNamespace,
		DomainSuffix:                options.DomainSuffix,
		ClusterID:                   options.ClusterID,
		XDSUpdater:                  options.XDSUpdater,
		Debugger:                    options.Debugger,
		Flags:                       options.Flags,
		revision:                    options.Revision,
		clientBuilder:               options.ClientBuilder,
		stop:                        make(chan struct{}),
		cs:                          multicluster.NewClustersStore(),
		remoteClientConfigOverrides: options.RemoteClientConfigOverrides,
	}
	filter := kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}
	configFilter := kclient.Filter{
		ObjectFilter: kubetypes.ComposeFilters(options.Client.ObjectFilter(), a.inRevision),
	}
	opts := krt.NewOptionsBuilder(a.stop, "ambient", options.Debugger)

	a.meshConfig = options.MeshConfig
	// First, we're going to build the collections necessary for multicluster ambient in case that feature flag is turned on.
	// All of these collections need cluster ID metadata stored in krt for merging purposes.

	// TODO: Should this go ahead and transform the full ns into some intermediary with just the details we care about?
	Namespaces := krt.NewInformer[*corev1.Namespace](options.Client, opts.With(
		append(opts.WithName("informer/Namespaces"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)
	authzPolicies := kclient.NewDelayedInformer[*securityclient.AuthorizationPolicy](options.Client,
		gvr.AuthorizationPolicy, kubetypes.StandardInformer, configFilter)
	AuthzPolicies := krt.WrapClient[*securityclient.AuthorizationPolicy](authzPolicies, opts.WithName("informer/AuthorizationPolicies")...)

	peerAuths := kclient.NewDelayedInformer[*securityclient.PeerAuthentication](options.Client,
		gvr.PeerAuthentication, kubetypes.StandardInformer, configFilter)
	PeerAuths := krt.WrapClient[*securityclient.PeerAuthentication](peerAuths, opts.WithName("informer/PeerAuthentications")...)

	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](options.Client, gvr.KubernetesGateway, kubetypes.StandardInformer, filter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, opts.With(
		append(opts.WithName("informer/Gateways"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)

	gatewayClassClient := kclient.NewDelayedInformer[*v1beta1.GatewayClass](options.Client, gvr.GatewayClass, kubetypes.StandardInformer, filter)
	GatewayClasses := krt.WrapClient[*v1beta1.GatewayClass](gatewayClassClient, opts.WithName("informer/GatewayClasses")...)
	Pods := krt.NewInformerFiltered[*corev1.Pod](options.Client, kclient.Filter{
		ObjectFilter:    options.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripPodUnusedFields,
	}, opts.With(
		append(opts.WithName("informer/Pods"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)

	serviceEntries := kclient.NewDelayedInformer[*networkingclient.ServiceEntry](options.Client,
		gvr.ServiceEntry, kubetypes.StandardInformer, configFilter)
	ServiceEntries := krt.WrapClient[*networkingclient.ServiceEntry](serviceEntries, opts.WithName("informer/ServiceEntries")...)

	workloadEntries := kclient.NewDelayedInformer[*networkingclient.WorkloadEntry](options.Client,
		gvr.WorkloadEntry, kubetypes.StandardInformer, configFilter)
	WorkloadEntries := krt.WrapClient[*networkingclient.WorkloadEntry](workloadEntries, opts.WithName("informer/WorkloadEntries")...)

	servicesClient := kclient.NewFiltered[*corev1.Service](options.Client, filter)
	Services := krt.WrapClient[*corev1.Service](servicesClient, opts.With(
		append(opts.WithName("informer/Services"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)
	Nodes := krt.NewInformerFiltered[*corev1.Node](options.Client, kclient.Filter{
		ObjectFilter:    options.Client.ObjectFilter(),
		ObjectTransform: kubeclient.StripNodeUnusedFields,
	}, opts.With(
		append(opts.WithName("informer/Nodes"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)

	EndpointSlices := krt.NewInformerFiltered[*discovery.EndpointSlice](options.Client, kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}, opts.With(
		append(opts.WithName("informer/EndpointSlices"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)

	ConfigMaps := krt.NewInformerFiltered[*corev1.ConfigMap](options.Client, kclient.Filter{
		ObjectFilter: options.Client.ObjectFilter(),
	}, opts.With(
		append(opts.WithName("informer/ConfigMaps"),
			krt.WithMetadata(krt.Metadata{
				multicluster.ClusterKRTMetadataKey: options.ClusterID,
			}),
		)...,
	)...)

	// In the multicluster use-case, we populate the collections with global, dynamically changing data.
	// We only do this if this cluster is the config cluster
	if features.EnableAmbientMultiNetwork && options.IsConfigCluster {
		mcCollections := multicluster.NewRemoteClusterCollections(
			Namespaces,
			Pods,
			Services,
			EndpointSlices,
			Nodes,
			Gateways,
			ConfigMaps,
		)
		LocalCluster := multicluster.NewCluster(
			options.ClusterID,
			options.Client,
			nil,
			nil,
			mcCollections,
		)

		// We can run this in a goroutine because all of the dependent collections will wait for the initial sync
		go LocalCluster.Run(a.meshConfig, a.Debugger)
		a.buildGlobalCollections(
			LocalCluster,
			AuthzPolicies,
			PeerAuths,
			GatewayClasses,
			WorkloadEntries,
			ServiceEntries,
			serviceEntries,
			servicesClient,
			authzPolicies,
			options,
			opts,
			a.remoteClientConfigOverrides...,
		)

		return a
	}

	Networks := buildNetworkCollections(Namespaces, Gateways, options, opts)
	a.networks = Networks
	// N.B Waypoints depends on networks
	Waypoints := a.WaypointsCollection(options.ClusterID, Gateways, GatewayClasses, Pods, opts)

	// AllPolicies includes peer-authentication converted policies
	AuthorizationPolicies, AllPolicies := PolicyCollections(AuthzPolicies, PeerAuths, a.meshConfig, Waypoints, opts, a.Flags)
	AllPolicies.RegisterBatch(PushXds(a.XDSUpdater,
		func(i model.WorkloadAuthorization) model.ConfigKey {
			if i.Authorization == nil {
				return model.ConfigKey{} // nop, filter this out
			}
			return model.ConfigKey{Kind: kind.AuthorizationPolicy, Name: i.Authorization.Name, Namespace: i.Authorization.Namespace}
		}), false)

	// these are workloadapi-style services combined from kube services and service entries
	WorkloadServices := a.ServicesCollection(options.ClusterID, Services, ServiceEntries, Waypoints, Namespaces, opts)

	if features.EnableAmbientStatus {
		serviceEntriesWriter := kclient.NewWriteClient[*networkingclient.ServiceEntry](options.Client)
		servicesWriter := kclient.NewWriteClient[*corev1.Service](options.Client)
		authorizationPoliciesWriter := kclient.NewWriteClient[*securityclient.AuthorizationPolicy](options.Client)

		WaypointPolicyStatus := WaypointPolicyStatusCollection(
			AuthzPolicies,
			Waypoints,
			Services,
			ServiceEntries,
			GatewayClasses,
			a.meshConfig,
			Namespaces,
			opts,
		)
		statusQueue := statusqueue.NewQueue(options.StatusNotifier)
		statusqueue.Register(statusQueue, "istio-ambient-service", WorkloadServices,
			func(info model.ServiceInfo) (kclient.Patcher, map[string]model.Condition) {
				// Since we have 1 collection for multiple types, we need to split these out
				if info.Source.Kind == kind.ServiceEntry {
					return kclient.ToPatcher(serviceEntriesWriter), getConditions(info.Source.NamespacedName, serviceEntries)
				}
				return kclient.ToPatcher(servicesWriter), getConditions(info.Source.NamespacedName, servicesClient)
			})
		statusqueue.Register(statusQueue, "istio-ambient-ztunnel-policy", AuthorizationPolicies,
			func(pol model.WorkloadAuthorization) (kclient.Patcher, map[string]model.Condition) {
				return kclient.ToPatcher(authorizationPoliciesWriter), getConditions(pol.Source.NamespacedName, authzPolicies)
			})
		statusqueue.Register(statusQueue, "istio-ambient-waypoint-policy", WaypointPolicyStatus,
			func(pol model.WaypointPolicyStatus) (kclient.Patcher, map[string]model.Condition) {
				return kclient.ToPatcher(authorizationPoliciesWriter), getConditions(pol.Source.NamespacedName, authzPolicies)
			})
		a.statusQueue = statusQueue
	}

	ServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](WorkloadServices, "serviceAddress", networkAddressFromService)
	ServiceInfosByOwningWaypointHostname := krt.NewIndex(WorkloadServices, "namespaceHostname", func(s model.ServiceInfo) []NamespaceHostname {
		// Filter out waypoint services
		// TODO: we are looking at the *selector* -- we should be looking the labels themselves or something equivalent.
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// Filter out east west gateway services
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayEastWestControllerLabel {
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
	ServiceInfosByOwningWaypointIP := krt.NewIndex(WorkloadServices, "owningWaypointIp", func(s model.ServiceInfo) []networkAddress {
		// Filter out waypoint services
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// Filter out east west gateway services
		if s.LabelSelector.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayEastWestControllerLabel {
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
		PushXdsAddress(a.XDSUpdater, model.ServiceInfo.ResourceName),
	), false)

	NamespacesInfo := krt.NewCollection(Namespaces, func(ctx krt.HandlerContext, i *corev1.Namespace) *model.NamespaceInfo {
		return &model.NamespaceInfo{
			Name:               i.Name,
			IngressUseWaypoint: strings.EqualFold(i.Labels["istio.io/ingress-use-waypoint"], "true"),
		}
	}, opts.WithName("NamespacesInfo")...)

	NodeLocality := NodesCollection(Nodes, opts.WithName("NodeLocality")...)
	Workloads := a.WorkloadsCollection(
		Pods,
		NodeLocality,
		a.meshConfig,
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

	WorkloadAddressIndex := krt.NewIndex[networkAddress, model.WorkloadInfo](Workloads, "networkAddress", networkAddressFromWorkload)
	WorkloadServiceIndex := krt.NewIndex[string, model.WorkloadInfo](Workloads, "service", func(o model.WorkloadInfo) []string {
		return maps.Keys(o.Workload.Services)
	})
	WorkloadWaypointIndexHostname := krt.NewIndex(Workloads, "namespaceHostname", func(w model.WorkloadInfo) []NamespaceHostname {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// Filter out east west gateways
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayEastWestControllerLabel {
			return nil
		}
		waypoint := w.Workload.Waypoint
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
	WorkloadWaypointIndexIP := krt.NewIndex(Workloads, "waypointIp", func(w model.WorkloadInfo) []networkAddress {
		// Filter out waypoints.
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayMeshControllerLabel {
			return nil
		}
		// Filter out east west gateways
		if w.Labels[label.GatewayManaged.Name] == constants.ManagedGatewayEastWestControllerLabel {
			return nil
		}
		waypoint := w.Workload.Waypoint
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
		PushXdsAddress(a.XDSUpdater, model.WorkloadInfo.ResourceName),
	), false)

	if features.EnableIngressWaypointRouting {
		RegisterEdsShim(
			a.XDSUpdater,
			Workloads,
			NamespacesInfo,
			WorkloadServiceIndex,
			WorkloadServices,
			ServiceAddressIndex,
			opts,
		)
	}

	a.namespaces = NamespacesInfo
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

func getConditions[T controllers.ComparableObject](name types.NamespacedName, i kclient.Informer[T]) map[string]model.Condition {
	o := i.Get(name.Name, name.Namespace)
	if controllers.IsNil(o) {
		return nil
	}
	switch t := any(o).(type) {
	case *corev1.Service:
		return translateKubernetesCondition(t.Status.Conditions)
	case *networkingclient.ServiceEntry:
		return translateIstioCondition(t.Status.Conditions)
	case *securityclient.AuthorizationPolicy:
		return translateIstioCondition(t.Status.Conditions)
	default:
		log.Fatalf("unknown type %T; cannot write status", o)
	}
	return nil
}

func translateIstioCondition(conds []*v1alpha1.IstioCondition) map[string]model.Condition {
	res := make(map[string]model.Condition, len(conds))
	for _, cond := range conds {
		c := model.Condition{
			ObservedGeneration: cond.ObservedGeneration,
			Reason:             cond.Reason,
			Message:            cond.Message,
			Status:             cond.Status == string(metav1.ConditionTrue),
		}
		res[cond.Type] = c
	}
	return res
}

func translateKubernetesCondition(conds []metav1.Condition) map[string]model.Condition {
	res := make(map[string]model.Condition, len(conds))
	for _, cond := range conds {
		c := model.Condition{
			ObservedGeneration: cond.ObservedGeneration,
			Reason:             cond.Reason,
			Message:            cond.Message,
			Status:             cond.Status == metav1.ConditionTrue,
		}
		res[cond.Type] = c
	}
	return res
}

// Lookup finds all addresses associated with a given key. Many different key formats are supported; see inline comments.
func (a *index) Lookup(key string) []model.AddressInfo {
	// 1. Workload UID
	if w := a.workloads.GetKey(key); w != nil {
		return []model.AddressInfo{w.AsAddress}
	}

	network, ip, found := strings.Cut(key, "/")
	if !found {
		log.Warnf(`key (%v) did not contain the expected "/" character`, key)
		return nil
	}
	networkAddr := networkAddress{network: network, ip: ip}

	// 2. Workload by IP
	if wls := a.workloads.ByAddress.Lookup(networkAddr); len(wls) > 0 {
		return slices.Map(wls, modelWorkloadToAddressInfo)
	}

	// 3. Service
	if svc := a.lookupService(key); svc != nil {
		res := []model.AddressInfo{svc.AsAddress}
		for _, w := range a.workloads.ByServiceKey.Lookup(svc.ResourceName()) {
			res = append(res, w.AsAddress)
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

func (a *index) inRevision(obj any) bool {
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}
	result := config.LabelsInRevision(object.GetLabels(), a.revision)
	return result
}

// All return all known workloads. Result is un-ordered
func (a *index) All() []model.AddressInfo {
	res := slices.Map(a.workloads.List(), modelWorkloadToAddressInfo)
	for _, s := range a.services.List() {
		res = append(res, s.AsAddress)
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
			for namespacedHostname := range wl.Workload.Services {
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
			if wl.Workload.Node == nodeName {
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

func LookupNetworkGateway(
	ctx krt.HandlerContext,
	id network.ID,
	networkGateways krt.Collection[NetworkGateway],
	gatewaysByNetwork krt.Index[network.ID, NetworkGateway],
) []NetworkGateway {
	return krt.Fetch(ctx, networkGateways, krt.FilterIndex(gatewaysByNetwork, id))
}

func LookupAllNetworkGateway(
	ctx krt.HandlerContext,
	networkGateways krt.Collection[NetworkGateway],
) []NetworkGateway {
	return krt.Fetch(ctx, networkGateways)
}

func (a *index) LookupNetworkGateway(ctx krt.HandlerContext, id network.ID) []NetworkGateway {
	return LookupNetworkGateway(ctx, id, a.networks.NetworkGateways, a.networks.GatewaysByNetwork)
}

func (a *index) LookupAllNetworkGateway(ctx krt.HandlerContext) []NetworkGateway {
	return LookupAllNetworkGateway(ctx, a.networks.NetworkGateways)
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
	return a.services.HasSynced() &&
		a.workloads.HasSynced() &&
		a.waypoints.HasSynced() &&
		a.authorizationPolicies.HasSynced() &&
		a.networks.HasSynced()
}

func (a *index) Network(ctx krt.HandlerContext) network.ID {
	net := krt.FetchOne(ctx, a.networks.LocalSystemNamespace.AsCollection())
	return network.ID(ptr.OrEmpty(net))
}

func PushXds[T any](xds model.XDSUpdater, f func(T) model.ConfigKey) func(events []krt.Event[T]) {
	return func(events []krt.Event[T]) {
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

func PushXdsAddress[T any](xds model.XDSUpdater, f func(T) string) func(events []krt.Event[T]) {
	return func(events []krt.Event[T]) {
		au := sets.New[string]()
		for _, e := range events {
			for _, i := range e.Items() {
				c := f(i)
				if c != "" {
					au.Insert(c)
				}
			}
		}
		if len(au) == 0 {
			return
		}
		cu := sets.NewWithLength[model.ConfigKey](len(au))
		for v := range au {
			cu.Insert(model.ConfigKey{
				Kind: kind.Address,
				Name: v,
			})
		}
		xds.ConfigUpdate(&model.PushRequest{
			Full:             false,
			AddressesUpdated: au,
			ConfigsUpdated:   cu,
			Reason:           model.NewReasonStats(model.AmbientUpdate),
		})
	}
}

type MeshConfig = meshwatcher.MeshConfigResource
