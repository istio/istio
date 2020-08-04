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

package sink

import (
	"sync"
	"time"

	mcp "istio.io/api/mcp/v1alpha1"
)

// RecentRequestInfo is metadata about a request that the client has sent.
type RecentRequestInfo struct {
	Time    time.Time
	Request *mcp.RequestResources
}

// Acked indicates whether the message was an ack or not.
func (r RecentRequestInfo) Acked() bool {
	return r.Request.ErrorDetail == nil
}

const journalDepth = 32

// RecentRequestsJournal captures debug metadata about the latest requests that was sent by this client.
type RecentRequestsJournal struct {
	itemsMutex sync.Mutex
	items      []RecentRequestInfo
	next       int
	size       int
}

func NewRequestJournal() *RecentRequestsJournal {
	return &RecentRequestsJournal{
		items: make([]RecentRequestInfo, journalDepth),
	}
}

func (r *RecentRequestsJournal) RecordRequestResources(req *mcp.RequestResources) { // nolint:interfacer
	item := RecentRequestInfo{
		Time:    time.Now(),
		Request: req,
	}

	r.itemsMutex.Lock()
	defer r.itemsMutex.Unlock()

	r.items[r.next] = item

	r.next++
	if r.next == cap(r.items) {
		r.next = 0
	}
	if r.size < cap(r.items) {
		r.size++
	}
}

func (r *RecentRequestsJournal) Snapshot() []RecentRequestInfo {
	r.itemsMutex.Lock()
	defer r.itemsMutex.Unlock()

	var result []RecentRequestInfo

	if r.size < cap(r.items) {
		result = make([]RecentRequestInfo, r.next)
		copy(result, r.items[0:r.next])
	} else {
		result = make([]RecentRequestInfo, len(r.items))
		copy(result, r.items[r.next:])
		copy(result[cap(r.items)-r.next:], r.items[0:r.next])
	}

	return result
}
