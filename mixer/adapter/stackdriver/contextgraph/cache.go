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

package contextgraph

import "istio.io/istio/mixer/pkg/adapter"

type cacheStatus struct {
	lastSeen int
	lastSent int
}

// entityCache tracks the entities we've already seen.
// It is not thread-safe.
type entityCache struct {
	// cache maps an entity to the epoch it was last seen in.
	cache     map[entity]cacheStatus
	lastFlush int
	logger    adapter.Logger
}

func newEntityCache(logger adapter.Logger) *entityCache {
	return &entityCache{
		cache:     make(map[entity]cacheStatus),
		logger:    logger,
		lastFlush: -1,
	}
}

// AssertAndCheck reports the existence of e at epoch, and returns
// true if the entity needs to be sent immediately.
func (ec *entityCache) AssertAndCheck(e entity, epoch int) bool {
	cEpoch, ok := ec.cache[e]
	defer func() { ec.cache[e] = cEpoch }()
	if cEpoch.lastSeen < epoch {
		cEpoch.lastSeen = epoch
	}
	if !ok || cEpoch.lastSent < ec.lastFlush {
		ec.logger.Debugf("%q needs to be sent anew, old epoch: %d, now seen: %d",
			e.fullName, cEpoch.lastSent, epoch)
		cEpoch.lastSent = epoch
		return true
	}
	return false
}

// Flush returns the list of entities that have been asserted in the
// most recent epoch, to be reasserted.  It also cleans up stale
// entries from the cache.
func (ec *entityCache) Flush(epoch int) []entity {
	var result []entity
	for k, e := range ec.cache {
		if e.lastSeen <= ec.lastFlush {
			delete(ec.cache, k)
			continue
		}
		if e.lastSent == epoch {
			// Don't republish entities that are already in this batch.
			continue
		}
		e.lastSent = epoch
		ec.cache[k] = e
		result = append(result, k)
	}
	ec.lastFlush = epoch
	return result
}

// edgeCache tracks the edges we've already seen.
// It is not thread-safe.
type edgeCache struct {
	// cache maps an edge to the epoch it was last seen in.
	cache     map[edge]cacheStatus
	lastFlush int
	logger    adapter.Logger
}

func newEdgeCache(logger adapter.Logger) *edgeCache {
	return &edgeCache{
		cache:     make(map[edge]cacheStatus),
		logger:    logger,
		lastFlush: -1,
	}
}

// AssertAndCheck reports the existence of e at epoch, and returns
// true if the edge needs to be sent immediately.
func (ec *edgeCache) AssertAndCheck(e edge, epoch int) bool {
	cEpoch, ok := ec.cache[e]
	defer func() { ec.cache[e] = cEpoch }()
	if cEpoch.lastSeen < epoch {
		cEpoch.lastSeen = epoch
	}
	if !ok || cEpoch.lastSent < ec.lastFlush {
		ec.logger.Debugf("%v needs to be sent anew, old epoch: %d, now seen: %d",
			e, cEpoch.lastSent, epoch)
		cEpoch.lastSent = epoch
		return true
	}
	return false
}

// Flush returns the list of entities that have been asserted in the
// most recent epoch, to be reasserted.  It also cleans up stale
// entries from the cache.
func (ec *edgeCache) Flush(epoch int) []edge {
	var result []edge
	for k, e := range ec.cache {
		if e.lastSeen <= ec.lastFlush {
			delete(ec.cache, k)
			continue
		}
		if e.lastSent == epoch {
			// Don't republish entities that are already in this batch.
			continue
		}
		e.lastSent = epoch
		ec.cache[k] = e
		result = append(result, k)
	}
	ec.lastFlush = epoch
	return result
}

// Invalidate removes all edges with a source of fullName from the
// cache, so the next assertion will trigger a report.
func (ec *edgeCache) Invalidate(fullName string) {
	for e := range ec.cache {
		if e.sourceFullName == fullName {
			delete(ec.cache, e)
		}
	}
}
