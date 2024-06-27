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
	// "gomodules.xyz/jsonpatch/v2"
	"istio.io/api/meta/v1alpha1"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
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
	serviceEntryClient kclient.Client[*networkingclient.ServiceEntry]
	stopChan           <-chan struct{}
	queue              controllers.Queue

	// This controller is not safe for concurency but it exists outside the critical path performing important but minimal functionality in a single thread.
	// If we want the multi-thread this controller we must add locking around accessing the allocators which would otherwise be racy
	v4allocator *ipAllocator
	v6allocator *ipAllocator
}

const (
	controllerName           = "IP Autoallocate"
	IpAutoallocateStatusType = "ip-autoallocate"
	// these are the ranges of the v1 logic, do we need to choose new ranges?
	IpV4Prefix = "240.240.0.0/16"
	IpV6Prefix = "2001:2::/48"
)

func NewIPAllocate(stop <-chan struct{}, c kubelib.Client) *IPAllocate {
	client := kclient.New[*networkingclient.ServiceEntry](c)
	allocator := &IPAllocate{
		serviceEntryClient: client,
		stopChan:           stop,
		// MustParsePrefix is OK because these are const. If we allow user configuration we must not use this function.
		v4allocator: newIpAllocator(netip.MustParsePrefix(IpV4Prefix)),
		v6allocator: newIpAllocator(netip.MustParsePrefix(IpV6Prefix)),
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
		log.Infof("%s/%s found during warming", serviceentry.Namespace, serviceentry.Name)
		if len(serviceentry.Spec.Addresses) > 0 {
			// user assiged there own
			// TODO: we should record this just to be safe, at least if it is without our range
			log.Infof("%s/%s supplied its own addresses", serviceentry.Namespace, serviceentry.Name)

			continue
		}

		if serviceentry.Status.String() != "" {
			addresses := kludgeFromStatus(serviceentry.Status.GetConditions())
			log.Infof("status: %v", addresses)
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
	log.Infof("discovered %v during warming", count)
}

func (c *IPAllocate) reconcile(se types.NamespacedName) error {
	log.Infof("reconciling service entry %s/%s", se.Namespace, se.Name)
	serviceentry := c.serviceEntryClient.Get(se.Name, se.Namespace)
	if serviceentry == nil {
		// probably a delete so we should remove ips from our addresses most likely
		// TODO: we never actually remove IP right now, likely this should be done a little more slowly anyway to prevent reuse if we are too fast
		log.Infof("deleted %s", se.String())
		return nil
	}
	// TODO: check if we are supposed to assign

	if len(serviceentry.Spec.Addresses) > 0 {
		// user assiged
		// TODO: we should record this just to be safe, at least if it is without our range
		log.Infof("%s supplied its own addresses", se.String())
		return nil
	}

	orig, err := json.Marshal(serviceentry)
	if err != nil {
		panic("failed to marshal original service entry")
	}

	if serviceentry.Status.String() == "" {
		// initialize status field
		serviceentry.Status = v1alpha1.IstioStatus{}
	} else {
		log.Infof("found a status for %s", se.String())
		foundAddresses := kludgeFromStatus(serviceentry.Status.GetConditions())
		if len(foundAddresses) > 0 {
			// this is a noop, if we already assigned addresses we do not mess with them
			log.Infof("found addresses %v", foundAddresses)
			return nil // nothing to do
		}
	}
	// begin allocating new addresses
	kludge := conditionKludge(c.nextAddresses())

	serviceentry.Status.Conditions = append(serviceentry.Status.Conditions, &kludge)
	modified, err := json.Marshal(serviceentry)
	if err != nil {
		panic("failed to marshal modified service entry")
	}
	// TODO: patch don't update so we don't accidentally remove status written by another controller during our reconcile
	// _, e := c.serviceEntryClient.UpdateStatus(serviceentry)
	patch, err := jsonpatch.CreateMergePatch(orig, modified)
	// patch, err := jsonpatch.CreatePatch(orig, modified)
	if err != nil {
		panic("failed to create merge patch")
	}
	// patchjson, err := patch[0].MarshalJSON()
	// if err != nil {
	// 	panic("failed to marshal patch")
	// }
	log.Infof("patch: %v", string(patch))
	result, e := c.serviceEntryClient.Patch(se.Name, se.Namespace, types.MergePatchType, patch)
	if e != nil {
		log.Errorf("darn... %s", e.Error())
	}
	log.Infof("patching result: %v", result.Status)

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

// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// ==========================================================================================================================================================
// probably doesn't belong here
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

func newIpAllocator(p netip.Prefix) *ipAllocator {
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
		// unlucky inital looping, start over
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

type ServiceEntryStatusKludge struct {
	Addresses []string // json:"addresses"
}

func kludgeFromStatus(conditions []*v1alpha1.IstioCondition) []netip.Addr {
	result := []netip.Addr{}
	for _, c := range conditions {
		if c == nil {
			continue
		}
		if c.Type != IpAutoallocateStatusType {
			continue
		}
		jsonAddresses := c.Message
		kludge := ServiceEntryStatusKludge{}
		json.Unmarshal([]byte(jsonAddresses), &kludge)
		for _, address := range kludge.Addresses {
			result = append(result, netip.MustParseAddr(address))
		}
	}
	return result
}

func conditionKludge(input []netip.Addr) v1alpha1.IstioCondition {
	addresses := []string{}
	for _, addr := range input {
		addresses = append(addresses, addr.String())
	}
	kludge := ServiceEntryStatusKludge{
		Addresses: addresses,
	}
	result, _ := json.Marshal(kludge)
	return v1alpha1.IstioCondition{
		Type:    IpAutoallocateStatusType,
		Status:  "true",
		Reason:  "AutoAllocatedAddress",
		Message: string(result),
	}
}
