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
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

// TestLockLoop is a regression test for an issue that cause infinite loops in the worker handler
func TestLockLoop(t *testing.T) {
	mgr := NewManager(nil)
	g := make(chan struct{}, 10)
	fakefunc := func(status Manipulator, context any) {
	}
	allowWork := make(chan struct{})
	c1 := mgr.CreateIstioStatusController(fakefunc)
	wp := NewWorkerPool(func(c *config.Config) error {
		g <- struct{}{}
		<-allowWork
		return nil
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
	pool := wp.(*WorkerPool)
	assert.EventuallyEqual(t, func() uint {
		pool.lock.Lock()
		defer pool.lock.Unlock()
		return pool.workerCount
	}, 0)
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
	c1 := mgr.CreateIstioStatusController(fakefunc)
	c2 := mgr.CreateIstioStatusController(fakefunc)
	workers := NewWorkerPool(func(_ *config.Config) error {
		return nil
	}, func(resource Resource) *config.Config {
		// The live object is at whatever generation was last observed and pushed.
		gen, _ := strconv.ParseInt(resource.Generation, 10, 64)
		return &config.Config{
			Meta: config.Meta{Generation: gen},
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

// TestWorkQueuePushUpdatesGeneration is a regression test for status updates being silently dropped.
// A second Push for an object that is still queued must carry the newer generation forward, otherwise
// the worker sees a generation mismatch against the live object and drops the write without retrying.
func TestWorkQueuePushUpdatesGeneration(t *testing.T) {
	g := NewWithT(t)
	wq := WorkQueue{cache: map[lockResource]cacheEntry{}}
	ctl := &Controller{}
	res := Resource{
		GroupVersionResource: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "backendtlspolicies"},
		Namespace:            "default",
		Name:                 "policy",
	}

	gen1 := res
	gen1.Generation = "1"
	wq.Push(gen1, ctl, "status for generation 1")

	// the object is updated before the worker gets to it
	gen2 := res
	gen2.Generation = "2"
	wq.Push(gen2, ctl, "status for generation 2")

	target, progress := wq.Pop(sets.New[lockResource]())
	g.Expect(target.Generation).To(Equal("2"))
	g.Expect(progress[ctl]).To(Equal("status for generation 2"))
}

// TestWorkerPoolSkipsStaleGeneration verifies the guard that stops a status computed for an old
// generation from being written over a newer object.
func TestWorkerPoolSkipsStaleGeneration(t *testing.T) {
	g := NewWithT(t)
	wrote := make(chan struct{}, 1)
	mgr := NewManager(nil)
	c := mgr.CreateIstioStatusController(func(status Manipulator, context any) {})
	workers := NewWorkerPool(func(_ *config.Config) error {
		wrote <- struct{}{}
		return nil
	}, func(_ Resource) *config.Config {
		// live object has moved on
		return &config.Config{Meta: config.Meta{Generation: 12}}
	}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workers.Run(ctx)

	stale := Resource{
		GroupVersionResource: schema.GroupVersionResource{Group: "r1", Version: "r1"},
		Namespace:            "r1",
		Name:                 "r1",
		Generation:           "11",
	}
	workers.Push(stale, c, nil)

	g.Consistently(wrote, 100*time.Millisecond).ShouldNot(Receive())
}

func TestWorkerPoolStopsConflictRetry(t *testing.T) {
	conflict := kerrors.NewConflict(schema.GroupResource{Resource: "httproutes"}, "route", errors.New("stale resourceVersion"))
	for _, tt := range []struct {
		name   string
		err    error
		next   *config.Config
		cancel bool
		reads  int32
	}{
		{name: "deleted", err: conflict, reads: 2},
		{name: "newer generation", err: conflict, next: &config.Config{Meta: config.Meta{Generation: 12}}, reads: 2},
		{name: "non-conflict error", err: errors.New("write failed"), reads: 1},
		{name: "shutdown", err: conflict, cancel: true, reads: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reads, writes atomic.Int32
			cached := &config.Config{Meta: config.Meta{Generation: 11}, Status: &v1alpha1.IstioStatus{}}
			workers := NewWorkerPool(func(*config.Config) error {
				writes.Add(1)
				if tt.cancel {
					cancel()
				}
				return tt.err
			}, func(Resource) *config.Config {
				if reads.Add(1) == 1 {
					return cached
				}
				return tt.next
			}, 1)
			workers.Run(ctx)
			workers.Push(Resource{Name: "route", Generation: "11"}, &Controller{fn: func(Manipulator, any) {}}, nil)
			assert.EventuallyEqual(t, writes.Load, 1)
			assert.EventuallyEqual(t, reads.Load, tt.reads)
			pool := workers.(*WorkerPool)
			assert.EventuallyEqual(t, func() uint {
				pool.lock.Lock()
				defer pool.lock.Unlock()
				return pool.workerCount
			}, 0)
			assert.Equal(t, reads.Load(), tt.reads)
			assert.Equal(t, writes.Load(), 1)
			// Failed writes must not mutate the cached status and hide the need to retry.
			assert.Equal(t, cached.Status.(*v1alpha1.IstioStatus).ObservedGeneration, int64(0))
		})
	}
}

func TestWorkerPoolSkipsRecreatedResource(t *testing.T) {
	original := config.Config{Meta: config.Meta{GroupVersionKind: gvk.HTTPRoute, Name: "route", Namespace: "default", UID: "old", Generation: 1}}
	replacement := original.DeepCopy()
	replacement.UID = "new"
	var reads, writes atomic.Int32
	workers := NewWorkerPool(func(*config.Config) error {
		if writes.Add(1) == 1 {
			return kerrors.NewConflict(schema.GroupResource{Resource: "httproutes"}, "route", errors.New("deleted and recreated"))
		}
		return nil
	}, func(Resource) *config.Config {
		if reads.Add(1) == 1 {
			return &original
		}
		return &replacement
	}, 1)
	workers.Run(test.NewContext(t))
	workers.Push(ResourceFromModelConfig(original), &Controller{fn: func(s Manipulator, _ any) { s.SetInner("old status") }}, nil)
	assert.EventuallyEqual(t, reads.Load, 2)
	pool := workers.(*WorkerPool)
	assert.EventuallyEqual(t, func() uint {
		pool.lock.Lock()
		defer pool.lock.Unlock()
		return pool.workerCount
	}, 0)
	assert.Equal(t, writes.Load(), 1, "must not apply the deleted object's status to its replacement")
}

func TestWorkQueueRecreatedResource(t *testing.T) {
	wq := WorkQueue{cache: map[lockResource]cacheEntry{}}
	original := config.Config{Meta: config.Meta{GroupVersionKind: gvk.HTTPRoute, Name: "route", Namespace: "default", UID: "old", Generation: 1}}
	replacement := original.DeepCopy()
	replacement.UID = "new"
	oldController, newController := &Controller{}, &Controller{}
	wq.Push(ResourceFromModelConfig(original), oldController, "old status")
	wq.Push(ResourceFromModelConfig(replacement), newController, "new status")
	_, progress := wq.Pop(sets.New[lockResource]())
	assert.Equal(t, progress, map[*Controller]any{newController: "new status"})
}

func TestWorkerPoolConflictDoesNotBlockQueuedWork(t *testing.T) {
	for _, name := range []string{"unrelated resource", "newer status"} {
		t.Run(name, func(t *testing.T) {
			firstWrite := make(chan struct{})
			releaseWrite := make(chan struct{})
			var attempts, completed atomic.Int32
			workers := NewWorkerPool(func(c *config.Config) error {
				if c.Status == "old" {
					if attempts.Add(1) == 1 {
						close(firstWrite)
						<-releaseWrite
					}
					return kerrors.NewConflict(schema.GroupResource{Resource: "httproutes"}, c.Name, errors.New("conflicting writer"))
				}
				completed.Add(1)
				return nil
			}, func(r Resource) *config.Config {
				return &config.Config{Meta: config.Meta{Name: r.Name, Generation: 1}}
			}, 1)
			workers.Run(test.NewContext(t))
			controller := &Controller{fn: func(s Manipulator, value any) { s.SetInner(value) }}
			target := Resource{Name: "conflicting", Generation: "1"}
			workers.Push(target, controller, "old")
			<-firstWrite
			if name == "unrelated resource" {
				target.Name = "healthy"
			}
			workers.Push(target, controller, "new")
			close(releaseWrite)
			assert.EventuallyEqual(t, completed.Load, 1)
		})
	}
}

func TestWorkQueueRetryPreservesLatestStatus(t *testing.T) {
	for _, beforeRetry := range []bool{false, true} {
		for _, change := range []string{"same resource", "new generation", "new UID"} {
			t.Run(fmt.Sprintf("%s/queuedBeforeRetry=%v", change, beforeRetry), func(t *testing.T) {
				wq := WorkQueue{cache: map[lockResource]cacheEntry{}}
				first, second := &Controller{}, &Controller{}
				target := Resource{Name: "route", UID: "old", Generation: "1"}
				latest := target
				if change == "new generation" {
					latest.Generation = "2"
				}
				if change == "new UID" {
					latest.UID = "new"
				}
				if beforeRetry {
					wq.Push(latest, first, "new")
				}
				wq.Retry(target, map[*Controller]any{first: "old", second: "other controller"})
				if !beforeRetry {
					wq.Push(latest, first, "new")
					assert.Equal(t, wq.Length(), 0)
				}
				wq.Ready(target)
				actual, progress := wq.Pop(sets.New[lockResource]())
				assert.Equal(t, actual, latest)
				want := map[*Controller]any{first: "new"}
				if change == "same resource" {
					want[second] = "other controller"
				}
				assert.Equal(t, progress, want)
				assert.Equal(t, wq.Length(), 0)
			})
		}
	}
}
