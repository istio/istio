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

package controller

import (
	"sync"
	"time"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/util/sets"
)

// The wait added by batching happens before the XDSUpdater is told about a change, so it is not part of
// pilot_debounce_time, pilot_proxy_queue_time or pilot_proxy_convergence_time. These metrics make it visible.
var (
	endpointUpdatesBatched = monitoring.NewSum(
		"pilot_k8s_endpoint_updates_batched_total",
		"Endpoint events of large Services whose update was held back to be batched.",
	)

	endpointBatchFlushes = monitoring.NewSum(
		"pilot_k8s_endpoint_batch_flushes_total",
		"Batched endpoint updates performed for large Services.",
	)

	endpointBatchDelay = monitoring.NewDistribution(
		"pilot_k8s_endpoint_batch_delay_seconds",
		"Delay in seconds between the first held back endpoint event of a Service and its batched update.",
		[]float64{.01, .1, .5, 1, 3, 5, 10, 20, 30},
	)
)

// largeEndpointBatchThreshold is the number of endpoints a Service must exceed before its endpoint
// updates are batched. It is a variable only so that tests can lower it.
var largeEndpointBatchThreshold = 5000

type endpointBatchKey struct {
	hostname  host.Name
	namespace string
}

type endpointBatchState struct {
	// first is when the first event of this batch was held back.
	first time.Time
	// push is set when an EndpointSlice changed: the flush sends the Service's endpoints with EDSUpdate.
	push bool
	// rebuild is set when a pod label changed: the flush rebuilds the cached endpoints of every slice
	// of the Service before sending them.
	rebuild bool
}

// endpointBatcher defers the expensive part of an endpoint update for large Services. Handling one
// EndpointSlice or pod label event otherwise costs time proportional to the size of the Service,
// because the full endpoint list is rebuilt and compared each time. With features.BatchLargeEndpointUpdates
// the per-slice cache is still updated right away, but the full list is rebuilt and handed to the
// XDSUpdater at most once per features.BatchLargeEndpointUpdatesDebounceTime.
type endpointBatcher struct {
	esc *endpointSliceController

	mu      sync.Mutex
	pending map[endpointBatchKey]*endpointBatchState
}

func newEndpointBatcher(esc *endpointSliceController) *endpointBatcher {
	return &endpointBatcher{
		esc:     esc,
		pending: make(map[endpointBatchKey]*endpointBatchState),
	}
}

// batch defers the update for the Service and reports true, or reports false when the caller must do
// the update inline: batching is disabled, the controller is still doing its initial sync, or the
// Service is not large and has nothing pending.
func (b *endpointBatcher) batch(hostname host.Name, namespace string, push, rebuild bool) bool {
	if !features.BatchLargeEndpointUpdates {
		return false
	}
	// Never hold back the initial sync: istiod must not report ready with a stale endpoint index.
	if !b.esc.c.queue.HasSynced() {
		return false
	}
	key := endpointBatchKey{hostname: hostname, namespace: namespace}
	b.mu.Lock()
	defer b.mu.Unlock()
	state, pending := b.pending[key]
	if !pending {
		if b.esc.endpointCache.count(hostname) <= largeEndpointBatchThreshold {
			return false
		}
		state = &endpointBatchState{first: time.Now()}
		b.pending[key] = state
		// Flush on the controller queue so that it is serialized with every other event. The first
		// event starts the wait; later events join it without extending it.
		time.AfterFunc(features.BatchLargeEndpointUpdatesDebounceTime, func() {
			b.esc.c.queue.Push(func() error {
				b.flush(key)
				return nil
			})
		})
	}
	state.push = state.push || push
	state.rebuild = state.rebuild || rebuild
	endpointUpdatesBatched.Increment()
	return true
}

// flush performs the deferred update for the Service, if there is one.
func (b *endpointBatcher) flush(key endpointBatchKey) {
	b.mu.Lock()
	state := b.pending[key]
	delete(b.pending, key)
	b.mu.Unlock()
	if state == nil {
		return
	}
	endpointBatchFlushes.Increment()
	endpointBatchDelay.Record(time.Since(state.first).Seconds())

	c := b.esc.c
	rebuilt := false
	if state.rebuild {
		if svc := c.GetService(key.hostname); svc != nil {
			rebuilt = true
			endpoints := c.buildEndpointsForService(svc, true)
			if !state.push && len(endpoints) > 0 {
				c.opts.XDSUpdater.EDSCacheUpdate(model.ShardKeyFromRegistry(c), string(key.hostname), key.namespace, endpoints)
			}
		}
	}
	if state.push {
		b.esc.pushEDS([]host.Name{key.hostname}, key.namespace)
	}
	if rebuilt {
		c.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: string(key.hostname), Namespace: key.namespace}),
			Reason:         model.NewReasonStats(model.EndpointUpdate),
		})
	}
}
