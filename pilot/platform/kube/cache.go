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
	"sync"

	"k8s.io/api/core/v1"

	"istio.io/pilot/model"
)

// PodCache is an eventually consistent pod cache
type PodCache struct {
	rwMu sync.RWMutex
	cacheHandler

	// keys maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP
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

		pod := *obj.(*v1.Pod)
		ip := pod.Status.PodIP
		if len(ip) > 0 {
			switch ev {
			case model.EventAdd, model.EventUpdate:
				out.keys[ip] = KeyFunc(pod.Name, pod.Namespace)
			case model.EventDelete:
				delete(out.keys, ip)
			}
		}
		return nil
	})
	return out
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
