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

package model

import (
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

type XdsCacheImpl struct {
	cds typedXdsCache[uint64]
	eds typedXdsCache[uint64]
	rds typedXdsCache[uint64]
	sds typedXdsCache[string]
}

// XdsCache interface defines a store for caching XDS responses.
// All operations are thread safe.
type XdsCache interface {
	// Run starts a background thread to flush evicted indexes periodically.
	Run(stop <-chan struct{})
	// Add adds the given XdsCacheEntry with the value for the given pushContext to the cache.
	// If the cache has been updated to a newer push context, the write will be dropped silently.
	// This ensures stale data does not overwrite fresh data when dealing with concurrent
	// writers.
	Add(entry XdsCacheEntry, pushRequest *PushRequest, value *discovery.Resource)
	// Get retrieves the cached value if it exists.
	Get(entry XdsCacheEntry) *discovery.Resource
	// Clear removes the cache entries that are dependent on the configs passed.
	Clear(sets.Set[ConfigKey])
	// ClearAll clears the entire cache.
	ClearAll()
	// Keys returns all currently configured keys for the type. This is for testing/debug only
	Keys(t string) []any
	// Snapshot returns a snapshot of all values. This is for testing/debug only
	Snapshot() []*discovery.Resource
}

// XdsCacheEntry interface defines functions that should be implemented by
// resources that can be cached.
type XdsCacheEntry interface {
	// Type indicates the type of Xds resource being cached like CDS.
	Type() string
	// Key is the key to be used in cache.
	Key() any
	// DependentConfigs is config items that this cache key is dependent on.
	// Whenever these configs change, we should invalidate this cache entry.
	DependentConfigs() []ConfigHash
	// Cacheable indicates whether this entry is valid for cache. For example
	// for EDS to be cacheable, the Endpoint should have corresponding service.
	Cacheable() bool
}

const (
	CDSType = "cds"
	EDSType = "eds"
	RDSType = "rds"
	SDSType = "sds"
)

// NewXdsCache returns an instance of a cache.
func NewXdsCache() XdsCache {
	cache := XdsCacheImpl{
		eds: newTypedXdsCache[uint64](),
	}
	if features.EnableCDSCaching {
		cache.cds = newTypedXdsCache[uint64]()
	} else {
		cache.cds = disabledCache[uint64]{}
	}
	if features.EnableRDSCaching {
		cache.rds = newTypedXdsCache[uint64]()
	} else {
		cache.rds = disabledCache[uint64]{}
	}

	cache.sds = newTypedXdsCache[string]()

	return cache
}

func (x XdsCacheImpl) Run(stop <-chan struct{}) {
	interval := features.XDSCacheIndexClearInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				x.cds.Flush()
				x.eds.Flush()
				x.rds.Flush()
				x.sds.Flush()
			case <-stop:
				return
			}
		}
	}()
}

func (x XdsCacheImpl) Add(entry XdsCacheEntry, pushRequest *PushRequest, value *discovery.Resource) {
	if !entry.Cacheable() {
		return
	}
	k := entry.Key()
	switch entry.Type() {
	case CDSType:
		key := k.(uint64)
		x.cds.Add(key, entry, pushRequest, value)
	case EDSType:
		key := k.(uint64)
		x.eds.Add(key, entry, pushRequest, value)
	case SDSType:
		key := k.(string)
		x.sds.Add(key, entry, pushRequest, value)
	case RDSType:
		key := k.(uint64)
		x.rds.Add(key, entry, pushRequest, value)
	default:
		log.Errorf("unknown type %s", entry.Type())
	}
}

func (x XdsCacheImpl) Get(entry XdsCacheEntry) *discovery.Resource {
	if !entry.Cacheable() {
		return nil
	}

	k := entry.Key()
	switch entry.Type() {
	case CDSType:
		key := k.(uint64)
		return x.cds.Get(key)
	case EDSType:
		key := k.(uint64)
		return x.eds.Get(key)
	case SDSType:
		key := k.(string)
		return x.sds.Get(key)
	case RDSType:
		key := k.(uint64)
		return x.rds.Get(key)
	default:
		log.Errorf("unknown type %s", entry.Type())
		return nil
	}
}

func (x XdsCacheImpl) Clear(s sets.Set[ConfigKey]) {
	x.cds.Clear(s)
	// clear all EDS cache for PA change
	if HasConfigsOfKind(s, kind.PeerAuthentication) {
		x.eds.ClearAll()
	} else {
		x.eds.Clear(s)
	}
	x.rds.Clear(s)
	x.sds.Clear(s)
}

func (x XdsCacheImpl) ClearAll() {
	x.cds.ClearAll()
	x.eds.ClearAll()
	x.rds.ClearAll()
	x.sds.ClearAll()
}

func (x XdsCacheImpl) Keys(t string) []any {
	switch t {
	case CDSType:
		keys := x.cds.Keys()
		return convertToAnySlices(keys)
	case EDSType:
		keys := x.eds.Keys()
		return convertToAnySlices(keys)
	case SDSType:
		keys := x.sds.Keys()
		return convertToAnySlices(keys)
	case RDSType:
		keys := x.rds.Keys()
		return convertToAnySlices(keys)
	default:
		return nil
	}
}

func convertToAnySlices[K comparable](in []K) []any {
	out := make([]any, len(in))
	for i, k := range in {
		out[i] = k
	}
	return out
}

func (x XdsCacheImpl) Snapshot() []*discovery.Resource {
	var out []*discovery.Resource
	out = append(out, x.cds.Snapshot()...)
	out = append(out, x.eds.Snapshot()...)
	out = append(out, x.rds.Snapshot()...)
	out = append(out, x.sds.Snapshot()...)
	return out
}

// DisabledCache is a cache that is always empty
type DisabledCache struct{}

func (d DisabledCache) Run(stop <-chan struct{}) {
}

func (d DisabledCache) Add(entry XdsCacheEntry, pushRequest *PushRequest, value *discovery.Resource) {
}

func (d DisabledCache) Get(entry XdsCacheEntry) *discovery.Resource {
	return nil
}

func (d DisabledCache) Clear(s sets.Set[ConfigKey]) {
}

func (d DisabledCache) ClearAll() {
}

func (d DisabledCache) Keys(t string) []any {
	return nil
}

func (d DisabledCache) Snapshot() []*discovery.Resource {
	return nil
}

var _ XdsCache = &DisabledCache{}
