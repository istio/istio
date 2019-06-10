package v2

import (
	"sync"

	"istio.io/istio/pilot/pkg/model"
)

type PushInformation struct {
	// If not empty, it is used to indicate the event is caused by a change in the clusters.
	// Only EDS for the listed clusters will be sent.
	edsUpdatedServices map[string]struct{}

	push *model.PushContext

	full bool
}

type PushQueue struct {
	mu          sync.RWMutex
	connections map[*XdsConnection]*PushInformation
	order       []*XdsConnection
	signal      chan struct{}
}

func NewPushQueue() *PushQueue {
	return &PushQueue{
		connections: make(map[*XdsConnection]*PushInformation),
		signal:      make(chan struct{}),
	}
}

func (p *PushQueue) Add(proxy *XdsConnection, pushInfo *PushInformation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	info, exists := p.connections[proxy]
	if !exists {
		p.connections[proxy] = pushInfo
		p.order = append(p.order, proxy)
	} else {
		if info.edsUpdatedServices == nil && len(pushInfo.edsUpdatedServices) != 0 {
			info.edsUpdatedServices = map[string]struct{}{}
		}
		info.push = pushInfo.push
		info.full = info.full || pushInfo.full
		for endpoint := range pushInfo.edsUpdatedServices {
			info.edsUpdatedServices[endpoint] = struct{}{}
		}
	}
	select {
	case p.signal <- struct{}{}:
	default:
	}
}

func (p *PushQueue) waitForPendingPush() {
	p.mu.RLock()
	pending := len(p.order)
	if pending == 0 {
		p.mu.RUnlock()
		<-p.signal
	} else {
		p.mu.RUnlock()
	}
}

func (p *PushQueue) Remove() (*XdsConnection, *PushInformation) {
	p.waitForPendingPush()

	p.mu.Lock()
	defer p.mu.Unlock()
	head := p.order[0]
	p.order = p.order[1:]
	info := p.connections[head]
	delete(p.connections, head)
	return head, info
}

func (p *PushQueue) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.order)
}
