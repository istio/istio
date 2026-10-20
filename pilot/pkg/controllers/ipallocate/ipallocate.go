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

package ipallocate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8snetworkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/api/label"
	meshapi "istio.io/api/mesh/v1alpha1"
	apiv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	autoallocate "istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pkg/config"
	cfghost "istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh/labelselector"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvr"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/multicluster"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("ip-autoallocate", "IP autoallocate controller")

type IPAllocator struct {
	serviceEntryClient kclient.Informer[*networkingv1.ServiceEntry]
	serviceEntryWriter kclient.Writer[*networkingv1.ServiceEntry]
	index              kclient.Index[netip.Addr, *networkingv1.ServiceEntry]
	ipAddressClient    kclient.Informer[*k8snetworkingv1.IPAddress]
	ipAddressWriter    kclient.Writer[*k8snetworkingv1.IPAddress]
	remoteAddressIndex kclient.Index[remoteServiceKey, *k8snetworkingv1.IPAddress]
	stopChan           <-chan struct{}
	queue              controllers.Queue
	multicluster       *multicluster.Controller
	remoteServices     krt.Collection[remoteService]
	serviceClient      typedcorev1.ServiceInterface
	systemNamespace    string

	// This controller is not safe for concurrency but it exists outside the critical path performing important but minimal functionality in a single thread.
	// If we want the multi-thread this controller we must add locking around accessing the allocators which would otherwise be racy
	v4allocator *prefixUse
	v6allocator *prefixUse
}

// HasSynced returns true when the controller has completed initial sync:
// the informer cache is populated, warming is done, and the queue is processing events.
func (c *IPAllocator) HasSynced() bool {
	return c.queue.HasSynced()
}

// Unfortunately slices are not comparable which is required by kube client-go workqueue sets.
// We want to be able to record multiple addresses as being in conflict to handle dual stack smoothly though.
// Workaround is a conversion of []netip.Addr to a string which is comparable but requires some helper
// functions to be in any way reasonable.
type conflictDetectedEvent struct {
	conflictingResourceIdentifier types.NamespacedName
	conflictingAddresses          string
}

const conflictDetectedEventDelim = `,`

func (event conflictDetectedEvent) getAddresses() []netip.Addr {
	addresses := []netip.Addr{}
	for _, a := range strings.Split(event.conflictingAddresses, conflictDetectedEventDelim) {
		parsedAddress, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		addresses = append(addresses, parsedAddress)
	}
	return addresses
}

func newConflictDetectedEvent(id types.NamespacedName, addresses []netip.Addr) conflictDetectedEvent {
	var conflictingAddresses string
	if len(addresses) > 0 {
		conflictingAddresses = addresses[0].String()
		for _, a := range addresses[1:] {
			conflictingAddresses += conflictDetectedEventDelim
			conflictingAddresses += a.String()
		}
	}
	return conflictDetectedEvent{
		conflictingResourceIdentifier: id,
		conflictingAddresses:          conflictingAddresses,
	}
}

const (
	controllerName                = "IP Autoallocator"
	allocationParentServiceName   = "istio-ip-allocator"
	originalServiceNameLabel      = "istio.io/original-service-name"
	originalServiceNamespaceLabel = "istio.io/original-service-namespace"
	originalNetworkAnnotation     = "istio.io/original-network"
)

type remoteServiceKey struct {
	network network.ID
	service types.NamespacedName
}

func (k remoteServiceKey) resourceName() string {
	return fmt.Sprintf("%s/%s/%s", k.network, k.service.Namespace, k.service.Name)
}

func (k remoteServiceKey) String() string {
	return k.resourceName()
}

type remoteService struct {
	key     remoteServiceKey
	service *corev1.Service
	network network.ID
}

func (s remoteService) ResourceName() string {
	return s.key.resourceName()
}

