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
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	apiv1alpha3 "istio.io/api/networking/v1alpha3"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	autoallocate "istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pkg/config"
	cfghost "istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvr"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("ip-autoallocate", "IP autoallocate controller")

type IPAllocator struct {
	serviceEntryClient kclient.Informer[*networkingv1.ServiceEntry]
	serviceEntryWriter kclient.Writer[*networkingv1.ServiceEntry]
	index              kclient.Index[netip.Addr, *networkingv1.ServiceEntry]
	stopChan           <-chan struct{}
	queue              controllers.Queue

	// This controller is not safe for concurrency but it exists outside the critical path performing important but minimal functionality in a single thread.
	// If we want the multi-thread this controller we must add locking around accessing the allocators which would otherwise be racy
	v4allocator *prefixUse
	v6allocator *prefixUse
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
	controllerName = "IP Autoallocator"
)

func NewIPAllocator(stop <-chan struct{}, c kubelib.Client) *IPAllocator {
	client := kclient.NewDelayedInformer[*networkingv1.ServiceEntry](c, gvr.ServiceEntry, kubetypes.StandardInformer, kclient.Filter{
		ObjectFilter: c.ObjectFilter(),
	})
	writer := kclient.NewWriteClient[*networkingv1.ServiceEntry](c)
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
		stopChan:           stop,
		// MustParsePrefix is OK because these are const. If we allow user configuration we must not use this function.
		v4allocator: newPrefixUse(netip.MustParsePrefix(features.IPAutoallocateIPv4Prefix)),
		v6allocator: newPrefixUse(netip.MustParsePrefix(features.IPAutoallocateIPv6Prefix)),
	}
	allocator.queue = controllers.NewQueue(controllerName, controllers.WithGenericReconciler(allocator.reconcile), controllers.WithMaxAttempts(5))
	client.AddEventHandler(controllers.ObjectHandler(allocator.queue.AddObject))
	return allocator
}

func (c *IPAllocator) Run(stop <-chan struct{}) {
	log.Debugf("starting %s controller", controllerName)
	kubelib.WaitForCacheSync(controllerName, stop, c.serviceEntryClient.HasSynced)
	// it is important that we always warm cache before we try to run the queue
	// failing to warm cache first could result in allocating IP which are already in use
	c.populateControllerDatastructures()
	// logically the v1 behavior is a state of the world function which cannot be replicated in reconcile
	// if we wish to replicate v1 assignment behavior for existing serviceentry it should go here and must
	// include a change to respect the used IPs from the warmed cache begin reconcile
	c.queue.Run(stop)
	c.serviceEntryClient.ShutdownHandlers()
}

// populateControllerDatastructures reads all serviceentries and records any addresses in our ranges as in use
func (c *IPAllocator) populateControllerDatastructures() {
	serviceentries := c.serviceEntryClient.List(metav1.NamespaceAll, klabels.Everything())
	count := 0
	for _, serviceentry := range serviceentries {
		count++
		owner := config.NamespacedName(serviceentry)
		c.checkInSpecAddresses(serviceentry)
		c.markUsedOrQueueConflict(autoallocate.GetAddressesFromServiceEntry(serviceentry), owner)
	}

	log.Debugf("discovered %v during warming", count)
}

func (c *IPAllocator) reconcile(a any) error {
	if nn, ok := a.(types.NamespacedName); ok {
		return c.reconcileServiceEntry(nn)
	}
	if conflict, ok := a.(conflictDetectedEvent); ok {
		return c.resolveConflict(conflict)
	}

	return nil
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

	// TODO: also what do we do if we already allocated IPs in a previous reconcile but are not longer meant to
	// or IP would not be usabled due to an update? write a condition "IP unsabled due to wildcard host or something similar"
	if !autoallocate.ShouldV2AutoAllocateIP(serviceentry) {
		// we may have an address in our range so we should check and record it
		c.checkInSpecAddresses(serviceentry)
		log.Debugf("allocation not required")
		return nil
	}

	replaceAddresses, addStatusAndAddresses, err := c.statusPatchForAddresses(serviceentry, false)
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
			// if this also didn't work there is perhaps a real issue and perhaps a requeue will resolve
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
		patch, _, err := c.statusPatchForAddresses(resolveMe, true)
		if err != nil {
			errs = errors.Join(errs, err)
		}

		if patch == nil {
			errs = errors.Join(errs, fmt.Errorf("this should not occur but patch was empty on a forced reassignment"))
		}

		_, err = c.serviceEntryWriter.PatchStatus(resolveMe.Name, resolveMe.Namespace, types.JSONPatchType, patch)
		if err != nil {
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

func (c *IPAllocator) markUsedOrQueueConflict(maybeMappedAddrs []netip.Addr, owner types.NamespacedName) {
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
		c.queue.Add(newConflictDetectedEvent(owner, conflictingAddrs))
	}
}

type jsonPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value"`
}

func keepHost(se *networkingv1.ServiceEntry, h string) bool {
	// Only keep wildcarded hosts if the resolution is not dynamic DNS
	return !cfghost.Name(h).IsWildCarded() || se.Spec.Resolution == apiv1alpha3.ServiceEntry_DYNAMIC_DNS
}

func (c *IPAllocator) statusPatchForAddresses(se *networkingv1.ServiceEntry, forcedReassign bool) ([]byte, []byte, error) {
	if se == nil {
		return nil, nil, nil
	}

	existingHostAddresses := autoallocate.GetHostAddressesFromServiceEntry(se)
	hostsWithAddresses := sets.New[string]()
	hostsInSpec := sets.New[string]()

	// TODO(jaellio): Make this behavior conditional on ambient mode being enabled
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
		return nil, nil, nil
	}

	assignedHosts := sets.New[string]()

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
			assignedIPs = append(assignedIPs, c.nextAddresses(config.NamespacedName(se))...)
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

	return replaceAddresses, addStatusAndAddresses, errors.Join(err, err2)
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
