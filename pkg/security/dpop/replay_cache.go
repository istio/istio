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

package dpop

import (
	"fmt"
	"sync"
	"time"
)

// replayEntry represents a cached DPoP proof JTI
type replayEntry struct {
	timestamp time.Time
}

// ReplayCache prevents replay attacks by tracking used JTI values
type ReplayCache struct {
	mu       sync.RWMutex
	cache    map[string]*replayEntry
	maxSize  int
	stopChan chan struct{}
}

// NewReplayCache creates a new replay cache
func NewReplayCache(maxSize int, cleanupInterval time.Duration) *ReplayCache {
	cache := &ReplayCache{
		cache:    make(map[string]*replayEntry),
		maxSize:  maxSize,
		stopChan: make(chan struct{}),
	}
	
	// Start cleanup goroutine
	go cache.cleanup(cleanupInterval)
	
	return cache
}

// CheckAndAdd checks if a JTI has been used before and adds it to the cache
func (r *ReplayCache) CheckAndAdd(jti string, iat time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Check if JTI already exists
	if _, exists := r.cache[jti]; exists {
		return fmt.Errorf("replay attack detected: JTI %s has already been used", jti)
	}
	
	// Add new entry
	r.cache[jti] = &replayEntry{
		timestamp: iat,
	}
	
	// Ensure cache doesn't exceed max size
	if len(r.cache) > r.maxSize {
		r.evictOldest()
	}
	
	return nil
}

// cleanup removes old entries from the cache
func (r *ReplayCache) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			r.cleanupOldEntries()
		case <-r.stopChan:
			return
		}
	}
}

// cleanupOldEntries removes entries older than the DPoP max age
func (r *ReplayCache) cleanupOldEntries() {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	cutoff := time.Now().Add(-DefaultDPoPMaxAge)
	for jti, entry := range r.cache {
		if entry.timestamp.Before(cutoff) {
			delete(r.cache, jti)
		}
	}
}

// evictOldest removes the oldest entry from the cache
func (r *ReplayCache) evictOldest() {
	var oldestJTI string
	var oldestTime time.Time
	
	for jti, entry := range r.cache {
		if oldestJTI == "" || entry.timestamp.Before(oldestTime) {
			oldestJTI = jti
			oldestTime = entry.timestamp
		}
	}
	
	if oldestJTI != "" {
		delete(r.cache, oldestJTI)
	}
}

// Close stops the cleanup goroutine
func (r *ReplayCache) Close() {
	close(r.stopChan)
}

// Size returns the current cache size
func (r *ReplayCache) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}