func isGlobalService(
	ctx krt.HandlerContext,
	meshConfig krt.Singleton[meshwatcher.MeshConfigResource],
	namespaces krt.Collection[*corev1.Namespace],
	service *corev1.Service,
) bool {
	meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
	if meshCfg == nil {
		return false
	}
	namespace := krt.FetchOne(ctx, namespaces, krt.FilterKey(service.Namespace))
	if namespace == nil || *namespace == nil {
		log.Warnf("namespace %s not found for service %s/%s", service.Namespace, service.Namespace, service.Name)
		return false
	}
	namespaceLabels := klabels.Set((*namespace).Labels)
	serviceLabels := klabels.Set(service.Labels)
	for _, scopeConfig := range meshCfg.ServiceScopeConfigs {
		namespaceSelector, err := labelselector.LabelSelectorAsSelector(scopeConfig.NamespaceSelector)
		if err != nil {
			log.Warnf("failed to convert namespace selector: %v", err)
			continue
		}
		if namespaceSelector.String() == klabels.Nothing().String() {
			namespaceSelector = klabels.Everything()
		}
		serviceSelector, err := labelselector.LabelSelectorAsSelector(scopeConfig.ServicesSelector)
		if err != nil {
			log.Warnf("failed to convert service selector: %v", err)
			continue
		}
		if serviceSelector.String() == klabels.Nothing().String() {
			serviceSelector = klabels.Everything()
		}
		if namespaceSelector.Matches(namespaceLabels) && serviceSelector.Matches(serviceLabels) &&
			scopeConfig.GetScope() == meshapi.MeshConfig_ServiceScopeConfigs_GLOBAL {
			return true
		}
	}
	return false
}

func NewIPAllocator(
	stop <-chan struct{},
	c kubelib.Client,
	multiclusterController *multicluster.Controller,
	meshConfig krt.Singleton[meshwatcher.MeshConfigResource],
	systemNamespace string,
) *IPAllocator {
	clusters := multiclusterController.Clusters()
	localNamespaces := multiclusterController.ConfigCluster().Namespaces()
	client := kclient.NewDelayedInformer[*networkingv1.ServiceEntry](c, gvr.ServiceEntry, kubetypes.StandardInformer, kclient.Filter{
		ObjectFilter: c.ObjectFilter(),
	})
	writer := kclient.NewWriteClient[*networkingv1.ServiceEntry](c)
	ipAddressClient := kclient.New[*k8snetworkingv1.IPAddress](c)
	ipAddressWriter := kclient.NewWriteClient[*k8snetworkingv1.IPAddress](c)
	remoteAddressIndex := kclient.CreateIndex[remoteServiceKey, *k8snetworkingv1.IPAddress](
		ipAddressClient,
		"remoteService",
		func(address *k8snetworkingv1.IPAddress) []remoteServiceKey {
			name := address.Labels[originalServiceNameLabel]
			namespace := address.Labels[originalServiceNamespaceLabel]
			remoteNetwork, hasNetwork := address.Annotations[originalNetworkAnnotation]
			if name == "" || namespace == "" || !hasNetwork {
				return nil
			}
			return []remoteServiceKey{{
				network: network.ID(remoteNetwork),
				service: types.NamespacedName{Namespace: namespace, Name: name},
			}}
		},
	)
	index := kclient.CreateIndex[netip.Addr, *networkingv1.ServiceEntry](client, "address", func(serviceentry *networkingv1.ServiceEntry) []netip.Addr {
		addresses := autoallocate.GetAddressesFromServiceEntry(serviceentry)
		for _, addr := range serviceentry.Spec.Addresses {
			a, err := netip.ParseAddr(addr)
			if err != nil {
				continue
			}
			addresses = append(addresses, a)
		}
		return addresses
	})
	allocator := &IPAllocator{
		serviceEntryClient: client,
		serviceEntryWriter: writer,
		index:              index,
		ipAddressClient:    ipAddressClient,
		ipAddressWriter:    ipAddressWriter,
		remoteAddressIndex: remoteAddressIndex,
		stopChan:           stop,
		multicluster:       multiclusterController,
		serviceClient:      c.Kube().CoreV1().Services(systemNamespace),
		systemNamespace:    systemNamespace,
		// MustParsePrefix is OK because these are const. If we allow user configuration we must not use this function.
		v4allocator: newPrefixUse(netip.MustParsePrefix(features.IPAutoallocateIPv4Prefix)),
		v6allocator: newPrefixUse(netip.MustParsePrefix(features.IPAutoallocateIPv6Prefix)),
	}
	allocator.queue = controllers.NewQueue(controllerName, controllers.WithGenericReconciler(allocator.reconcile), controllers.WithMaxAttempts(5))
	remoteServiceCollections := krt.NewManyCollection(clusters, func(ctx krt.HandlerContext, remoteCluster *multicluster.Cluster) []krt.Collection[remoteService] {
		remoteNetwork := network.ID("")
		if namespace := krt.FetchOne(ctx, localNamespaces, krt.FilterKey(systemNamespace)); namespace != nil && *namespace != nil {
			var localNetwork = network.ID((*namespace).Labels[label.TopologyNetwork.Name])
			if namespace := krt.FetchOne(ctx, remoteCluster.Namespaces(), krt.FilterKey(systemNamespace)); namespace != nil && *namespace != nil {
				remoteNetwork = network.ID((*namespace).Labels[label.TopologyNetwork.Name])
				if localNetwork == remoteNetwork || remoteNetwork.String() == "" {
					return nil
				}
			}
		}
		remoteNamespaces := remoteCluster.Namespaces()
		services := krt.NewCollection(remoteCluster.Services(), func(ctx krt.HandlerContext, service *corev1.Service) *remoteService {
			if service.Namespace == systemNamespace && service.Name == allocationParentServiceName {
				return nil
			}
			if !isGlobalService(ctx, meshConfig, remoteNamespaces, service) {
				log.Debugf("%s is not a global service", service.Name)
				return nil
			}
			return &remoteService{
				key: remoteServiceKey{
					network: remoteNetwork,
					service: config.NamespacedName(service),
				},
				service: service,
				network: remoteNetwork,
			}
		}, krt.WithName(fmt.Sprintf("IPAllocatorServices[%s]", remoteCluster.ID)), krt.WithStop(remoteCluster.GetStop()))
		return []krt.Collection[remoteService]{services}
	}, krt.WithName("IPAllocatorRemoteServiceCollections"), krt.WithStop(stop))
	allocator.remoteServices = krt.NestedJoinWithMergeCollection(remoteServiceCollections, func(services []remoteService) *remoteService {
		if len(services) == 0 {
			return nil
		}
		return &services[0]
	}, krt.WithName("IPAllocatorRemoteServices"), krt.WithStop(stop))
	allocator.remoteServices.Register(func(event krt.Event[remoteService]) {
		allocator.queue.Add(event.Latest().key)
	})
	client.AddEventHandler(controllers.ObjectHandler(allocator.queue.AddObject))
	return allocator
}

