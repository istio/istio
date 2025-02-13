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

package queue

import (
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func TestOrdering(t *testing.T) {
	numValues := 1000

	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})
	defer close(stop)

	wg := sync.WaitGroup{}
	wg.Add(numValues)
	mu := sync.Mutex{}
	out := make([]int, 0)
	for i := 0; i < numValues; i++ {
		q.Push(func() error {
			mu.Lock()
			out = append(out, i)
			defer mu.Unlock()
			wg.Done()
			return nil
		})

		// Start the queue at the halfway point.
		if i == numValues/2 {
			go q.Run(stop)
		}
	}

	// wait for all task processed
	wg.Wait()

	if len(out) != numValues {
		t.Fatalf("expected output array length %d to equal %d", len(out), numValues)
	}

	for i := 0; i < numValues; i++ {
		if i != out[i] {
			t.Fatalf("expected out[%d] %v to equal %v", i, out[i], i)
		}
	}
}

func TestRetry(t *testing.T) {
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})
	defer close(stop)

	// Push a task that fails the first time and retries.
	wg := sync.WaitGroup{}
	wg.Add(2)
	failed := false
	q.Push(func() error {
		defer wg.Done()
		if failed {
			return nil
		}
		failed = true
		return errors.New("fake error")
	})

	go q.Run(stop)

	// wait for the task to run twice.
	wg.Wait()
}

func TestResourceFree(t *testing.T) {
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})
	signal := make(chan struct{})
	go func() {
		q.Run(stop)
		signal <- struct{}{}
	}()

	q.Push(func() error {
		t.Log("mock exec")
		return nil
	})

	// mock queue block wait cond signal
	time.AfterFunc(10*time.Millisecond, func() {
		close(stop)
	})

	select {
	case <-time.After(200 * time.Millisecond):
		t.Error("close stop, method exit timeout.")
	case <-signal:
		t.Log("queue return.")
	}
}

func TestClosed(t *testing.T) {
	t.Run("immediate close", func(t *testing.T) {
		stop := make(chan struct{})
		q := NewQueue(0)
		go q.Run(stop)
		close(stop)
		if err := WaitForClose(q, 10*time.Second); err != nil {
			t.Error(err)
		}
	})
	t.Run("no tasks after close", func(t *testing.T) {
		stop := make(chan struct{})
		q := NewQueue(0)
		taskComplete := atomic.NewBool(false)
		q.Push(func() error {
			close(stop)
			return nil
		})
		go q.Run(stop)
		if err := WaitForClose(q, 10*time.Second); err != nil {
			t.Error()
		}
		q.Push(func() error {
			taskComplete.Store(true)
			return nil
		})
		if taskComplete.Load() {
			t.Error("task ran on closed queue")
		}
	})
}

func TestSync(t *testing.T) {
	handles := atomic.NewInt32(0)
	task := func() error {
		handles.Inc()
		return nil
	}
	q := NewQueue(0)
	q.Push(task)
	stop := make(chan struct{})
	go q.Run(stop)
	retry.UntilOrFail(t, q.HasSynced, retry.Delay(time.Microsecond))
	// Must always be 1 since we are synced
	assert.Equal(t, handles.Load(), 1)
	close(stop)
	assert.NoError(t, WaitForClose(q, time.Second))
}
