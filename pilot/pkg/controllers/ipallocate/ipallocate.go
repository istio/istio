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
	"sync"

	"istio.io/api/meta/v1alpha1"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1alpha3"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
	"k8s.io/apimachinery/pkg/types"
)

var log = istiolog.RegisterScope("ipallocate", "IP autoallocate controller")

type IPAllocate struct {
	serviceEntryClient kclient.Client[*networkingclient.ServiceEntry]
	stopChan           <-chan struct{}
	queue              controllers.Queue

	// use this mutext when access these fields
	// TODO: is locking just a function of the ipAllocator type instead? probably should be
	mu          sync.Mutex
	v4allocator *ipAllocator
	v6allocator *ipAllocator
}

const (
	IpAutoallocateStatusType = "ip-autoallocate"
	IpV4Prefix               = "240.240.0.0/16"
	IpV6Prefix               = "2001:2::/48"
)

func NewIPAllocate(stop <-chan struct{}, c kubelib.Client) *IPAllocate {
	log.Debugf("starting serviceentry ip autoallocate controller")
	client := kclient.New[*networkingclient.ServiceEntry](c)
	allocator := &IPAllocate{
		serviceEntryClient: client,
		mu:                 sync.Mutex{},
		stopChan:           stop,
		v4allocator:        newIpAllocator(netip.MustParsePrefix(IpV4Prefix)),
		v6allocator:        newIpAllocator(netip.MustParsePrefix(IpV6Prefix)),
	}

	allocator.queue = controllers.NewQueue("ip autoallocate", controllers.WithReconciler(allocator.reconcile), controllers.WithMaxAttempts(5))
	client.AddEventHandler(controllers.ObjectHandler(allocator.queue.AddObject))
	return allocator
}

func (c *IPAllocate) Run(stop <-chan struct{}) {
	kubelib.WaitForCacheSync("node untainer", stop, c.serviceEntryClient.HasSynced)
	c.queue.Run(stop)
	c.serviceEntryClient.ShutdownHandlers()
}

func (c *IPAllocate) nextAddresses() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
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

func (c *IPAllocate) reconcile(se types.NamespacedName) error {
	log.Debugf("reconciling service entry %s/%s", se.Namespace, se.Name)
	serviceentry := c.serviceEntryClient.Get(se.Name, se.Namespace)
	if serviceentry == nil {
		// probably a delete so we should remove ips from our addresses most likely
		log.Infof("deleted %s", se.String())
		return nil
	}
	// check if we are supposed to assign
	if !c.queue.HasSynced() {
		// warming: we need to record any IPs from the status
		log.Info("beep boop, we're not synced yet")
		log.Info("we can still look for IPs though")
		if len(serviceentry.Spec.Addresses) > 0 {
			// user assiged there own
			// TODO: we should record this just to be safe, at least if it is without our range
			log.Infof("%s supplied its own addresses", se.String())

			return nil
		}

		if serviceentry.Status.String() == "" {
			log.Info("found no status")
		} else {
			log.Infof("status: %s", serviceentry.Status.String())
			addresses := kludgeFromStatus(serviceentry.Status.GetConditions())
			if len(addresses) > 0 {
				c.mu.Lock()
				defer c.mu.Unlock()
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
				return nil // we found some addresses, lets go
			}
		}
		return fmt.Errorf("unable to assign IPs for %s becuase controller has not synced completely", se.String())
	}
	log.Info("now we are synced so we could do something")
	if len(serviceentry.Spec.Addresses) > 0 {
		// user assiged there own
		// TODO: we should record this just to be safe, at least if it is without our range
		log.Infof("%s supplied its own addresses", se.String())

		return nil
	}
	if serviceentry.Status.String() == "" {
		log.Info("found no status")
		serviceentry.Status = v1alpha1.IstioStatus{}
	} else {
		log.Infof("found a status for %s", se.String())
		addresses := kludgeFromStatus(serviceentry.Status.GetConditions())
		if len(addresses) > 0 {

			log.Infof("found addresses %v", addresses)
			return nil // nothing to do
		}
	}
	addresses := c.nextAddresses()
	kludge := conditionFromKludge(addresses)

	serviceentry.Status.Conditions = append(serviceentry.Status.Conditions, &kludge)

	_, e := c.serviceEntryClient.UpdateStatus(serviceentry)
	if e != nil {
		log.Errorf("darn... %s", e.Error())
	}
	log.Info("looks like this all worked")

	return nil
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
// MarkUsed return true if the addr was already used and false if it was a new insert
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

func conditionFromKludge(input []netip.Addr) v1alpha1.IstioCondition {
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
