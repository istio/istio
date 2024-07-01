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
	"fmt"
	"net/netip"

	jsonpatch "github.com/evanphx/json-patch"
	"istio.io/api/meta/v1alpha1"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	autoallocate "istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

var log = istiolog.RegisterScope("ip-autoallocate", "IP autoallocate controller")

type IPAllocate struct {
	serviceEntryClient kclient.Client[*networkingv1alpha3.ServiceEntry]
	stopChan           <-chan struct{}
	queue              controllers.Queue

	// This controller is not safe for concurency but it exists outside the critical path performing important but minimal functionality in a single thread.
	// If we want the multi-thread this controller we must add locking around accessing the allocators which would otherwise be racy
	v4allocator *ipAllocator
	v6allocator *ipAllocator
}

const (
	controllerName = "IP Autoallocate"
	// these are the ranges of the v1 logic, do we need to choose new ranges?
	IPV4Prefix = "240.240.0.0/16"
	IPV6Prefix = "2001:2::/48"
)

func NewIPAllocate(stop <-chan struct{}, c kubelib.Client) *IPAllocate {
	client := kclient.New[*networkingv1alpha3.ServiceEntry](c)
	allocator := &IPAllocate{
		serviceEntryClient: client,
		stopChan:           stop,
		// MustParsePrefix is OK because these are const. If we allow user configuration we must not use this function.
		v4allocator: newIPAllocator(netip.MustParsePrefix(IPV4Prefix)),
		v6allocator: newIPAllocator(netip.MustParsePrefix(IPV6Prefix)),
	}
	allocator.queue = controllers.NewQueue(controllerName, controllers.WithReconciler(allocator.reconcile), controllers.WithMaxAttempts(5))
	client.AddEventHandler(controllers.ObjectHandler(allocator.queue.AddObject))
	return allocator
}

func (c *IPAllocate) Run(stop <-chan struct{}) {
	log.Debugf("starting %s controller", controllerName)
	kubelib.WaitForCacheSync(controllerName, stop, c.serviceEntryClient.HasSynced)
	// it is important that we always warm cache before we try to run the queue
	// failing to warm cache first could result in allocating IP which are already in use
	c.warmCache()
	// logically the v1 behavior is a state of the world function which cannot be replicated in reconcile
	// if we wish to replicate v1 assignment behavior for existing serviceentry it should go here and must include a change to respect the used IPs from the warmed cache
	// begin reconcile
	c.queue.Run(stop)
	c.serviceEntryClient.ShutdownHandlers()
}

// warmCache reads all serviceentries and records any auto-assigned addresses as in use
func (c *IPAllocate) warmCache() {
	serviceentries := c.serviceEntryClient.List(metav1.NamespaceAll, klabels.Everything())
	count := 0
	for _, serviceentry := range serviceentries {
		count++
		log.Debugf("%s/%s found during warming", serviceentry.Namespace, serviceentry.Name)
		if len(serviceentry.Spec.Addresses) > 0 {
			// user assiged there own
			// TODO: we should record this just to be safe, at least if it is without our range
			continue
		}

		if serviceentry.Status.String() != "" {
			addresses := autoallocate.GetV2AddressesFromServiceEntry(serviceentry)
			if len(addresses) > 0 {
				for _, a := range addresses {
					if a.Is6() {
						if c.v6allocator.MarkUsed(a) {
							panic("collision v6")
						}
					} else {
						if c.v4allocator.MarkUsed(a) {
							panic("collision v4")
						}
					}
				}
			}
		}
	}
	log.Debugf("discovered %v during warming", count)
}

func (c *IPAllocate) reconcile(se types.NamespacedName) error {
	log.Debugf("reconciling ServiceEntry %s/%s", se.Namespace, se.Name)
	serviceentry := c.serviceEntryClient.Get(se.Name, se.Namespace)
	if serviceentry == nil {
		// probably a delete so we should remove ips from our addresses most likely
		// TODO: we never actually remove IP right now, likely this should be done a little more slowly anyway to prevent reuse if we are too fast
		return nil
	}

	// TODO: also what do we do if we already allocated IPs in a previous reconcile but are not longer meant to or IP would not be usabled due to an update? write a condition "IP unsabled due to wildcard host or something similar"
	if !autoallocate.ShouldV2AutoAllocateIP(serviceentry) {
		return nil // nothing to do
	}

	patch, err := c.statusPatchForAddresses(serviceentry)
	if err != nil {
		panic("failed generating status patch for addresses")
	}

	if patch == nil {
		return nil // nothing to patch
	}

	_, err = c.serviceEntryClient.PatchStatus(se.Name, se.Namespace, types.MergePatchType, patch)
	if err != nil {
		log.Errorf("unable to patch %s, received error %s", se.String(), err.Error())
		return err
	}

	return nil
}

func (c *IPAllocate) nextAddresses() []netip.Addr {
	v4address, err := c.v4allocator.AllocateNext()
	if err != nil {
		panic("failed to allocate v4 address")
	}
	v6address, err := c.v6allocator.AllocateNext()
	if err != nil {
		panic("failed to allocate v6 address")
	}
	return []netip.Addr{
		v4address,
		v6address,
	}
}

func (c *IPAllocate) statusPatchForAddresses(se *networkingv1alpha3.ServiceEntry) ([]byte, error) {
	if se == nil {
		return nil, nil
	}
	orig, err := json.Marshal(se)
	if err != nil {
		panic("failed to marshal original service entry")
	}

	if se.Status.String() == "" {
		// initialize status field
		se.Status = v1alpha1.IstioStatus{}
	} else {
		foundAddresses := autoallocate.GetV2AddressesFromServiceEntry(se)
		if len(foundAddresses) > 0 {
			// this is a noop, if we already assigned addresses we do not mess with them
			// TODO: potentially check if foundAddresses == addresses and error if not
			return nil, nil // nothing to patch
		}
	}
	addresses := c.nextAddresses()
	// begin allocating new addresses
	kludge := autoallocate.ConditionKludge(addresses)

	se.Status.Conditions = append(se.Status.Conditions, &kludge)
	modified, err := json.Marshal(se)
	if err != nil {
		panic("failed to marshal modified service entry")
	}
	return jsonpatch.CreateMergePatch(orig, modified)
}

// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// probably doesn't belong here, although nothing else would ever really use it...
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================

type ipAllocator struct {
	prefix netip.Prefix
	used   sets.Set[netip.Addr]
	next   netip.Addr
}

func newIPAllocator(p netip.Prefix) *ipAllocator {
	n := p.Addr().Next()
	return &ipAllocator{
		prefix: p,
		used:   sets.Set[netip.Addr]{},
		next:   n,
	}
}

func (i *ipAllocator) AllocateNext() (netip.Addr, error) {
	n := i.next
	var looped bool
	if !n.IsValid() || !i.prefix.Contains(n) {
		// unlucky initial looping, start over
		looped = true
		n = i.prefix.Addr().Next() // take the net address of the prefix and select next, this should be the first usable
	}

	for i.IsUsed(n) {
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
	i.MarkUsed(n)

	i.next = n.Next()

	return n, nil
}

func (i ipAllocator) IsUsed(n netip.Addr) bool {
	return i.used.Contains(n)
}

// MarkUsed will store the provided addr as used in this ipAllocator
// MarkUsed returns true if the addr was already used and false if it was a new insert
func (i ipAllocator) MarkUsed(n netip.Addr) bool {
	inUse := i.IsUsed(n)
	if inUse {
		return true
	}
	i.used.Insert(n)
	return false
}
