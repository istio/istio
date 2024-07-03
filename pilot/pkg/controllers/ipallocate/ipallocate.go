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

	jsonpatch "github.com/evanphx/json-patch/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/meta/v1alpha1"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	autoallocate "istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

var log = istiolog.RegisterScope("ip-autoallocate", "IP autoallocate controller")

type IPAllocate struct {
	serviceEntryClient kclient.Client[*networkingv1alpha3.ServiceEntry]
	index              kclient.Index[netip.Addr, *networkingv1alpha3.ServiceEntry]
	stopChan           <-chan struct{}
	queue              controllers.Queue

	// This controller is not safe for concurency but it exists outside the critical path performing important but minimal functionality in a single thread.
	// If we want the multi-thread this controller we must add locking around accessing the allocators which would otherwise be racy
	v4allocator *ipAllocator
	v6allocator *ipAllocator
}

type conflictDetectedEvent struct {
	conflictingResourceIdentifier types.NamespacedName
	conflictingAddress            netip.Addr
}

const (
	controllerName = "IP Autoallocate"
	// these are the ranges of the v1 logic, do we need to choose new ranges?
	IPV4Prefix = "240.240.0.0/16"
	IPV6Prefix = "2001:2::/48"
)

func NewIPAllocate(stop <-chan struct{}, c kubelib.Client) *IPAllocate {
	client := kclient.New[*networkingv1alpha3.ServiceEntry](c)
	index := kclient.CreateIndex[netip.Addr, *networkingv1alpha3.ServiceEntry](client, func(serviceentry *networkingv1alpha3.ServiceEntry) []netip.Addr {
		addresses := autoallocate.GetV2AddressesFromServiceEntry(serviceentry)
		for _, addr := range serviceentry.Spec.Addresses {
			a, err := netip.ParseAddr(addr)
			if err != nil {
				continue
			}
			addresses = append(addresses, a)
		}
		return addresses
	})
	allocator := &IPAllocate{
		serviceEntryClient: client,
		index:              index,
		stopChan:           stop,
		// MustParsePrefix is OK because these are const. If we allow user configuration we must not use this function.
		v4allocator: newIPAllocator(netip.MustParsePrefix(IPV4Prefix)),
		v6allocator: newIPAllocator(netip.MustParsePrefix(IPV6Prefix)),
	}
	allocator.queue = controllers.NewQueue(controllerName, controllers.WithGenericReconciler(allocator.reconcile), controllers.WithMaxAttempts(5))
	client.AddEventHandler(controllers.ObjectHandler(allocator.queue.AddObject))
	return allocator
}

