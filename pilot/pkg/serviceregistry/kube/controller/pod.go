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

package controller

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
)

type podKey struct {
	ip   string
	name string
}

// PodCache is an eventually consistent pod cache
type PodCache struct {
	informer cache.SharedIndexInformer

	sync.RWMutex
	// podsByIP maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP.
	// This should only contain RUNNING or PENDING pods with an allocated IP.
	podsByIP map[string]string
	// IPByPods is a reverse map of podsByIP. This exists to allow us to prune stale entries in the
	// pod cache if a pod changes IP.
	IPByPods map[string]string

	recentDeletedPods map[podKey]struct{}

	c *Controller
}

func newPodCache(c *Controller, informer coreinformers.PodInformer) *PodCache {
	out := &PodCache{
		informer:          informer.Informer(),
		c:                 c,
		podsByIP:          make(map[string]string),
		IPByPods:          make(map[string]string),
		recentDeletedPods: make(map[podKey]struct{}),
	}

	return out
}

// onEvent updates the IP-based index (pc.podsByIP).
func (pc *PodCache) onEvent(curr interface{}, ev model.Event) error {
	pc.Lock()
	defer pc.Unlock()

	// When a pod is deleted obj could be an *v1.Pod or a DeletionFinalStateUnknown marker item.
	pod, ok := curr.(*v1.Pod)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %+v", curr)
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a pod %#v", curr)
		}
	}

	ip := pod.Status.PodIP
	// PodIP will be empty when pod is just created, but before the IP is assigned
	// via UpdateStatus.

	if len(ip) > 0 {
		log.Debugf("Handling event %s for pod %s (%v) in namespace %s -> %v", ev, pod.Name, pod.Status.Phase, pod.Namespace, ip)
		key := kube.KeyFunc(pod.Name, pod.Namespace)
		switch ev {
		case model.EventAdd:
			switch pod.Status.Phase {
			case v1.PodPending, v1.PodRunning:
				if key != pc.podsByIP[ip] {
					// add to cache if the pod is running or pending
					pc.update(ip, key)
				}
			}
		case model.EventUpdate:
			if pod.DeletionTimestamp != nil {
				// just ignore this event as endpoint update might come later for this.
				return nil
			}
			switch pod.Status.Phase {
			case v1.PodPending, v1.PodRunning:
				if key != pc.podsByIP[ip] {
					// add to cache if the pod is running or pending
					pc.update(ip, key)
				}

			default:
				// delete if the pod switched to other states and is in the cache
				if pc.podsByIP[ip] == key {
					pc.deleteIP(ip)
				}
			}
		case model.EventDelete:
			// delete only if this pod was in the cache
			if pc.podsByIP[ip] == key {
				pc.deleteIP(ip)
			}
		}
	}
	return nil
}

func (pc *PodCache) deleteIP(ip string) {
	pod := pc.podsByIP[ip]
	delete(pc.podsByIP, ip)
	delete(pc.IPByPods, pod)
	pc.recentDeletedPods[podKey{ip, pod}] = struct{}{}
}

func (pc *PodCache) update(ip, key string) {
	if current, f := pc.IPByPods[key]; f {
		// The pod already exists, but with another IP Address. We need to clean up that
		delete(pc.podsByIP, current)
	}
	pc.podsByIP[ip] = key
	pc.IPByPods[key] = ip

	pc.proxyUpdates(ip)
}

func (pc *PodCache) proxyUpdates(ip string) {
	if pc.c != nil && pc.c.xdsUpdater != nil {
		pc.c.xdsUpdater.ProxyUpdate(pc.c.clusterID, ip)
	}
}

// nolint: unparam
func (pc *PodCache) getPodKey(addr string) (string, bool) {
	pc.RLock()
	defer pc.RUnlock()
	key, exists := pc.podsByIP[addr]
	return key, exists
}

// getPodByIp returns the pod or nil if pod not found or an error occurred
func (pc *PodCache) getPodByIP(addr string) *v1.Pod {
	key, exists := pc.getPodKey(addr)
	if !exists {
		return nil
	}
	item, exists, err := pc.informer.GetStore().GetByKey(key)
	if !exists || err != nil {
		return nil
	}
	return item.(*v1.Pod)
}
