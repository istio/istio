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
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResourceLock_Lock(t *testing.T) {
	g := NewGomegaWithT(t)
	r1 := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:   "r1",
			Version: "r1",
		},
		Namespace:       "r1",
		Name:            "r1",
		Generation: "11",
	}
	r1a := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:   "r1",
			Version: "r1",
		},
		Namespace:       "r1",
		Name:            "r1",
		Generation: "12",
	}
	var runCount int32
	workers := NewQueue(10*time.Second, func(_ *cacheEntry) error {
		atomic.AddInt32(&runCount, 1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}, 10)
	workers.Push(r1, Progress{})
	workers.Push(r1, Progress{})
	workers.Push(r1a, Progress{})
	time.Sleep(40 * time.Millisecond)
	result := atomic.LoadInt32(&runCount)
	g.Expect(result).To(Equal(int32(1)))
}
