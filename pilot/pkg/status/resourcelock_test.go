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
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/test"
)

// TestLockLoop is a regression test for an issue that cause infinite loops in the worker handler
func TestLockLoop(t *testing.T) {
	mgr := NewManager(nil)
	g := make(chan struct{}, 10)
	fakefunc := func(status Manipulator, context any) {
	}
	allowWork := make(chan struct{})
	c1 := mgr.CreateGenericController(fakefunc)
	wp := NewWorkerPool(func(c *config.Config) {
		g <- struct{}{}
		<-allowWork
	}, func(resource Resource) *config.Config {
		return &config.Config{
			Meta: config.Meta{Generation: 11},
		}
	}, 10)
	go wp.Run(test.NewContext(t))

	r1 := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:   "r1",
			Version: "r1",
		},
		Namespace:  "r1",
		Name:       "r1",
		Generation: "11",
	}
	r2 := Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:   "r2",
			Version: "r2",
		},
		Namespace:  "r2",
		Name:       "r2",
		Generation: "12",
	}
	// Add a resource that takes a while
	wp.Push(r1, c1, nil)
	// Once it is in progress, and another resource
	<-g
	wp.Push(r2, c1, nil)
	// Now add the first resource again. This should be queued up
	wp.Push(r1, c1, nil)
	// In the issue we are regression testing, the time between now and closing AllowWork would infinite loop
	// Unfortunately, we don't really have a way to assert this is not infinite looping beyond looking at the logs manually..
	time.Sleep(time.Millisecond)
	// And should complete once we unblock the work
	close(allowWork)
	<-g
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
	x := make(chan struct{})
	y := make(chan struct{})
	mgr := NewManager(nil)
	fakefunc := func(status Manipulator, context any) {
		x <- struct{}{}
		atomic.AddInt32(&runCount, 1)
		y <- struct{}{}
	}
	c1 := mgr.CreateGenericController(fakefunc)
	c2 := mgr.CreateGenericController(fakefunc)
	workers := NewWorkerPool(func(_ *config.Config) {
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