func (c *IPAllocator) Run(stop <-chan struct{}) {
	log.Debugf("starting %s controller", controllerName)
	defer c.serviceEntryClient.ShutdownHandlers()
	defer c.ipAddressClient.ShutdownHandlers()
	if err := wait.PollUntilContextCancel(wait.ContextForChannel(stop), time.Second, true, func(ctx context.Context) (bool, error) {
		if err := c.ensureAllocationParentService(ctx); err != nil {
			log.Errorf("failed to ensure IP allocation parent Service: %v; retrying", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		log.Debugf("stopping %s controller before initialization completed: %v", controllerName, err)
		c.queue.ShutDownEarly()
		return
	}
	kubelib.WaitForCacheSync(controllerName, stop, c.serviceEntryClient.HasSynced, c.ipAddressClient.HasSynced, c.remoteServices.HasSynced)
	// it is important that we always warm cache before we try to run the queue
	// failing to warm cache first could result in allocating IP which are already in use
	c.populateControllerDatastructures()
	// logically the v1 behavior is a state of the world function which cannot be replicated in reconcile
	// if we wish to replicate v1 assignment behavior for existing serviceentry it should go here and must
	// include a change to respect the used IPs from the warmed cache begin reconcile
	c.queue.Run(stop)
}

func (c *IPAllocator) ensureAllocationParentService(ctx context.Context) error {
	if _, err := c.serviceClient.Get(ctx, allocationParentServiceName, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting Service %s/%s: %w", c.systemNamespace, allocationParentServiceName, err)
	}

	// This Service only anchors parent references; it must not allocate a ClusterIP or select workloads.
	_, err := c.serviceClient.Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      allocationParentServiceName,
			Namespace: c.systemNamespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
		},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating Service %s/%s: %w", c.systemNamespace, allocationParentServiceName, err)
	}
	return nil
}

// populateControllerDatastructures reads all serviceentries and records any addresses in our ranges as in use
func (c *IPAllocator) populateControllerDatastructures() {
	serviceentries := c.serviceEntryClient.List(metav1.NamespaceAll, klabels.Everything())
	count := 0
	for _, serviceentry := range serviceentries {
		count++
		owner := config.NamespacedName(serviceentry)
		log.Debugf("checking SE with addresses %s", autoallocate.GetAddressesFromServiceEntry(serviceentry))
		c.checkInSpecAddresses(serviceentry)
		c.markUsedOrQueueConflict(autoallocate.GetAddressesFromServiceEntry(serviceentry), owner)
	}
	for _, address := range c.ipAddressClient.List(metav1.NamespaceAll, klabels.Everything()) {
		ip, err := netip.ParseAddr(address.Name)
		if err != nil {
			continue
		}
		// can use config.NamespacedName here
		owner := types.NamespacedName{Name: "IPAddress/" + address.Name}
		key := remoteServiceKey{
			network: network.ID(address.Annotations[originalNetworkAnnotation]),
			service: types.NamespacedName{
				Namespace: address.Labels[originalServiceNamespaceLabel],
				Name:      address.Labels[originalServiceNameLabel],
			},
		}
		if addresses := c.remoteAddressIndex.Lookup(key); len(addresses) > 0 {
			owner = remoteServiceOwner(key)
		}
		c.markUsedOrQueueConflict([]netip.Addr{ip}, owner)
	}

	log.Debugf("discovered %v during warming", count)
}

func (c *IPAllocator) reconcile(a any) error {
	log.Debugf("reconciling %s", a)
	if nn, ok := a.(types.NamespacedName); ok {
		return c.reconcileServiceEntry(nn)
	}
	if key, ok := a.(remoteServiceKey); ok {
		return c.reconcileRemoteService(key)
	}
	if conflict, ok := a.(conflictDetectedEvent); ok {
		return c.resolveConflict(conflict)
	}

	return nil
}

func (c *IPAllocator) reconcileRemoteService(key remoteServiceKey) error {
	log.Infof("handling remote service %s from network %s", key.service, key.network)
	service := c.remoteServices.GetKey(key.resourceName())
	if service == nil {
		return c.deleteRemoteServiceAddresses(key)
	}
	// try to allocate
	return c.allocateRemoteServiceAddresses(key)
}

func remoteServiceOwner(key remoteServiceKey) types.NamespacedName {
	return types.NamespacedName{Namespace: string(key.network), Name: key.service.String()}
}

func (c *IPAllocator) allocateRemoteServiceAddresses(key remoteServiceKey) error {
	for _, address := range c.remoteAddressIndex.Lookup(key) {
		ip, err := netip.ParseAddr(address.Name)
		if err == nil && ip.Is4() {
			return nil
		}
	}

	owner := remoteServiceOwner(key)
	address, err := c.v4allocator.allocateNext(owner)
	if err != nil {
		return fmt.Errorf("allocating IPv4 address for remote service %s: %v", key, err)
	}

	resource := &k8snetworkingv1.IPAddress{
		ObjectMeta: metav1.ObjectMeta{
			Name: address.String(),
			Labels: map[string]string{
				originalServiceNameLabel:      key.service.Name,
				originalServiceNamespaceLabel: key.service.Namespace,
			},
			Annotations: map[string]string{originalNetworkAnnotation: string(key.network)},
		},
		Spec: k8snetworkingv1.IPAddressSpec{ParentRef: &k8snetworkingv1.ParentReference{
			Resource:  "services",
			Namespace: c.systemNamespace,
			Name:      allocationParentServiceName,
		}},
	}
	if _, err := c.ipAddressWriter.Create(resource); err != nil {
		c.freeAddresses([]netip.Addr{address}, owner)
		return fmt.Errorf("creating IPAddress %s for remote service %s: %v", address, key, err)
	}
	return nil
}

func (c *IPAllocator) deleteRemoteServiceAddresses(key remoteServiceKey) error {
	owner := remoteServiceOwner(key)
	var errs error
	for _, address := range c.remoteAddressIndex.Lookup(key) {
		if err := controllers.IgnoreNotFound(c.ipAddressWriter.Delete(address.Name, "")); err != nil {
			errs = errors.Join(errs, fmt.Errorf("deleting IPAddress %s for remote service %s: %v", address.Name, key, err))
			continue
		}
		if ip, err := netip.ParseAddr(address.Name); err == nil {
			c.freeAddresses([]netip.Addr{ip}, owner)
		}
	}
	return errs
}

func (c *IPAllocator) reconcileServiceEntry(se types.NamespacedName) error {
	log := log.WithLabels("service entry", se)
	log.Debugf("reconciling")
	serviceentry := c.serviceEntryClient.Get(se.Name, se.Namespace)
	if serviceentry == nil {
		log.Debugf("not found, no action required")
		// probably a delete so we should remove ips from our addresses most likely
		// TODO: we never actually remove IP right now, likely this should be done a little more slowly anyway to prevent reuse if we are too fast
		return nil
	}

	if !autoallocate.ShouldV2AutoAllocateIP(serviceentry) {
		// we may have an address in our range so we should check and record it
		c.checkInSpecAddresses(serviceentry)
		if len(serviceentry.Status.Addresses) > 0 {
			log.Debugf("allocation no longer required for %v, clearing stale addresses", config.NamespacedName(serviceentry))
			return c.clearStaleAddresses(serviceentry)
		}
		log.Debugf("allocation not required")
		return nil
	}

	replaceAddresses, addStatusAndAddresses, newlyAllocated, err := c.statusPatchForAddresses(serviceentry, false)
	if err != nil {
		return err
	}

	if replaceAddresses == nil {
		log.Debugf("no change needed")
		return nil // nothing to patch
	}

	// this patch may fail if there is no status which exists
	_, err = c.serviceEntryWriter.PatchStatus(se.Name, se.Namespace, types.JSONPatchType, replaceAddresses)
	if err != nil {
		// try this patch which tests that status doesn't exist, adds status and then add addresses all in 1 operation
		_, err2 := c.serviceEntryWriter.PatchStatus(se.Name, se.Namespace, types.JSONPatchType, addStatusAndAddresses)
		if err2 != nil {
			log.Errorf("second patch also rejected %v, patch: %s", err2.Error(), addStatusAndAddresses)
			// Both patches failed, likely due to stale informer state.
			// Free the IPs we allocated so they aren't leaked; the retry
			// will re-read from the informer and re-allocate if needed.
			c.freeAddresses(newlyAllocated, config.NamespacedName(serviceentry))
			return err
		}
		return nil
	}

	log.Debugf("patched successfully")
	return nil
}

func (c *IPAllocator) resolveConflict(conflict conflictDetectedEvent) error {
	var serviceentries, autoConflicts, userConflicts []*networkingv1.ServiceEntry

	for _, conflictingAddress := range conflict.getAddresses() {
		serviceentries = append(serviceentries, c.index.Lookup(conflictingAddress)...)
	}
	for _, serviceentry := range serviceentries {
		auto, user := allAddresses(serviceentry)
		for _, conflictingAddress := range conflict.getAddresses() {
			if slices.Contains(auto, conflictingAddress) {
				autoConflicts = append(autoConflicts, serviceentry)
			}
			if slices.Contains(user, conflictingAddress) {
				userConflicts = append(userConflicts, serviceentry)
			}
		}
	}

	slices.SortFunc(autoConflicts, func(a, b *networkingv1.ServiceEntry) int {
		return a.CreationTimestamp.Compare(b.CreationTimestamp.Time)
	})

	// this may be brittle since it relies on memory addresses being identical to filter
	// should we simply take care when constructing to not append dupes
	slices.FilterDuplicatesPresorted(autoConflicts)

	start := 1

	if len(userConflicts) > 0 {
		start = 0
	}

	var errs error
	for _, resolveMe := range autoConflicts[start:] {
		// filtering may leave behind nil values which we DO NOT want to deref
		if resolveMe == nil {
			continue
		}
		log.Warnf("reassigned %s/%s due to IP Address conflict", resolveMe.Namespace, resolveMe.Name)
		// we are patching from the slice of ServiceEntry where the conflicting address was found in status.addresses
		// this means we should already have a status and never need to use the patch that initializes the status
		patch, _, newlyAllocated, err := c.statusPatchForAddresses(resolveMe, true)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		if patch == nil {
			errs = errors.Join(errs, fmt.Errorf("this should not occur but patch was empty on a forced reassignment"))
			continue
		}

		_, err = c.serviceEntryWriter.PatchStatus(resolveMe.Name, resolveMe.Namespace, types.JSONPatchType, patch)
		if err != nil {
			c.freeAddresses(newlyAllocated, config.NamespacedName(resolveMe))
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func allAddresses(se *networkingv1.ServiceEntry) ([]netip.Addr, []netip.Addr) {
	if se == nil {
		return nil, nil
	}
	autoAssigned := autoallocate.GetAddressesFromServiceEntry(se)
	userAssigned := []netip.Addr{}

	for _, a := range se.Spec.Addresses {
		parsed, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		userAssigned = append(userAssigned, parsed)
	}
	return autoAssigned, userAssigned
}

func (c *IPAllocator) nextAddresses(owner types.NamespacedName) []netip.Addr {
	results := []netip.Addr{}
	v4address, err := c.v4allocator.allocateNext(owner)
	if err != nil {
		log.Error("failed to allocate v4 address")
	} else {
		results = append(results, v4address)
	}
	v6address, err := c.v6allocator.allocateNext(owner)
	if err != nil {
		log.Error("failed to allocate v6 address")
	} else {
		results = append(results, v6address)
	}
	return results
}

// freeAddresses releases addresses that were allocated by nextAddresses but never
// successfully written (e.g. because the patch was rejected due to stale informer state).
func (c *IPAllocator) freeAddresses(addrs []netip.Addr, owner types.NamespacedName) {
	for _, a := range addrs {
		a = a.Unmap()
		if a.Is6() {
			c.v6allocator.free(a, owner)
		} else {
			c.v4allocator.free(a, owner)
		}
	}
}

func (c *IPAllocator) markUsedOrQueueConflict(maybeMappedAddrs []netip.Addr, owner types.NamespacedName) {
	log.Debugf("marking used or queueing conflict for %s, owner: %s", maybeMappedAddrs, owner)
	conflictingAddrs := []netip.Addr{}
	for _, maybeMappedAddr := range maybeMappedAddrs {
		a := maybeMappedAddr.Unmap()
		if a.Is6() {
			if used, usedByOwner := c.v6allocator.markUsed(a, owner); used && !usedByOwner {
				conflictingAddrs = append(conflictingAddrs, a)
			}
		} else {
			if used, usedByOwner := c.v4allocator.markUsed(a, owner); used && !usedByOwner {
				conflictingAddrs = append(conflictingAddrs, a)
			}
		}
	}
	if len(conflictingAddrs) > 0 {
		log.Debugf("conflicting addresses: %s, owner: %s", conflictingAddrs, owner)
		c.queue.Add(newConflictDetectedEvent(owner, conflictingAddrs))
	}
}

type jsonPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value"`
}

func keepHost(se *networkingv1.ServiceEntry, h string) bool {
	// Only keep wildcarded hosts if the resolution is dynamic DNS
	return !cfghost.Name(h).IsWildCarded() || se.Spec.Resolution == apiv1alpha3.ServiceEntry_DYNAMIC_DNS
}

// statusPatchForAddresses returns two patch variants (replace and add-status) along with
// a list of any newly allocated addresses. On error, any newly allocated addresses are freed
// internally and all return values will be nil. The caller must still free the newly allocated
// addresses if neither patch succeeds, to avoid leaking IPs from the allocator.
func (c *IPAllocator) statusPatchForAddresses(se *networkingv1.ServiceEntry, forcedReassign bool) ([]byte, []byte, []netip.Addr, error) {
	if se == nil {
		return nil, nil, nil, nil
	}

	existingHostAddresses := autoallocate.GetHostAddressesFromServiceEntry(se)
	hostsWithAddresses := sets.New[string]()
	hostsInSpec := sets.New[string]()

	for _, host := range slices.Filter(se.Spec.Hosts, func(h string) bool { return keepHost(se, h) }) {
		hostsInSpec.Insert(host)
	}
	existingAddresses := []netip.Addr{}

	// collect existing addresses and the hosts which already have assigned addresses
	for host, addresses := range existingHostAddresses {
		// this is likely a noop, but just to be safe we should check and potentially resolve conflict
		existingAddresses = append(existingAddresses, addresses...)
		hostsWithAddresses.Insert(host)
	}

	// if we are being forced to reassign we already know there is a conflict
	if !forcedReassign {
		// if we're not being forced to reassign then we should ensure any addresses found are marked for use and queue up any conflicts found
		c.markUsedOrQueueConflict(existingAddresses, config.NamespacedName(se))
	}

	// nothing to patch
	if hostsInSpec.Equals(hostsWithAddresses) && !forcedReassign {
		return nil, nil, nil, nil
	}

	assignedHosts := sets.New[string]()
	var newlyAllocated []netip.Addr

	// construct the assigned addresses datastructure to patch
	assignedAddresses := []apiv1alpha3.ServiceEntryAddress{}
	for _, host := range slices.Filter(se.Spec.Hosts, func(h string) bool { return keepHost(se, h) }) {
		if assignedHosts.InsertContains(host) {
			continue
		}
		assignedIPs := []netip.Addr{}
		if aa, ok := existingHostAddresses[host]; ok && !forcedReassign {
			// we already assigned this host, do not re-assign
			assignedIPs = append(assignedIPs, aa...)
		} else {
			next := c.nextAddresses(config.NamespacedName(se))
			newlyAllocated = append(newlyAllocated, next...)
			assignedIPs = append(assignedIPs, next...)
		}

		for _, a := range assignedIPs {
			assignedAddresses = append(assignedAddresses, apiv1alpha3.ServiceEntryAddress{Value: a.String(), Host: host})
		}
	}

	replaceAddresses, err := json.Marshal([]jsonPatch{
		// Ensure the existing status we are acting on has not changed since we decided to allocate.
		// This avoids TOCTOU race conditions
		{
			Operation: "test",
			Path:      "/status/addresses",
			Value:     se.Status.Addresses,
		},
		{
			Operation: "replace",
			Path:      "/status/addresses",
			Value:     assignedAddresses,
		},
	})

	addStatusAndAddresses, err2 := json.Marshal([]jsonPatch{
		{
			Operation: "test",
			Path:      "/status",
			Value:     nil, // we want to test that status is nil before we try to modiy it with this
		},
		{
			Operation: "add",
			Path:      "/status",
			Value:     apiv1alpha3.ServiceEntryStatus{},
		},
		{
			Operation: "add",
			Path:      "/status/addresses",
			Value:     assignedAddresses,
		},
	})

	if errs := errors.Join(err, err2); errs != nil {
		c.freeAddresses(newlyAllocated, config.NamespacedName(se))
		return nil, nil, nil, errs
	}

	return replaceAddresses, addStatusAndAddresses, newlyAllocated, nil
}

// clearStaleAddresses removes auto-allocated addresses from status when the
// ServiceEntry no longer qualifies for auto-allocation (e.g. resolution
// changed to NONE, user opted out, or user supplied their own addresses).
func (c *IPAllocator) clearStaleAddresses(se *networkingv1.ServiceEntry) error {
	nn := config.NamespacedName(se)
	originalAddresses := autoallocate.GetAddressesFromServiceEntry(se)
	if err := c.tryClearAddresses(se); err != nil {
		// The TOCTOU "test" can fail if the status was modified between our
		// informer read and the patch. Re-read and retry once before falling
		// back to the queue's rate-limited retry.
		se = c.serviceEntryClient.Get(nn.Name, nn.Namespace)
		if se == nil || len(se.Status.Addresses) == 0 {
			// Already cleared or deleted; free the original addresses.
			c.freeAddresses(originalAddresses, nn)
			return nil
		}
		if err2 := c.tryClearAddresses(se); err2 != nil {
			log.Errorf("failed to clear stale addresses for %v: %v", nn, err2)
			return err2
		}
	}
	c.freeAddresses(originalAddresses, nn)
	return nil
}

func (c *IPAllocator) tryClearAddresses(se *networkingv1.ServiceEntry) error {
	replaceAddresses, err := json.Marshal([]jsonPatch{
		// Same TOCTOU guard as statusPatchForAddresses: test that addresses
		// have not changed since we read them before replacing with empty.
		{
			Operation: "test",
			Path:      "/status/addresses",
			Value:     se.Status.Addresses,
		},
		{
			Operation: "replace",
			Path:      "/status/addresses",
			Value:     []apiv1alpha3.ServiceEntryAddress{},
		},
	})
	if err != nil {
		return err
	}
	nn := config.NamespacedName(se)
	_, err = c.serviceEntryWriter.PatchStatus(nn.Name, nn.Namespace, types.JSONPatchType, replaceAddresses)
	return err
}

func (c *IPAllocator) checkInSpecAddresses(serviceentry *networkingv1.ServiceEntry) {
	addrs := []netip.Addr{}
	for _, addr := range serviceentry.Spec.Addresses {
		a, err := netip.ParseAddr(addr)
		if err != nil {
			log.Debugf("unable to parse address %s for %s/%s, received error: %s", addr, serviceentry.Namespace, serviceentry.Name, err.Error())
			continue
		}
		addrs = append(addrs, a)
	}
	// these are not assigned by us but could conflict with our IP ranges and cause issues
	c.markUsedOrQueueConflict(addrs, config.NamespacedName(serviceentry))
}

type prefixUse struct {
	prefix netip.Prefix
	used   map[netip.Addr]types.NamespacedName
	next   netip.Addr
}

func newPrefixUse(p netip.Prefix) *prefixUse {
	n := p.Addr().Next()
	return &prefixUse{
		prefix: p,
		used:   map[netip.Addr]types.NamespacedName{},
		next:   n,
	}
}

func (i *prefixUse) allocateNext(owner types.NamespacedName) (netip.Addr, error) {
	n := i.next
	var looped bool
	if !n.IsValid() || !i.prefix.Contains(n) {
		// we started outside of our range, that is unlucky but we can start over
		looped = true
		n = i.prefix.Addr().Next() // take the net address of the prefix and select next, this should be the first usable
	}

	for i.isUsed(n) {
		n = n.Next()
		if !n.IsValid() || !i.prefix.Contains(n) {
			if looped {
				// prefix is exhausted
				return netip.Addr{}, fmt.Errorf("%s is exhausted", i.prefix.String())
			}
			looped = true
			n = i.prefix.Addr().Next() // take the net address of the prefix and select next, this should be the first usable
		}
	}
	i.markUsed(n, owner)

	i.next = n.Next()

	return n, nil
}

func (i prefixUse) isUsed(n netip.Addr) bool {
	_, found := i.used[n]
	return found
}

func (i prefixUse) isUsedBy(n netip.Addr, owner types.NamespacedName) (used, usedByOwner bool) {
	if foundOwner, found := i.used[n]; found {
		used = true // it is in use
		res := strings.Compare(foundOwner.String(), owner.String())
		usedByOwner = res == 0 // is it in use by the providec owner?
		return used, usedByOwner
	}
	return used, usedByOwner
}

// free removes an address from the used map. If the address is before the current
// next pointer, next is reset so the address can be reused by the next allocation.
func (i *prefixUse) free(n netip.Addr, owner types.NamespacedName) {
	if foundOwner, found := i.used[n]; found && foundOwner == owner {
		delete(i.used, n)
		if !i.next.IsValid() || !i.prefix.Contains(i.next) || n.Less(i.next) {
			i.next = n
		}
	}
}

// markUsed will store the provided addr as used in this ipAllocator
// markUsed returns true if the addr was already used and false if it was a new insert
func (i prefixUse) markUsed(n netip.Addr, owner types.NamespacedName) (used, usedByOwner bool) {
	if !i.prefix.Contains(n) {
		// not in our range, no need to track it
		return used, usedByOwner
	}
	used, usedByOwner = i.isUsedBy(n, owner)
	if used {
		return used, usedByOwner
	}
	i.used[n] = owner

	return false, false
}