func (c *IPAllocate) Run(stop <-chan struct{}) {
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
func (c *IPAllocate) populateControllerDatastructures() {
	serviceentries := c.serviceEntryClient.List(metav1.NamespaceAll, klabels.Everything())
	count := 0
	for _, serviceentry := range serviceentries {
		count++
		owner := config.NamespacedName(serviceentry)
		log.Debugf("%s/%s found during warming", serviceentry.Namespace, serviceentry.Name)

		for _, addr := range serviceentry.Spec.Addresses {
			a, err := netip.ParseAddr(addr)
			if err != nil {
				log.Debugf("unable to parse address %s for %s/%s, received error: %s", addr, serviceentry.Namespace, serviceentry.Name, err.Error())
				continue
			}
			// these are not assigned by us but could conflict with our IP ranges and cause issues
			if !c.inOurRange(a) {
				// don't need to worry about this one
				continue
			}
			c.markUsedOrQueueConflict(a, owner)
		}

		for _, a := range autoallocate.GetV2AddressesFromServiceEntry(serviceentry) {
			c.markUsedOrQueueConflict(a, owner)
		}
	}

	log.Debugf("discovered %v during warming", count)
}

func (c *IPAllocate) reconcile(a any) error {
	if nn, ok := a.(types.NamespacedName); ok {
		return c.reconcileServiceEntry(nn)
	}
	if conflict, ok := a.(conflictDetectedEvent); ok {
		return c.resolveConflict(conflict)
	}

	return nil
}

func (c *IPAllocate) reconcileServiceEntry(se types.NamespacedName) error {
	log.Debugf("reconciling ServiceEntry %s/%s", se.Namespace, se.Name)
	serviceentry := c.serviceEntryClient.Get(se.Name, se.Namespace)
	if serviceentry == nil {
		// probably a delete so we should remove ips from our addresses most likely
		// TODO: we never actually remove IP right now, likely this should be done a little more slowly anyway to prevent reuse if we are too fast
		return nil
	}

	// TODO: also what do we do if we already allocated IPs in a previous reconcile but are not longer meant to
	// or IP would not be usabled due to an update? write a condition "IP unsabled due to wildcard host or something similar"
	if !autoallocate.ShouldV2AutoAllocateIP(serviceentry) {
		// TODO: DRY with warm...
		// we may have an address in our range so we should check and record it
		for _, addr := range serviceentry.Spec.Addresses {
			a, err := netip.ParseAddr(addr)
			if err != nil {
				log.Debugf("unable to parse address %s for %s/%s, received error: %s", addr, serviceentry.Namespace, serviceentry.Name, err.Error())
				continue
			}
			// these are not assigned by us but could conflict with our IP ranges and cause issues
			if !c.inOurRange(a) {
				// don't need to worry about this one
				continue
			}
			c.markUsedOrQueueConflict(a, se)
		}
		return nil // nothing to do
	}

	patch, err := c.statusPatchForAddresses(serviceentry, false)
	if err != nil {
		return err
	}

	if patch == nil {
		return nil // nothing to patch
	}

	_, err = c.serviceEntryClient.PatchStatus(se.Name, se.Namespace, types.MergePatchType, patch)
	if err != nil {
		return err
	}

	return nil
}

func (c *IPAllocate) resolveConflict(conflict conflictDetectedEvent) error {
	var autoConflicts, userConflicts []*networkingv1alpha3.ServiceEntry
	serviceentries := c.index.Lookup(conflict.conflictingAddress)
	for _, serviceentry := range serviceentries {
		auto, user := allAddresses(serviceentry)
		if slices.Contains(auto, conflict.conflictingAddress) {
			autoConflicts = append(autoConflicts, serviceentry)
		}
		if slices.Contains(user, conflict.conflictingAddress) {
			userConflicts = append(userConflicts, serviceentry)
		}
	}

	slices.SortFunc(autoConflicts, func(a, b *networkingv1alpha3.ServiceEntry) int {
		return a.CreationTimestamp.Compare(b.CreationTimestamp.Time)
	})

	start := 1

	if len(userConflicts) > 0 {
		start = 0
	}
	var errs error
	for _, resolveMe := range autoConflicts[start:] {
		log.Debugf("reassigned %s/%s due to IP Address conflict", resolveMe.Namespace, resolveMe.Name)
		patch, err := c.statusPatchForAddresses(resolveMe, true)
		if err != nil {
			errs = errors.Join(errs, err)
		}

		if patch == nil {
			errs = errors.Join(errs, fmt.Errorf("this should not occur but patch was empty on a forced reassignment"))
		}

		_, err = c.serviceEntryClient.PatchStatus(resolveMe.Name, resolveMe.Namespace, types.MergePatchType, patch)
		if err != nil {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

func allAddresses(se *networkingv1alpha3.ServiceEntry) ([]netip.Addr, []netip.Addr) {
	if se == nil {
		return nil, nil
	}
	autoAssigned := autoallocate.GetV2AddressesFromServiceEntry(se)
	userAssigned := []netip.Addr{}

	for _, a := range se.Spec.Addresses {
		parsed, err := netip.ParseAddr(a)
		if err != nil {
			// maybe log
			continue
		}
		userAssigned = append(userAssigned, parsed)
	}
	return autoAssigned, userAssigned
}

func (c *IPAllocate) nextAddresses(owner types.NamespacedName) []netip.Addr {
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

func (c *IPAllocate) inOurRange(a netip.Addr) bool {
	a = a.Unmap()
	if a.Is6() {
		return c.v6allocator.prefix.Contains(a)
	}

	return c.v4allocator.prefix.Contains(a)
}

func (c *IPAllocate) markUsedOrQueueConflict(a netip.Addr, owner types.NamespacedName) {
	if a.Is6() {
		if used, usedByOwner := c.v6allocator.markUsed(a, owner); used && !usedByOwner {
			c.queue.Add(conflictDetectedEvent{
				conflictingResourceIdentifier: owner,
				conflictingAddress:            a,
			})
		}
	} else {
		if used, usedByOwner := c.v4allocator.markUsed(a, owner); used && !usedByOwner {
			c.queue.Add(conflictDetectedEvent{
				conflictingResourceIdentifier: owner,
				conflictingAddress:            a,
			})
		}
	}
}

func (c *IPAllocate) statusPatchForAddresses(se *networkingv1alpha3.ServiceEntry, forcedReassign bool) ([]byte, error) {
	if se == nil {
		return nil, nil
	}
	orig, err := json.Marshal(se)
	if err != nil {
		return nil, err
	}

	if forcedReassign {
		// TODO: this is a kludge and incorrect, we should not overwrite status in the final commit
		// initialize status field
		se.Status = v1alpha1.IstioStatus{}
	}
	foundAddresses := autoallocate.GetV2AddressesFromServiceEntry(se)
	if len(foundAddresses) > 0 && !forcedReassign {

		// this is likely a noop, but just to be safe we should check and potentially resolve conflict
		for _, a := range foundAddresses {
			c.markUsedOrQueueConflict(a, types.NamespacedName{
				Name:      se.Name,
				Namespace: se.Namespace,
			})
		}
		return nil, nil // nothing to patch
	}
	addresses := c.nextAddresses(types.NamespacedName{
		Name:      se.Name,
		Namespace: se.Namespace,
	})
	// begin allocating new addresses
	kludge := autoallocate.ConditionKludge(addresses)

	se.Status.Conditions = append(se.Status.Conditions, &kludge)
	modified, err := json.Marshal(se)
	if err != nil {
		return nil, err
	}
	return jsonpatch.CreateMergePatch(orig, modified)
}

type ipAllocator struct {
	prefix netip.Prefix
	used   map[netip.Addr]types.NamespacedName
	next   netip.Addr
}

func newIPAllocator(p netip.Prefix) *ipAllocator {
	n := p.Addr().Next()
	return &ipAllocator{
		prefix: p,
		used:   map[netip.Addr]types.NamespacedName{},
		next:   n,
	}
}

func (i *ipAllocator) allocateNext(owner types.NamespacedName) (netip.Addr, error) {
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

func (i ipAllocator) isUsed(n netip.Addr) bool {
	_, found := i.used[n]
	return found
}

func (i ipAllocator) isUsedBy(n netip.Addr, owner types.NamespacedName) (used, usedByOwner bool) {
	if foundOwner, found := i.used[n]; found {
		used = true // it is in use
		res := strings.Compare(foundOwner.String(), owner.String())
		usedByOwner = res == 0 // is it in use by the providec owner?
		return
	}
	return
}

// markUsed will store the provided addr as used in this ipAllocator
// markUsed returns true if the addr was already used and false if it was a new insert
func (i ipAllocator) markUsed(n netip.Addr, owner types.NamespacedName) (used, usedByOwner bool) {
	used, usedByOwner = i.isUsedBy(n, owner)
	if used {
		return
	}
	i.used[n] = owner

	return false, false
}
