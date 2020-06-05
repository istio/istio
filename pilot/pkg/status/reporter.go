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

package status

import (
	"context"
	"sync"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/pkg/log"
)

type Reporter struct {
	mu                     sync.RWMutex
	status                 map[string]string
	distributionEventQueue chan distributionEvent
}

var _ v2.DistributionStatusCache = &Reporter{}
var scope = log.RegisterScope("status", "tracks the status of config distribution in the mesh", 0)

// Starts the reporter, which watches dataplane ack's and resource changes so that it can update status leader
// with distribution information.  To run in read-only mode, (for supporting istioctl wait), set writeMode = false
func (r *Reporter) Start(stop <-chan struct{}) {
	scope.Info("Starting status follower controller")
	r.distributionEventQueue = make(chan distributionEvent, 10^5)
	r.status = make(map[string]string)
	go r.readFromEventQueue(NewIstioContext(stop))

}

type distributionEvent struct {
	conID            string
	distributionType v2.EventType
	nonce            string
}

func (r *Reporter) QueryLastNonce(conID string, distributionType v2.EventType) (noncePrefix string) {
	key := conID + string(distributionType)
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status[key]
}

// Register that a dataplane has acknowledged a new version of the config.
// Theoretically, we could use the ads connections themselves to harvest this data,
// but the mutex there is pretty hot, and it seems best to trade memory for time.
func (r *Reporter) RegisterEvent(conID string, distributionType v2.EventType, nonce string) {
	d := distributionEvent{nonce: nonce, distributionType: distributionType, conID: conID}
	select {
	case r.distributionEventQueue <- d:
		return
	default:
		scope.Errorf("Distribution Event Queue overwhelmed, status will be invalid.")
	}
}

func (r *Reporter) readFromEventQueue(ctx context.Context) {
	for ev := range r.distributionEventQueue {
		select {
		case <-ctx.Done():
			return
		default:
			// TODO might need to batch this to prevent lock contention
			r.processEvent(ev.conID, ev.distributionType, ev.nonce)
		}
	}

}
func (r *Reporter) processEvent(conID string, distributionType v2.EventType, nonce string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := conID + string(distributionType) // TODO: delimit?
	var version string
	if len(nonce) > 12 {
		version = nonce[:v2.VersionLen]
	} else {
		version = nonce
	}
	r.status[key] = version
}

// When a dataplane disconnects, we should no longer count it, nor expect it to ack config.
func (r *Reporter) RegisterDisconnect(conID string, types []v2.EventType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, xdsType := range types {
		key := conID + string(xdsType) // TODO: delimit?
		delete(r.status, key)
	}
}

func NewIstioContext(stop <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	return ctx
}
