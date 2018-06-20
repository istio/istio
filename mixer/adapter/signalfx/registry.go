// Copyright 2018 Istio Authors
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

package signalfx

import (
	"container/list"
	"sort"
	"sync"
	"time"

	"github.com/signalfx/golib/datapoint"
	"github.com/signalfx/golib/sfxclient"
)

type metricID string

// registry holds a set of collectors that produce datapoints on demand.  This
// allows us to smooth out the metrics we get to make them regular timeseries.
// We also need to expire collectors that haven't been updated after a certain
// amount of time to prevent excessive memory usage.
type registry struct {
	sync.RWMutex

	cumulatives    map[metricID]*cumulativeCollector
	rollingBuckets map[metricID]*sfxclient.RollingBucket

	// A linked list that we keep sorted by access time so that we can very
	// quickly tell which collectors are expired and should be deleted.
	lastAccessList list.List
	// A map optimizing lookup of the access time elements that are used in the
	// above linked list.
	lastAccesses map[metricID]*list.Element

	expiryTimeout time.Duration

	// This is the source of truth for the current time and exists to make unit
	// testing easier
	currentTime func() time.Time
}

var _ sfxclient.Collector = &registry{}

func newRegistry(expiryTimeout time.Duration) *registry {
	return &registry{
		cumulatives:    make(map[metricID]*cumulativeCollector),
		rollingBuckets: make(map[metricID]*sfxclient.RollingBucket),
		lastAccesses:   make(map[metricID]*list.Element),
		expiryTimeout:  expiryTimeout,
		currentTime:    time.Now,
	}
}

func (r *registry) RegisterOrGetCumulative(name string, dims map[string]string) *cumulativeCollector {
	id := idForMetric(name, dims)

	r.Lock()
	defer r.Unlock()

	if c := r.cumulatives[id]; c == nil {
		r.cumulatives[id] = &cumulativeCollector{
			MetricName: name,
			Dimensions: dims,
		}
	}

	r.markUsed(id)
	return r.cumulatives[id]
}

func (r *registry) RegisterOrGetRollingBucket(name string, dims map[string]string, intervalSeconds uint32) *sfxclient.RollingBucket {
	id := idForMetric(name, dims)

	r.Lock()
	defer r.Unlock()

	if c := r.rollingBuckets[id]; c == nil {
		r.rollingBuckets[id] = sfxclient.NewRollingBucket(name, dims)
		r.rollingBuckets[id].BucketWidth = time.Duration(intervalSeconds) * time.Second
	}

	r.markUsed(id)
	return r.rollingBuckets[id]
}

func (r *registry) Datapoints() []*datapoint.Datapoint {
	r.RLock()

	var out []*datapoint.Datapoint

	for id := range r.cumulatives {
		out = append(out, r.cumulatives[id].Datapoints()...)
	}

	for id := range r.rollingBuckets {
		out = append(out, r.rollingBuckets[id].Datapoints()...)
	}

	r.RUnlock()

	r.purgeOldCollectors()

	return out
}

type access struct {
	ts time.Time
	id metricID
}

// markUsed should be called to indicate that a metricID has been accessed and
// is still in use.  This causes it to move to the front of the lastAccessList
// list with an updated access timestamp.
// The registry lock should be held when calling this method.
func (r *registry) markUsed(id metricID) {
	// If this id is new, just push it to the front of the list and put it in
	// our map for quick lookup.
	if _, ok := r.lastAccesses[id]; !ok {
		elm := r.lastAccessList.PushFront(&access{
			ts: r.currentTime(),
			id: id,
		})
		r.lastAccesses[id] = elm
		return
	}
	// Otherwise, get the element from the map and scoot it up to the front of
	// the list with an updated timestamp.
	elm := r.lastAccesses[id]
	elm.Value.(*access).ts = r.currentTime()
	r.lastAccessList.MoveToFront(elm)
}

func (r *registry) purgeOldCollectors() {
	r.Lock()
	defer r.Unlock()

	now := r.currentTime()

	// Start at the back (end) of the linked list (which should always be
	// sorted by last access time) and remove any collectors that haven't been
	// accessed within the expiry timeout.
	elm := r.lastAccessList.Back()
	for elm != nil {
		acc := elm.Value.(*access)
		if now.Sub(acc.ts) <= r.expiryTimeout {
			// Since the list is sorted if we reach an element that isn't
			// expired we know no previous elements are expired.
			return
		}

		newElm := elm.Prev()
		// Remove zeros out prev/next so we have to copy previous first
		r.lastAccessList.Remove(elm)
		elm = newElm

		delete(r.cumulatives, acc.id)
		delete(r.rollingBuckets, acc.id)
		delete(r.lastAccesses, acc.id)
	}
}

func sortKeys(m map[string]string) []string {
	var keys sort.StringSlice
	for key := range m {
		keys = append(keys, key)
	}

	if keys != nil {
		keys.Sort()
	}
	return []string(keys)
}

// Construct a unique identifier that links metric instances with the same set
// of dimensions and monitored resource type/dimensions.
func idForMetric(name string, dims map[string]string) metricID {
	id := name + "|"

	for _, key := range sortKeys(dims) {
		id += key + ":" + dims[key] + "|"
	}

	return metricID(id)
}
