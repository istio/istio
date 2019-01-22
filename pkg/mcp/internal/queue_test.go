// Copyright 2018 Istio Authors
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

package internal

import (
	"testing"
	"time"
)

func setup() func() {
	prev := retryPushDelay
	retryPushDelay = time.Nanosecond
	return func() {
		retryPushDelay = prev
	}
}

func TestUniqueQueue_InitialState(t *testing.T) {
	defer setup()()

	depth := 5
	q := NewUniqueScheduledQueue(depth)

	if !q.Empty() {
		t.Fatal("initial queue should be empty")
	}

	if q.Full() {
		t.Fatal("initial queue shouldn't be full")
	}

	a := q.Dequeue()
	if a != nil {
		t.Fatal("Dequeue() should return nil")
	}
}

func TestUnique_EnqueueDequeue(t *testing.T) {
	defer setup()()

	depth := int(5)
	q := NewUniqueScheduledQueue(depth)

	for i := 0; i < depth; i++ {
		q.Enqueue(i)
	}

	if !q.Full() {
		t.Fatalf("queue should be full")
	}

	// enqueue some duplicates
	if q.Enqueue(0) != true {
		t.Fatal("could not enqueue first dup")
	}
	if q.Enqueue(1) != true {
		t.Fatal("could not enqueue second dup")
	}

	if !q.Full() {
		t.Fatalf("queue should be full")
	}

	if q.Enqueue(42) == true {
		t.Fatal("enqueueing new item into full queue should fail")
	}

	for want := 0; want < depth; want++ {
		got := q.Dequeue().(int)
		if got != want {
			t.Fatalf("wrong Dequeue() value: got %v want %v", got, want)
		}
	}
}

func getScheduledItem(t *testing.T, q *UniqueQueue) interface{} {
	t.Helper()

	select {
	case <-q.Done():
		t.Fatal("unexpected done indication")
	case <-q.Ready():
		return q.Dequeue()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for scheduled response")
	}
	t.Fatal("unreachable")
	return nil
}

func TestUnique_Schedule(t *testing.T) {
	defer setup()()

	q := NewUniqueScheduledQueue(5)

	// single enqueue / dequeue
	q.Enqueue(1)
	got := getScheduledItem(t, q)
	want := 1
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}

	// fill the queue and drain it
	for _, v := range []int{1, 2, 3, 4, 5} {
		q.Enqueue(v)
	}
	for _, want := range []int{1, 2, 3, 4, 5} {
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	// fill the queue and verify unique property
	for _, v := range []int{2, 3, 4, 5, 1} {
		q.Enqueue(v)
	}
	// enqueue dups in a different order
	for _, v := range []int{5, 1, 2, 3, 4} {
		q.Enqueue(v)
	}

	for _, want := range []int{2, 3, 4, 5, 1} {
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestUnique_ScheduleRetry(t *testing.T) {
	defer setup()()

	q := NewUniqueScheduledQueue(5)

	order := []int{1, 2, 3, 4, 5}
	for _, v := range order {
		q.Enqueue(v)
	}

	for i := 0; i < 100; i++ {
		q.Schedule()
	}

	for _, want := range order {
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	for i := 0; i < 100; i++ {
		<-q.Ready()
	}

	select {
	case <-q.Ready():
		t.Fatal("unexpected queue Ready()")
	case <-time.After(time.Second):
	}
}

func TestUnique_Done(t *testing.T) {
	defer setup()()

	q := NewUniqueScheduledQueue(5)

	q.Enqueue(1)
	q.Enqueue(2)
	q.Enqueue(3)

	q.Close()

	q.Enqueue(4)
	q.Enqueue(5) // queue full
	q.Enqueue(1) // and a dup

	wanted := []int{1, 2, 3, 4, 5}

	for {
		select {
		case <-q.Done():
			return
		case <-q.Ready():
			got := q.Dequeue().(int)

			if len(wanted) == 0 {
				t.Fatalf("got unexpected item: %v", got)
			}

			want := wanted[0]
			wanted = wanted[1:]

			if got != want {
				t.Fatalf("got %v want %v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for scheduled response")
		}
	}
}
