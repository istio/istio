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
		i := i

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
