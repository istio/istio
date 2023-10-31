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
	"sync/atomic"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pkg/config"
)

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
	x := make(chan struct{})
	y := make(chan struct{})
	mgr := NewManager(nil)
	fakefunc := func(status *v1alpha1.IstioStatus, context any) *v1alpha1.IstioStatus {
		x <- struct{}{}
		atomic.AddInt32(&runCount, 1)
		y <- struct{}{}
		return nil
	}
	c1 := mgr.CreateIstioStatusController(fakefunc)
	c2 := mgr.CreateIstioStatusController(fakefunc)
	workers := NewWorkerPool(func(_ *config.Config, _ any) {
	}, func(resource Resource) *config.Config {
		return &config.Config{
			Meta: config.Meta{Generation: 11},
		}
	}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	workers.Run(ctx)
	workers.Push(r1, c1, nil)
	workers.Push(r1, c2, nil)
	workers.Push(r1, c1, nil)
	<-x
	<-y
	<-x
	workers.Push(r1, c1, nil)
	workers.Push(r1a, c1, nil)
	<-y
	<-x
	select {
	case <-x:
		t.FailNow()
	default:
	}
	<-y
	result := atomic.LoadInt32(&runCount)
	g.Expect(result).To(Equal(int32(3)))
	cancel()
}
