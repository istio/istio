// Copyright 2017 Istio Authors
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
	"container/heap"
	"time"

	"istio.io/pkg/log"
)

type delayTask struct {
	do    func() error
	runAt time.Time
}

var _ heap.Interface = &pq{}

// pq implements an internal priority queue so that tasks with the soonest expiry will be run first
// much of this is taken from the example at https://golang.org/pkg/container/heap/
type pq []*delayTask

func (q pq) Len() int {
	return len(q)
}

func (q pq) Less(i, j int) bool {
	return q[i].runAt.Before(q[j].runAt)
}

func (q *pq) Swap(i, j int) {
	(*q)[i], (*q)[j] = (*q)[j], (*q)[i]
}

func (q *pq) Push(x interface{}) {
	*q = append(*q, x.(*delayTask))
}

func (q *pq) Pop() interface{} {
	old := *q
	n := len(old)
	if n == 0 {
		return nil
	}
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*q = old[0 : n-1]
	return item
}

func (q *pq) Peek() interface{} {
	if q.Len() < 1 {
		return nil
	}
	return (*q)[0]
}

type Delayed interface {
	Instance
	PushDelayed(t Task, delay time.Duration)
}

var _ Delayed = &delayQueue{}

func NewDelayed(workers int) Delayed {
	return &delayQueue{
		workers:  workers,
		register: make(chan *delayTask),
		run:      make(chan *delayTask),
	}
}

type delayQueue struct {
	workers int
	// incoming
	register chan *delayTask
	// outgoing
	run chan *delayTask
}

// PushDelayed will execute the task after waiting for the delay
func (d delayQueue) PushDelayed(t Task, delay time.Duration) {
	d.register <- &delayTask{do: t, runAt: time.Now().Add(delay)}
}

// Push will execute the task as soon as possible
func (d delayQueue) Push(task Task) {
	d.PushDelayed(task, 0)
}

func (d delayQueue) Run(stop <-chan struct{}) {
	for i := 0; i < d.workers; i++ {
		go d.work(stop)
	}

	// only needed while running
	q := &pq{}

	for {
		head := q.Peek()
		if head != nil {
			task := head.(*delayTask)
			delay := task.runAt.Sub(time.Now())
			if delay <= 0 {
				// actually remove the work item if we're ready to go
				heap.Pop(q)
				d.run <- task
			} else {
				// wait for another Push, or for them item to be ready
				select {
				case t := <-d.register:
					heap.Push(q, t)
				case <-time.After(delay):
					heap.Pop(q)
					d.run <- task
				case <-stop:
					return
				}
			}
		} else {
			// no items, wait for Push
			select {
			case t := <-d.register:
				q.Push(t)
			case <-stop:
				return
			}
		}
	}
}

func (d delayQueue) work(stop <-chan struct{}) {
	for {
		select {
		case t := <-d.run:
			if err := t.do(); err != nil {
				log.Errorf("Work item handle failed: %v", err)
				// TODO requeue?
			}
		case <-stop:
			return
		}
	}
}
