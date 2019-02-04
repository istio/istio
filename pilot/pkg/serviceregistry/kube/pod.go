// Copyright 2017 Istio Authors
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

package kube

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// PodCache is an eventually consistent pod cache
type PodCache struct {
	cacheHandler

	sync.RWMutex
	// keys maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP.
	// This should only contain RUNNING or PENDING pods with an allocated IP.
	keys map[string]string

	c *Controller
}

func newPodCache(ch cacheHandler, c *Controller) *PodCache {
	out := &PodCache{
		cacheHandler: ch,
		c:            c,
		keys:         make(map[string]string),
	}

	ch.handler.Append(func(obj interface{}, ev model.Event) error {
		return out.event(obj, ev)
	})
	return out
}

// event updates the IP-based index (pc.keys).
func (pc *PodCache) event(obj interface{}, ev model.Event) error {
	pc.Lock()
	defer pc.Unlock()

	// When a pod is deleted obj could be an *v1.Pod or a DeletionFinalStateUnknown marker item.
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %+v", obj)
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a pod %#v", obj)
		}
	}

	ip := pod.Status.PodIP
	// PodIP will be empty when pod is just created, but before the IP is assigned
	// via UpdateStatus.

	if len(ip) > 0 {
		log.Infof("Handling event %s for pod %s in namespace %s -> %v", ev, pod.Name, pod.Namespace, ip)
		key := KeyFunc(pod.Name, pod.Namespace)
		switch ev {
		case model.EventAdd:
			switch pod.Status.Phase {
			case v1.PodPending, v1.PodRunning:
				// add to cache if the pod is running or pending
				pc.keys[ip] = key
				if pc.c != nil && pc.c.XDSUpdater != nil {
					pc.c.XDSUpdater.WorkloadUpdate(ip, pod.ObjectMeta.Labels, pod.ObjectMeta.Annotations)
				}
			}
		case model.EventUpdate:
			switch pod.Status.Phase {
			case v1.PodPending, v1.PodRunning:
				// add to cache if the pod is running or pending
				pc.keys[ip] = key
				if pc.c != nil && pc.c.XDSUpdater != nil {
					pc.c.XDSUpdater.WorkloadUpdate(ip, pod.ObjectMeta.Labels, pod.ObjectMeta.Annotations)
				}
			default:
				// delete if the pod switched to other states and is in the cache
				if pc.keys[ip] == key {
					delete(pc.keys, ip)
					if pc.c != nil && pc.c.XDSUpdater != nil {
						pc.c.XDSUpdater.WorkloadUpdate(ip, nil, nil)
					}
				}
			}
		case model.EventDelete:
			// delete only if this pod was in the cache
			if pc.keys[ip] == key {
				delete(pc.keys, ip)
				if pc.c != nil && pc.c.XDSUpdater != nil {
					pc.c.XDSUpdater.WorkloadUpdate(ip, nil, nil)
				}
			}
		}
	}
	return nil
}

// nolint: unparam
func (pc *PodCache) getPodKey(addr string) (string, bool) {
	pc.RLock()
	defer pc.RUnlock()
	key, exists := pc.keys[addr]
	return key, exists
}

// getPodByIp returns the pod or nil if pod not found or an error occurred
func (pc *PodCache) getPodByIP(addr string) *v1.Pod {
	pc.RLock()
	defer pc.RUnlock()

	key, exists := pc.keys[addr]
	if !exists {
		return nil
	}
	item, exists, err := pc.informer.GetStore().GetByKey(key)
	if !exists || err != nil {
		return nil
	}
	return item.(*v1.Pod)
}

// labelsByIP returns pod labels or nil if pod not found or an error occurred
func (pc *PodCache) labelsByIP(addr string) (model.Labels, bool) {
	pod := pc.getPodByIP(addr)
	if pod == nil {
		return nil, false
	}
	return convertLabels(pod.ObjectMeta), true
}
