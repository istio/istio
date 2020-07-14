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

package internal

import (
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var (
	items = []struct {
		key string
		val int
	}{
		{"collection/1", 1},
		{"collection/2", 2},
		{"collection/3", 3},
		{"collection/4", 4},
		{"collection/5", 5},
		{"collection/6", 6},
	}
)

func TestUniqueQueue_InitialState(t *testing.T) {
	depth := 5
	q := NewUniqueScheduledQueue(depth)

	if !q.Empty() {
		t.Fatal("initial queue should be empty")
	}

	if q.Full() {
		t.Fatal("initial queue shouldn't be full")
	}

	k, v, ok := q.Dequeue()
	if ok {
		t.Fatalf("Dequeue() should not return an entry: got key=%q value=%v", k, v)
	}
}

func TestUniqueQueue_EnqueueDequeue(t *testing.T) {
	depth := int(5)
	q := NewUniqueScheduledQueue(depth)

	for i := 0; i < depth; i++ {
		q.Enqueue(items[i].key, items[i].val)
	}

	if !q.Full() {
		t.Fatalf("queue should be full")
	}

	// enqueue some duplicates
	for i := depth - 1; i >= 0; i-- {
		if q.Enqueue(items[i].key, items[i].val*2) != true {
			t.Fatalf("could not enqueue dup #%v", i)
		}
	}

	if !q.Full() {
		t.Fatalf("queue should be full")
	}

	if q.Enqueue(items[5].key, items[5].val) == true {
		t.Fatal("enqueueing new item into full queue should fail")
	}

	for i := 0; i < depth; i++ {
		<-q.Ready()

		want := items[i].val * 2
		key, v, _ := q.Dequeue()
		got := v.(int)
		if got != want {
			t.Fatalf("wrong Dequeue() value: got %v want %v", got, want)
		}
		q.Enqueue(key, got*2)
	}

	for i := 0; i < depth; i++ {
		want := items[i].val * 4
		_, v, _ := q.Dequeue()
		got := v.(int)
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
		_, v, ok := q.Dequeue()
		if !ok {
			t.Fatal("Dequeue() returned no entries")
		}
		return v
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for scheduled response")
	}
	return nil
}

func TestUniqueQueue_Schedule(t *testing.T) {
	q := NewUniqueScheduledQueue(5)

	// single enqueue / dequeue
	q.Enqueue(items[0].key, items[0].val)
	got := getScheduledItem(t, q)
	want := 1
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}

	// fill the queue and drain it
	for _, v := range []int{0, 1, 2, 3, 4} {
		q.Enqueue(items[v].key, items[v].val)
	}
	for _, v := range []int{0, 1, 2, 3, 4} {
		want := items[v].val
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	// fill the queue and verify unique property
	for _, v := range []int{1, 2, 3, 4, 0} {
		q.Enqueue(items[v].key, items[v].val)
	}
	// enqueue dups in a different order
	for _, v := range []int{4, 0, 1, 2, 3} {
		q.Enqueue(items[v].key, items[v].val)
	}

	for _, i := range []int{1, 2, 3, 4, 0} {
		want := items[i].val
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	select {
	case <-q.Done():
		t.Fatal("unexpected Done()")
	case <-q.Ready():
		t.Fatal("unexpected Ready()")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUniqueQueue_DequeueBeforeReady(t *testing.T) {
	q := NewUniqueScheduledQueue(5)

	for _, v := range []int{0, 1, 2, 3, 4} {
		q.Enqueue(items[v].key, items[v].val)
	}

	// // re-enqueue the first two dequeued items
	for range []int{0, 1} {
		key, item, _ := q.Dequeue()
		q.Enqueue(key, item)
	}

	for _, v := range []int{2, 3, 4, 0, 1} {
		want := items[v].val
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	// verify no more Ready() indications
	select {
	case <-q.Done():
		t.Fatal("unexpected Done()")
	case <-q.Ready():
		t.Fatal("unexpected Ready()")
	case <-time.After(100 * time.Millisecond):
	}

	// verify close

	q.Close()

	select {
	case <-q.Done():
		// pass
	case <-q.Ready():
		t.Fatal("unexpected Ready()")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Done() indication missed")
	}

	// test passes if nothing deadlocks
}

func TestUniqueQueue_Done(t *testing.T) {
	q := NewUniqueScheduledQueue(5)

	q.Enqueue(items[0].key, items[0].val)
	q.Enqueue(items[1].key, items[1].val)
	q.Enqueue(items[2].key, items[2].val)

	q.Close()

	q.Enqueue(items[4].key, items[4].val)
	q.Enqueue(items[5].key, items[5].val) // queue full
	q.Enqueue(items[1].key, items[1].val) // and a dup

	wanted := []int{0, 1, 2, 3, 4}

	for {
		select {
		case <-q.Done():
			return
		case <-q.Ready():
			_, v, _ := q.Dequeue()

			got := v.(int)
			if len(wanted) == 0 {
				t.Fatalf("got unexpected item: %v", got)
			}

			want := items[wanted[0]].val
			wanted = wanted[1:]

			if got != want {
				t.Fatalf("got %v want %v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for Done()")
		}
	}
}

func TestUniqueQueue_EnqueueAfterClose(t *testing.T) {
	q := NewUniqueScheduledQueue(5)

	for _, v := range []int{0, 1, 2, 3, 4} {
		q.Enqueue(items[v].key, items[v].val)
	}

	for _, v := range []int{0, 1, 2} {
		want := items[v].val
		got := getScheduledItem(t, q)
		if got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	q.Close()

	for _, v := range []int{3, 4} {
		want := items[v].val

		select {
		case <-q.Ready():
			_, got, ok := q.Dequeue()
			if !ok {
				t.Fatal("Dequeue() returned no entries")
			}
			if got != want {
				t.Fatalf("got %v want %v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for scheduled response")
		}
	}

	if q.Enqueue(items[0].key, items[0].val) != false {
		t.Fatal("Enqueue() after close should fail")
	}

	select {
	case <-q.Ready():
		k, v, ok := q.Dequeue()
		t.Fatalf("unexpected Dequeue() after close: got key=%v value=%v ok=%v", k, v, ok)
	case <-time.After(time.Second):
	}

	select {
	case <-q.Done():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Done()")
	}
}

func TestUniqueQueue_Dump(t *testing.T) {
	q := NewUniqueScheduledQueue(5)
	for i := 0; i < 5; i++ {
		q.Enqueue(items[i].key, items[i].val)
	}

	want := `{
   "closed":false,
   "queued_set":{  
      "collection/1":1,
      "collection/2":2,
      "collection/3":3,
      "collection/4":4,
      "collection/5":5
   },
   "queue":[  
      "collection/1",
      "collection/2",
      "collection/3",
      "collection/4",
      "collection/5",
      ""
   ],
   "head":0,
   "tail":5,
   "max_depth":5
}
`
	space := regexp.MustCompile(`\s+`)
	want = space.ReplaceAllString(want, "")
	got := q.Dump()

	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("\n got %v \nwant %v\n diff %v", got, want, diff)
	}

	jsonMarshalDumpHook = func(v interface{}) ([]byte, error) { return nil, errors.New("unmarhsal error") }
	defer func() { jsonMarshalDumpHook = json.Marshal }()

	want = ""
	got = q.Dump()
	if got != want {
		t.Fatalf("wrong output on json marshal error\n got %v \nwant %v", got, want)
	}
}
