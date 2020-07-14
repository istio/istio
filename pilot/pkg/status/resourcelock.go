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

package status

import "sync"

type ResourceLock struct {
	masterLock sync.RWMutex
	listing    map[interface{}]*sync.Mutex
}

func (r *ResourceLock) Lock(i interface{}) {
	r.retrieveMutex(i).Lock()
}

func (r *ResourceLock) Unlock(i interface{}) {
	r.retrieveMutex(i).Unlock()
}

// returns value indicating if init was necessary
func (r *ResourceLock) init() bool {
	if r.listing == nil {
		r.masterLock.Lock()
		defer r.masterLock.Unlock()
		// double check, per pattern
		if r.listing == nil {
			r.listing = make(map[interface{}]*sync.Mutex)
		}
		return true
	}
	return false
}

func (r *ResourceLock) retrieveMutex(i interface{}) *sync.Mutex {
	if !r.init() {
		r.masterLock.RLock()
		if result, ok := r.listing[i]; ok {
			r.masterLock.RUnlock()
			return result
		}
		// transition to write lock
		r.masterLock.RUnlock()
	}
	r.masterLock.Lock()
	defer r.masterLock.Unlock()
	r.listing[i] = &sync.Mutex{}
	return r.listing[i]
}
