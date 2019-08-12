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
	"time"

	"istio.io/istio/pilot/pkg/model"
)

type PushEvent struct {
	// If not empty, it is used to indicate the event is caused by a change in the clusters.
	// Only EDS for the listed clusters will be sent.
	edsUpdatedServices map[string]struct{}

	push *model.PushContext

	// start represents the time a push was started.
	start time.Time

	full bool
}

func (event *PushEvent) Merge(other *PushEvent) *PushEvent {
	if event == nil {
		return other
	}
	if other == nil {
		return event
	}
	merged := &PushEvent{}
	merged.push = other.push
	merged.full = event.full || other.full
	merged.start = event.start

	// When full push, do not care about edsUpdatedServices
	if !merged.full {
		edsUpdates := map[string]struct{}{}
		for endpoint := range other.edsUpdatedServices {
			edsUpdates[endpoint] = struct{}{}
		}
		for endpoint := range event.edsUpdatedServices {
			edsUpdates[endpoint] = struct{}{}
		}
		merged.edsUpdatedServices = edsUpdates
	}
	return merged
}

type PushQueue struct {
	mu   *sync.RWMutex
	cond *sync.Cond

	// eventsMap stores all connections in the queue. If the same connection is enqueued again, the
	// PushEvents will be merged.
	eventsMap map[*XdsConnection]*PushEvent

	// connections maintains ordering of the queue
	connections []*XdsConnection

	// inProgress stores all connections that have been Dequeue(), but not MarkDone().
	// The value stored will be initially be nil, but may be populated if the connection is Enqueue().
	// If PushEvent is not nil, it will be Enqueued again once MarkDone has been called.
	inProgress map[*XdsConnection]*PushEvent
}

func NewPushQueue() *PushQueue {
	mu := &sync.RWMutex{}
	return &PushQueue{
		mu:         mu,
		eventsMap:  make(map[*XdsConnection]*PushEvent),
		inProgress: make(map[*XdsConnection]*PushEvent),
		cond:       sync.NewCond(mu),
	}
}

// Add will mark a proxy as pending a push. If it is already pending, pushInfo will be merged.
// edsUpdatedServices will be added together, and full will be set if either were full
func (p *PushQueue) Enqueue(proxy *XdsConnection, pushInfo *PushEvent) {
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
func (p *PushQueue) Dequeue() (*XdsConnection, *PushEvent) {
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
