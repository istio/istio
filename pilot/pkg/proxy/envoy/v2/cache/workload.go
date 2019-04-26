// Copyright 2019 Istio Authors
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

package cache

import (
	"encoding/json"
	"sync"
)

// Workload has the minimal info we need to detect if we need to push workloads, and to
// cache data to avoid expensive model allocations.
type Workload struct {
	// Labels
	Labels map[string]string
}

// WorkloadsById keeps track of information about a workload, based on direct notifications
// from registry. This acts as a cache and allows detecting changes.
type WorkloadsByID struct {
	mutex sync.RWMutex

	// key is workload ip
	cache map[string]*Workload
}

func (w *WorkloadsByID) Delete(key string) {
	w.mutex.Lock()
	delete(w.cache, key)
	w.mutex.Unlock()
}

func (w *WorkloadsByID) Get(key string) (*Workload, bool) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	workload, ok := w.cache[key]
	return workload, ok
}

func (w *WorkloadsByID) Set(key string, value *Workload) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.cache == nil {
		w.cache = make(map[string]*Workload)
	}
	w.cache[key] = value
}

func (w *WorkloadsByID) ToBytes() []byte {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	out, _ := json.MarshalIndent(w.cache, " ", " ")
	return out
}
