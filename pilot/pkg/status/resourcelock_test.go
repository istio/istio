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

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/tests/util/leak"
)

func TestMain(m *testing.M) {
	leak.CheckMain(m)
}

func TestResourceLock_Lock(t *testing.T) {
	g := NewGomegaWithT(t)
	r1 := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:   "r1",
			Version: "r1",
		},
		Namespace:  "r1",
		Name:       "r1",
		Generation: "11",
	}
	r1a := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:   "r1",
			Version: "r1",
		},
		Namespace:  "r1",
		Name:       "r1",
		Generation: "12",
	}
	var runCount int32
	var m sync.Mutex
	m.Lock()
	workers := NewQueue(100*time.Second, func(a cacheEntry) {
		m.Unlock()
		atomic.AddInt32(&runCount, 1)
	}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	workers.Run(ctx)
	workers.Push(r1, Progress{1, 1})
	m.Lock()
	workers.Push(r1, Progress{2, 2})
	workers.Push(r1a, Progress{3, 3})
	m.Lock()
	result := atomic.LoadInt32(&runCount)
	g.Expect(result).To(Equal(int32(2)))
	cancel()
}
