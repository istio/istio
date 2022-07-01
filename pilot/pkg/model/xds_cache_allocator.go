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

package model

// Note: referenced kubernetes optimization: staging/src/k8s.io/apimachinery/pkg/runtime/allocator.go

// Allocator knows how to allocate memory
// It exists to make the cost of xds cache Add cheaper.
// In some cases, it allows for allocating memory only once and then reusing it.
// This approach puts less load on GC and leads to less fragmented memory in general.
type Allocator struct {
	configs []ConfigKey
}

// Allocate reserves memory for n configs only if the underlying array doesn't have enough capacity
// otherwise it returns previously allocated block of memory.
//
// Note that the returned array is not zeroed, it is the caller's
// responsibility to clean the memory if needed.
func (a *Allocator) Allocate(n int) []ConfigKey {
	if cap(a.configs) >= n {
		return a.configs[:0]
	}
	// grow the buffer
	size := 2*cap(a.configs) + n
	a.configs = make([]ConfigKey, 0, size)
	return a.configs
}
