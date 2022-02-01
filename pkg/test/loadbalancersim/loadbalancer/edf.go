//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package loadbalancer

import (
	"container/heap"
)

// Entry is an item for load balance
type Entry struct {
	deadline float64
	index    int64
	value    interface{}
	weight   float64
}

// priorityQueue is a queue that always pop the highest priority item
type priorityQueue []*Entry

// Len implements heap.Interface/sort.Interface
func (pq priorityQueue) Len() int { return len(pq) }

// Less implements heap.Interface/sort.Interface
func (pq priorityQueue) Less(i, j int) bool {
	// Flip logic to make this a min queue.
	if pq[i].deadline == pq[j].deadline {
		return pq[i].index < pq[j].index
	}
	return pq[i].deadline < pq[j].deadline
}

// Swap implements heap.Interface/sort.Interface
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

// Push implements heap.Interface for pushing an item into the heap
func (pq *priorityQueue) Push(x interface{}) {
	entry := x.(*Entry)
	*pq = append(*pq, entry)
}

// Pop implements heap.Interface for poping an item from the heap
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	entry := old[n-1]
	*pq = old[0 : n-1]
	return entry
}

// EDF implements the Earliest Deadline First scheduling algorithm
type EDF struct {
	pq              *priorityQueue
	currentIndex    int64
	currentDeadline float64
}

// Add a new entry for load balance
func (e *EDF) Add(weight float64, value interface{}) {
	e.currentIndex++
	heap.Push(e.pq, &Entry{
		value:    value,
		weight:   weight,
		deadline: e.currentDeadline + 1/weight,
		index:    e.currentIndex,
	})
}

// PickAndAdd picks an available entry and re-adds it with the given weight calculation
func (e *EDF) PickAndAdd(calcWeight func(prevWeight float64, value interface{}) float64) interface{} {
	// if no available entry, return nil
	if len(*e.pq) == 0 {
		return nil
	}
	entry := heap.Pop(e.pq).(*Entry)
	// currentDeadline should be entry's deadline so that new added entry would have a fair
	// competition environment with the old ones
	e.currentDeadline = entry.deadline

	// Re-add it with the updated weight.
	e.Add(calcWeight(entry.weight, entry.value), entry.value)
	return entry.value
}

// NewEDF create a new edf scheduler
func NewEDF() *EDF {
	pq := make(priorityQueue, 0)
	return &EDF{
		pq:           &pq,
		currentIndex: 0,
	}
}
