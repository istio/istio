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

package xds

import (
	"sync"

	"istio.io/istio/pilot/pkg/model"
)

type PushQueue struct {
	cond *sync.Cond

	// pending stores all connections in the queue. If the same connection is enqueued again,
	// the PushRequest will be merged.
	pending map[*Connection]*model.PushRequest

	// queue maintains ordering of the queue
	queue []*Connection

	// processing stores all connections that have been Dequeue(), but not MarkDone().
	// The value stored will be initially be nil, but may be populated if the connection is Enqueue().
	// If model.PushRequest is not nil, it will be Enqueued again once MarkDone has been called.
	processing map[*Connection]*model.PushRequest
}

func NewPushQueue() *PushQueue {
	return &PushQueue{
		pending:    make(map[*Connection]*model.PushRequest),
		processing: make(map[*Connection]*model.PushRequest),
		cond:       sync.NewCond(&sync.Mutex{}),
	}
}

// Enqueue will mark a proxy as pending a push. If it is already pending, pushInfo will be merged.
// ServiceEntry updates will be added together, and full will be set if either were full
func (p *PushQueue) Enqueue(proxy *Connection, pushRequest *model.PushRequest) {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	if request, f := p.pending[proxy]; f {
		p.pending[proxy] = request.Merge(pushRequest)
		return
	}

	if request, f := p.processing[proxy]; f {
		p.pending[proxy] = request.Merge(pushRequest)
		return
	}

	p.pending[proxy] = pushRequest
	p.queue = append(p.queue, proxy)
	// Signal waiters on Dequeue that a new item is available
	p.cond.Signal()
}

// Remove a proxy from the queue. If there are no proxies ready to be removed, this will block
func (p *PushQueue) Dequeue() (con *Connection, request *model.PushRequest) {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	// Block until there is one to remove. Enqueue will signal when one is added.
	for len(p.queue) == 0 {
		p.cond.Wait()
	}

	con, p.queue = p.queue[0], p.queue[1:]

	request = p.pending[con]
	delete(p.pending, con)

	// Mark the connection as in progress
	p.processing[con] = nil

	return con, request
}

func (p *PushQueue) MarkDone(con *Connection) {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()
	delete(p.processing, con)
	// If the con is in pending list, that means Enqueue was called while the
	// connection was not yet marked done. This means we need to add it back to
	// the queue and signal the waiters.
	if _, f := p.pending[con]; f {
		p.queue = append(p.queue, con)
		p.cond.Signal()
	}
}

// Get number of pending proxies
func (p *PushQueue) Pending() int {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()
	return len(p.queue)
}
