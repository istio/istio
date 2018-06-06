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
	"log"
	"sync"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
)

// PodCache is an eventually consistent pod cache
// TODO: rename the file to 'pod.go' (cache is too generic)
type PodCache struct {
	rwMu sync.RWMutex
	cacheHandler

	// keys maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP.
	// This should only contain RUNNING or PENDING pods with an allocated IP.
	keys map[string]string
}

func newPodCache(ch cacheHandler) *PodCache {
	out := &PodCache{
		cacheHandler: ch,
		keys:         make(map[string]string),
	}

	ch.handler.Append(func(obj interface{}, ev model.Event) error {
		out.rwMu.Lock()
		defer out.rwMu.Unlock()

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

		log.Printf("Handling event %s for pod %s in namespace %s -> %v", ev, pod.Name, pod.Namespace, ip)

		if len(ip) > 0 {
			key := KeyFunc(pod.Name, pod.Namespace)
			switch ev {
			case model.EventAdd:
				switch pod.Status.Phase {
				case v1.PodPending, v1.PodRunning:
					// add to cache if the pod is running or pending
					out.keys[ip] = key
				}
			case model.EventUpdate:
				switch pod.Status.Phase {
				case v1.PodPending, v1.PodRunning:
					// add to cache if the pod is running or pending
					out.keys[ip] = key
				default:
					// delete if the pod switched to other states and is in the cache
					if out.keys[ip] == key {
						delete(out.keys, ip)
					}
				}
			case model.EventDelete:
				// delete only if this pod was in the cache
				if out.keys[ip] == key {
					delete(out.keys, ip)
				}
			}
		}
		return nil
	})
	return out
}

func (pc *PodCache) getPodKey(addr string) (string, bool) {
	pc.rwMu.RLock()
	defer pc.rwMu.RUnlock()
	key, exists := pc.keys[addr]
	return key, exists
}

// getPodByIp returns the pod or nil if pod not found or an error occurred
func (pc *PodCache) getPodByIP(addr string) (*v1.Pod, bool) {
	pc.rwMu.RLock()
	defer pc.rwMu.RUnlock()

	key, exists := pc.keys[addr]
	if !exists {
		return nil, false
	}
	item, exists, err := pc.informer.GetStore().GetByKey(key)
	if !exists || err != nil {
		return nil, false
	}
	return item.(*v1.Pod), true
}

// labelsByIP returns pod labels or nil if pod not found or an error occurred
func (pc *PodCache) labelsByIP(addr string) (model.Labels, bool) {
	pod, exists := pc.getPodByIP(addr)
	if !exists {
		return nil, false
	}
	return convertLabels(pod.ObjectMeta), true
}
