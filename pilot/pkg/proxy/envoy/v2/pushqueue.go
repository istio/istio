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

type PushInformation struct {
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
	connections map[*XdsConnection]*PushInformation
	order       []*XdsConnection
}

func NewPushQueue() *PushQueue {
	mu := &sync.RWMutex{}
	return &PushQueue{
		mu:          mu,
		connections: make(map[*XdsConnection]*PushInformation),
		cond:        sync.NewCond(mu),
	}
}

// Add will mark a proxy as pending a push. If it is already pending, pushInfo will be merged.
// edsUpdatedServices will be added together, and full will be set if either were full
func (p *PushQueue) Enqueue(proxy *XdsConnection, pushInfo *PushInformation) {
	p.mu.Lock()
	defer p.mu.Unlock()

	info, exists := p.connections[proxy]
	if !exists {
		p.connections[proxy] = pushInfo
		p.order = append(p.order, proxy)
	} else {
		info.push = pushInfo.push
		info.full = info.full || pushInfo.full

		edsUpdates := map[string]struct{}{}
		for endpoint := range pushInfo.edsUpdatedServices {
			edsUpdates[endpoint] = struct{}{}
		}
		for endpoint := range info.edsUpdatedServices {
			edsUpdates[endpoint] = struct{}{}
		}
		info.edsUpdatedServices = edsUpdates
	}
	p.cond.Signal()
}

// Remove a proxy from the queue. If there are no proxies ready to be removed, this will block
func (p *PushQueue) Dequeue() (*XdsConnection, *PushInformation) {
	p.mu.Lock()
	// Block until there is one to remove. Enqueue will signal when one is added.
	for len(p.order) == 0 {
		p.cond.Wait()
	}

	defer p.mu.Unlock()
	head := p.order[0]
	p.order = p.order[1:]
	info := p.connections[head]
	delete(p.connections, head)
	return head, info
}

// Get number of pending proxies
func (p *PushQueue) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.order)
}
