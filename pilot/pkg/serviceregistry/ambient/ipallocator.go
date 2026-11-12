package ambient

import (
	"fmt"
	"net/netip"

	corev1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/ptr"

	"istio.io/istio/pilot/pkg/model"
)

var ipalog = log.RegisterScope("ambient-ipallocator", "ambient IP allocator")

type clusterNetwork struct {
	cluster cluster.ID
	network network.ID
}

func (g clusterNetwork) ResourceName() string {
	return fmt.Sprintf("%s/%s", g.network, g.cluster)
}

func systemNamespaceNetwork(ctx krt.HandlerContext, cluster cluster.ID, systemNamespace string, namespaces krt.Collection[*corev1.Namespace]) *clusterNetwork {
	ns := ptr.Flatten(krt.FetchOne(ctx, namespaces, krt.FilterKey(systemNamespace)))
	if ns == nil {
		return nil
	}
	nw, f := ns.Labels[label.TopologyNetwork.Name]
	if !f {
		return nil
	}
	return &clusterNetwork{
		cluster: cluster,
		network: network.ID(nw),
	}
}

func ClusterNetworksCollection(mc *multicluster.Controller, systemNamespace string, opts krt.OptionsBuilder) krt.Collection[clusterNetwork] {
	configCluster := mc.ConfigCluster()
	localNamespaces := configCluster.Namespaces()

	localClusterNetwork := krt.NewSingleton(func(ctx krt.HandlerContext) *clusterNetwork {
		return systemNamespaceNetwork(ctx, configCluster.ID(), systemNamespace, localNamespaces)
	}, opts.WithName("LocalClusterNetworkMapping")...)

	clusterNetworks := multicluster.NestedCollectionFromLocalAndRemote(
		mc,
		localClusterNetwork.AsCollection(),
		func(ctx krt.HandlerContext, c multicluster.ClusterCollections) *krt.Collection[clusterNetwork] {
			opts := []krt.CollectionOption{
				krt.WithName(fmt.Sprintf("RemoteClusterNetworkMapping[%s]", c.ID())),
				krt.WithDebugging(opts.Debugger()),
				krt.WithStop(c.GetStop()),
			}
			return ptr.Of(krt.NewSingleton(func(ctx krt.HandlerContext) *clusterNetwork {
				return systemNamespaceNetwork(ctx, c.ID(), systemNamespace, c.Namespaces())
			}, opts...).AsCollection())
		},
		"PerClusterNetworksMapping",
		opts)
	return krt.NestedJoinWithMergeCollection(
		clusterNetworks,
		func(ns []clusterNetwork) *clusterNetwork {
			return &ns[0]
		},
		opts.WithName("ClusterNetworksMapping")...)
}

type GlobalService struct {
	service      types.NamespacedName
	cluster      cluster.ID
	network      network.ID
	serviceScope model.ServiceScope
}

func (g GlobalService) ResourceName() string {
	return fmt.Sprintf("%s/%s/%s/%s", g.network, g.cluster, g.service.Namespace, g.service.Name)
}

func (g GlobalService) String() string {
	return fmt.Sprintf("%s/%s/%s", g.network, g.service.Namespace, g.service.Name)
}

func clusterGlobalServices(ctx krt.HandlerContext, cluster cluster.ID, clusterNetworkIndex krt.Index[cluster.ID, clusterNetwork], meshConfig krt.Singleton[MeshConfig], namespaces krt.Collection[*corev1.Namespace], svc *corev1.Service) *GlobalService {
	serviceScope := model.Local
	meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
	if meshCfg != nil {
		serviceScope = matchServiceScope(ctx, meshCfg, namespaces, svc)
	}
	clusterNetworks := clusterNetworkIndex.Fetch(ctx, cluster)
	if len(clusterNetworks) == 0 {
		return nil
	}
	return &GlobalService{
		service:      types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name},
		cluster:      cluster,
		network:      clusterNetworks[0].network,
		serviceScope: serviceScope,
	}
}

