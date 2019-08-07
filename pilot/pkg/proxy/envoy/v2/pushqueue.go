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

type PushQueue struct {
	mu          *sync.RWMutex
	cond        *sync.Cond
	eventsMap   map[*XdsConnection]*PushEvent
	connections []*XdsConnection
}

func NewPushQueue() *PushQueue {
	mu := &sync.RWMutex{}
	return &PushQueue{
		mu:        mu,
		eventsMap: make(map[*XdsConnection]*PushEvent),
		cond:      sync.NewCond(mu),
	}
}

// Add will mark a proxy as pending a push. If it is already pending, pushInfo will be merged.
// edsUpdatedServices will be added together, and full will be set if either were full
func (p *PushQueue) Enqueue(proxy *XdsConnection, pushInfo *PushEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	event, exists := p.eventsMap[proxy]
	if !exists {
		p.eventsMap[proxy] = pushInfo
		p.connections = append(p.connections, proxy)
	} else {
		event.push = pushInfo.push
		event.full = event.full || pushInfo.full
		// When full push, do not care about edsUpdatedServices
		if !event.full {
			edsUpdates := map[string]struct{}{}
			for endpoint := range pushInfo.edsUpdatedServices {
				edsUpdates[endpoint] = struct{}{}
			}
			for endpoint := range event.edsUpdatedServices {
				edsUpdates[endpoint] = struct{}{}
			}
			event.edsUpdatedServices = edsUpdates
		} else {
			event.edsUpdatedServices = nil
		}
	}
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
	return head, info
}

// Get number of pending proxies
func (p *PushQueue) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.connections)
}
