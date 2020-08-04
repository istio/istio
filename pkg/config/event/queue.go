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

package event

const (
	defaultSizeIncrement = 32
)

// A circular queue for events that can expand as needed. Queue is not thread-safe. It needs to be protected externally
// in a concurrent context.
type queue struct {
	items []Event
	head  int
	end   int
}

// add a new item to the queue.
func (q *queue) add(e Event) {
	if q.isFull() {
		q.expand()
	}

	q.items[q.end] = e
	q.end = incAndWrap(q.end, len(q.items))
}

func (q *queue) pop() (Event, bool) {
	if q.isEmpty() {
		return Event{}, false
	}

	idx := wrap(q.head, len(q.items))
	q.head = incAndWrap(q.head, len(q.items))

	return q.items[idx], true
}

func (q *queue) clear() {
	q.items = nil
	q.head = 0
	q.end = 0
}

func (q *queue) expand() {
	oldSize := len(q.items)
	newSize := len(q.items) + defaultSizeIncrement
	old := q.items
	oldh := q.head

	q.items = make([]Event, newSize)
	q.head = 0
	q.end = 0

	for i := 0; i < oldSize-1; i++ {
		idx := wrap(oldh+i, oldSize)
		q.add(old[idx])
	}
}

func (q *queue) size() int {
	h := q.head
	e := q.end
	if e < h {
		e += len(q.items)
	}

	return e - h
}

func (q *queue) isFull() bool {
	l := len(q.items)
	if l == 0 {
		return true
	}

	e := q.end + 1
	e = wrap(e, l)
	return e == q.head
}

func (q *queue) isEmpty() bool {
	return q.head == q.end
}

func wrap(i, length int) int {
	if length == 0 {
		return i
	}
	return i % length
}

func incAndWrap(i, length int) int {
	i++
	return wrap(i, length)
}