func GlobalServicesCollection(mc *multicluster.Controller, meshConfig krt.Singleton[MeshConfig], clusterNetworks krt.Collection[clusterNetwork], opts krt.OptionsBuilder) krt.Collection[GlobalService] {
	clusterNetworkIndex := krt.NewIndex(clusterNetworks, "clusterNetworkIndex", func(o clusterNetwork) []cluster.ID {
		return []cluster.ID{o.cluster}
	})

	configCluster := mc.ConfigCluster()
	localClusterNamespaces := configCluster.Namespaces()
	localClusterGlobalServices := krt.NewCollection(configCluster.Services(), func(ctx krt.HandlerContext, svc *corev1.Service) *GlobalService {
		return clusterGlobalServices(ctx, configCluster.ID(), clusterNetworkIndex, meshConfig, localClusterNamespaces, svc)
	}, append(
		opts.WithName("LocalClusterGlobalServices"),
		krt.WithMetadata(krt.Metadata{
			multicluster.ClusterKRTMetadataKey: configCluster.ID(),
		}),
	)...)

	globalServices := multicluster.NestedCollectionFromLocalAndRemote(
		mc,
		localClusterGlobalServices,
		func(ctx krt.HandlerContext, c multicluster.ClusterCollections) *krt.Collection[GlobalService] {
			opts := []krt.CollectionOption{
				krt.WithName(fmt.Sprintf("RemoteClusterNetworkMapping[%s]", c.ID())),
				krt.WithDebugging(opts.Debugger()),
				krt.WithStop(c.GetStop()),
			}

			return ptr.Of(krt.NewCollection(c.Services(), func(ctx krt.HandlerContext, svc *corev1.Service) *GlobalService {
				return clusterGlobalServices(ctx, c.ID(), clusterNetworkIndex, meshConfig, c.Namespaces(), svc)
			}, opts...))
		},
		"PerClusterGlobalServices",
		opts)
	return krt.NestedJoinWithMergeCollection(
		globalServices,
		func(gs []GlobalService) *GlobalService {
			return &gs[0]
		},
		opts.WithName("GlobalServices")...)
}

// When do we allocate a new IP address for the service?
//
// We allocate a new IP address when all of the following is true:
// * There is a service definition with a global service scope
// * There is no service definition on the local network
//
//
// When do we release an IP address we allocated for a service?
//
// We release an IP address when all of the following is true:
// * There is no service definitions for the service in any cluster
//
//
// How do we trigger those conditions based on the collection returned by
// GlobalServicesCollection?
//
// * When a new service added/updated we check:
//    - is the service scope global?
//    - is the service on a remote network?
//    - did we already allocate an IP address for this service?
// * When a service get deleted we check:
//    - is it the last service definition?
//
// Things we still need to do:
// - We need a way to check if an IPAddress has been created for a service

type allocator struct {
	cidrIndex   map[netip.Prefix]int
	cidrs       []netip.Prefix
	currentCidr int
	nextAddress netip.Addr
}

func (a *allocator) empty() bool {
	return len(a.cidrs) == 0
}

func (a *allocator) next() netip.Addr {
	for i := 0; i < len(a.cidrs); i++ {
		c := a.cidrs[a.currentCidr]
		if a.nextAddress.IsValid() && c.Contains(a.nextAddress) {
			addr := a.nextAddress
			a.nextAddress = a.nextAddress.Next()
			return addr
		}
		a.nextCidr()
	}
	return netip.Addr{}
}

func (a *allocator) add(cidr netip.Prefix) {
	if !cidr.IsValid() {
		return
	}
	if _, exists := a.cidrIndex[cidr]; exists {
		return
	}
	a.cidrs = append(a.cidrs, cidr)
	a.cidrIndex[cidr] = len(a.cidrs) - 1
	if len(a.cidrs) == 1 {
		a.currentCidr = 0
		a.nextAddress = cidr.Addr().Next()
	}
}

func (a *allocator) nextCidr() {
	if len(a.cidrs) > 0 {
		a.currentCidr = (a.currentCidr + 1) % len(a.cidrs)
		c := a.cidrs[a.currentCidr]
		a.nextAddress = c.Addr().Next()
	}
}

func (a *allocator) remove(cidr netip.Prefix) {
	if !cidr.IsValid() {
		return
	}

	ci, exists := a.cidrIndex[cidr]
	if !exists {
		return
	}

	last := len(a.cidrs) - 1
	removedCurrent := a.currentCidr == ci
	a.cidrs[ci] = a.cidrs[last]
	a.cidrs = a.cidrs[:last]
	delete(a.cidrIndex, cidr)

	// If we have no CIDRs left
	if len(a.cidrIndex) == 0 {
		a.nextAddress = netip.Addr{}
		a.currentCidr = 0
		return
	}

	if ci < len(a.cidrs) {
		a.cidrIndex[a.cidrs[ci]] = ci
	}

	if removedCurrent {
		a.currentCidr = ci % len(a.cidrs)
		a.nextAddress = a.cidrs[a.currentCidr].Addr().Next()
	} else if a.currentCidr == last {
		a.currentCidr = ci
	}
}

