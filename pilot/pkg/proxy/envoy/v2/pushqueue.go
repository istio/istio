// Copyright 2019 Istio Authors
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

package v2

import (
	"sync"

	"istio.io/istio/pilot/pkg/model"
)

type PushQueue struct {
	mu   *sync.RWMutex
	cond *sync.Cond

	// eventsMap stores all connections in the queue. If the same connection is enqueued again, the
	// PushEvents will be merged.
	eventsMap map[*XdsConnection]*model.PushRequest

	// connections maintains ordering of the queue
	connections []*XdsConnection

	// inProgress stores all connections that have been Dequeue(), but not MarkDone().
	// The value stored will be initially be nil, but may be populated if the connection is Enqueue().
	// If model.PushRequest is not nil, it will be Enqueued again once MarkDone has been called.
	inProgress map[*XdsConnection]*model.PushRequest
}

func NewPushQueue() *PushQueue {
	mu := &sync.RWMutex{}
	return &PushQueue{
		mu:         mu,
		eventsMap:  make(map[*XdsConnection]*model.PushRequest),
		inProgress: make(map[*XdsConnection]*model.PushRequest),
		cond:       sync.NewCond(mu),
	}
}

// Add will mark a proxy as pending a push. If it is already pending, pushInfo will be merged.
// edsUpdatedServices will be added together, and full will be set if either were full
func (p *PushQueue) Enqueue(proxy *XdsConnection, pushInfo *model.PushRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If its already in progress, merge the info and return
	if event, f := p.inProgress[proxy]; f {
		p.inProgress[proxy] = event.Merge(pushInfo)
		return
	}

	if event, f := p.eventsMap[proxy]; f {
		p.eventsMap[proxy] = event.Merge(pushInfo)
		return
	}

	p.eventsMap[proxy] = pushInfo
	p.connections = append(p.connections, proxy)
	// Signal waiters on Dequeue that a new item is available
	p.cond.Signal()
}

// Remove a proxy from the queue. If there are no proxies ready to be removed, this will block
func (p *PushQueue) Dequeue() (*XdsConnection, *model.PushRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Block until there is one to remove. Enqueue will signal when one is added.
	for len(p.connections) == 0 {
		p.cond.Wait()
	}

	head := p.connections[0]
	p.connections = p.connections[1:]

	info := p.eventsMap[head]
	delete(p.eventsMap, head)

	// Mark the connection as in progress
	p.inProgress[head] = nil

	return head, info
}

func (p *PushQueue) MarkDone(con *XdsConnection) {
	p.mu.Lock()

	info := p.inProgress[con]
	delete(p.inProgress, con)
	p.mu.Unlock()

	// If the info is present, that means Enqueue was called while connection was not yet marked done.
	// This means we need to add it back to the queue
	if info != nil {
		p.Enqueue(con, info)
	}
}

// Get number of pending proxies
func (p *PushQueue) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.connections)
}