type IPAllocator struct {
	ipAddressCollection   krt.Collection[*networking.IPAddress]
	ipAddressWriter       kclient.Writer[*networking.IPAddress]
	serviceCidrCollection krt.Collection[*networking.ServiceCIDR]
	v4Allocator           allocator
	v6Allocator           allocator
	usedAddresses         map[netip.Addr]bool
	globalServices        krt.Collection[GlobalService]
	serviceAddressIndex   krt.Index[GlobalService, *networking.IPAddress]
}

func NewIPAllocator(client kube.Client, stop <-chan struct{}, mc *multicluster.Controller, meshConfig krt.Singleton[MeshConfig]) *IPAllocator {
	if client == nil {
		return nil
	}

	configCluster := mc.ConfigCluster()
	localNamespaces := configCluster.Namespaces()
	localClusterNetwork := krt.NewSingleton(func(ctx krt.HandlerContext) *clusterNetwork {
		return systemNamespaceNetwork(ctx, configCluster.ID(), meshConfig.Get().RootNamespace, localNamespaces)
	}, krt.WithName("LocalClusterNetwork"))
	clusterNetworks := ClusterNetworksCollection(mc, "istio-system", krt.NewOptionsBuilder(stop, "ClusterNetworks", nil))

	ipallocator := IPAllocator{
		ipAddressCollection:   krt.WrapClient(kclient.New[*networking.IPAddress](client), krt.WithStop(stop)),
		ipAddressWriter:       kclient.NewWriteClient[*networking.IPAddress](client),
		serviceCidrCollection: krt.WrapClient(kclient.New[*networking.ServiceCIDR](client), krt.WithStop(stop)),
		v4Allocator:           allocator{cidrIndex: make(map[netip.Prefix]int)},
		v6Allocator:           allocator{cidrIndex: make(map[netip.Prefix]int)},
		usedAddresses:         make(map[netip.Addr]bool),
		globalServices:        GlobalServicesCollection(mc, meshConfig, clusterNetworks, krt.NewOptionsBuilder(stop, "GlobalServices", nil)),
	}

	ipallocator.globalServices.RegisterBatch(func(events []krt.Event[GlobalService]) {
		ipalog.Debugf("GlobalServices updated: %v", events)
		for _, e := range events {
			switch e.Event {
			case controllers.EventAdd:
				ipallocator.addGlobalService(e.New, localClusterNetwork)
			case controllers.EventUpdate:
				ln := localClusterNetwork.Get()
				if e.Old.network == ln.network {
					continue
				}
				if e.Old.serviceScope == model.Global && e.New.serviceScope == model.Local {
					ipallocator.removeGlobalService(e.Old)
				} else if e.New.serviceScope == model.Global && e.Old.serviceScope == model.Local {
					ipallocator.addGlobalService(e.New, localClusterNetwork)
				}
			case controllers.EventDelete:
				ipallocator.removeGlobalService(e.Old)
			}
		}
	}, false)

	ipallocator.ipAddressCollection.RegisterBatch(func(events []krt.Event[*networking.IPAddress]) {
		ipalog.Debugf("IPAddress updated")
		for _, e := range events {
			switch e.Event {
			case controllers.EventAdd:
				fallthrough
			case controllers.EventUpdate:
				addr, err := netip.ParseAddr((*e.New).Name)
				if err != nil {
					ipalog.Debugf("can't parse IP address %s", (*e.New).Name)
					continue
				}
				ipallocator.usedAddresses[addr] = true
			case controllers.EventDelete:
				addr, err := netip.ParseAddr((*e.New).Name)
				if err != nil {
					ipalog.Debugf("can't parse IP address %s", (*e.New).Name)
					continue
				}
				delete(ipallocator.usedAddresses, addr)
			}
		}
	}, false)

	ipallocator.serviceCidrCollection.RegisterBatch(func(events []krt.Event[*networking.ServiceCIDR]) {
		ipalog.Debugf("ServiceCIDR updated")
		for _, e := range events {
			switch e.Event {
			case controllers.EventAdd:
				ipallocator.addServiceCIDR(*e.New)
			case controllers.EventDelete:
				ipallocator.removeServiceCIDR(*e.Old)
				// TODO: what to do with IPs that were
				// allocated from this CIDR
			case controllers.EventUpdate:
				ipallocator.removeServiceCIDR(*e.Old)
				ipallocator.addServiceCIDR(*e.New)
			}
		}
	}, false)

	serviceAddressIndex := krt.NewIndex(ipallocator.ipAddressCollection, "serviceAddressIndex", func(address *networking.IPAddress) []GlobalService {
		labels := address.Labels
		if labels[networking.LabelManagedBy] != "remote-service-controller.istio.io" {
			return nil
		}
		return []GlobalService{{
			service: types.NamespacedName{
				Namespace: labels["networking.istio.io/remote-ns"],
				Name:      labels["networking.istio.io/remote-service"],
			},
			network: network.ID(labels["networking.istio.io/remote-network"]),
		}}
	})
	ipallocator.serviceAddressIndex = serviceAddressIndex

	return &ipallocator
}

func (a *IPAllocator) addGlobalService(service *GlobalService, localClusterNetwork krt.Singleton[clusterNetwork]) {
	localNetwork := localClusterNetwork.Get()
	if service.network == localNetwork.network || service.serviceScope != model.Global {
		return
	}
	currentIPs := a.serviceAddressIndex.Lookup(*service)
	if len(currentIPs) > 0 {
		return
	}
	a.allocate(service)
}

func (a *IPAllocator) removeGlobalService(service *GlobalService) {

}

func (a *IPAllocator) addServiceCIDR(serviceCidr *networking.ServiceCIDR) {
	for _, cidr := range serviceCidr.Spec.CIDRs {
		prefix, e := netip.ParsePrefix(cidr)
		if e != nil || !prefix.IsValid() {
			continue
		}
		if prefix.Addr().Is4() {
			a.v4Allocator.add(prefix)
		} else {
			a.v6Allocator.add(prefix)
		}
	}
}

func (a *IPAllocator) removeServiceCIDR(serviceCidr *networking.ServiceCIDR) {
	for _, c := range serviceCidr.Spec.CIDRs {
		cidr, err := netip.ParsePrefix(c)
		if err != nil || !cidr.IsValid() {
			continue
		}
		a.v4Allocator.remove(cidr)
		a.v6Allocator.remove(cidr)
	}
}

func (a *IPAllocator) allocateFrom(pool *allocator, g *GlobalService) error {
	var first netip.Addr
	for {
		addr := pool.next()
		if !addr.IsValid() {
			return fmt.Errorf("ip address range exhausted")
		}
		if first.IsValid() && addr == first {
			return fmt.Errorf("all valid ip addresses have been allocated")
		}
		if !first.IsValid() {
			first = addr
		}
		if a.usedAddresses[addr] {
			continue
		}
		_, err := a.ipAddressWriter.Create(&networking.IPAddress{
			ObjectMeta: metav1.ObjectMeta{
				Name: addr.String(),
				Labels: map[string]string{
					networking.LabelManagedBy:            "remote-service-controller.istio.io",
					"networking.istio.io/remote-ns":      g.service.Namespace,
					"networking.istio.io/remote-service": g.service.Name,
					"networking.istio.io/remote-network": string(g.network),
				},
			},
			Spec: networking.IPAddressSpec{
				// An invalid parent for now...
				ParentRef: &networking.ParentReference{
					Group:     "",
					Resource:  "services",
					Namespace: g.service.Namespace,
					Name:      g.service.Name,
				},
			},
		})
		if apierrors.IsAlreadyExists(err) {
			a.usedAddresses[addr] = true
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to create IPAddress %s: %w", addr, err)
		}
		a.usedAddresses[addr] = true
		return nil
	}
}

func (a *IPAllocator) allocate(g *GlobalService) {
	if !a.v4Allocator.empty() {
		if err := a.allocateFrom(&a.v4Allocator, g); err != nil {
			ipalog.Warnf("failed to allocate IPv4 address: %v", err)
		}
	}
	if !a.v6Allocator.empty() {
		if err := a.allocateFrom(&a.v6Allocator, g); err != nil {
			ipalog.Warnf("failed to allocate IPv6 address: %v", err)
		}
	}
}

func (a *IPAllocator) release(g *GlobalService) {
	ipaddrs := a.serviceAddressIndex.Lookup(*g)
	if len(ipaddrs) == 0 {
		return
	}
	for _, ipaddr := range ipaddrs {
		addr, e := netip.ParseAddr(ipaddr.Name)
		if e != nil || !addr.IsValid() {
			continue
		}
		if err := a.ipAddressWriter.Delete(ipaddr.Name, ""); err != nil && !apierrors.IsNotFound(err) {
			ipalog.Warnf("failed to delete IPAddress %s: %v", ipaddr.Name, err)
			continue
		}
	}
}
