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

package sink

import (
	"sync"
	"time"

	"github.com/gogo/googleapis/google/rpc"

	mcp "istio.io/api/mcp/v1alpha1"
)

// JournaledRequest is a common structure for journaling
// both mcp.MeshConfigRequest and mcp.RequestResources. It can be replaced with
// mcp.RequestResources once we fully switch over to the new API.
type JournaledRequest struct {
	VersionInfo   string
	Collection    string
	ResponseNonce string
	ErrorDetail   *rpc.Status
	SinkNode      *mcp.SinkNode
}

func (jr *JournaledRequest) ToMeshConfigRequest() *mcp.MeshConfigRequest {
	return &mcp.MeshConfigRequest{
		TypeUrl:       jr.Collection,
		VersionInfo:   jr.VersionInfo,
		ResponseNonce: jr.ResponseNonce,
		ErrorDetail:   jr.ErrorDetail,
		SinkNode:      jr.SinkNode,
	}
}

// RecentRequestInfo is metadata about a request that the client has sent.
type RecentRequestInfo struct {
	Time    time.Time
	Request *JournaledRequest
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

func (r *RecentRequestsJournal) RecordMeshConfigRequest(req *mcp.MeshConfigRequest) { // nolint:interfacer
	r.itemsMutex.Lock()
	defer r.itemsMutex.Unlock()

	item := RecentRequestInfo{
		Time: time.Now(),
		Request: &JournaledRequest{
			VersionInfo:   req.VersionInfo,
			Collection:    req.TypeUrl,
			ResponseNonce: req.ResponseNonce,
			ErrorDetail:   req.ErrorDetail,
			SinkNode:      req.SinkNode,
		},
	}

	r.items[r.next] = item

	r.next++
	if r.next == cap(r.items) {
		r.next = 0
	}
	if r.size < cap(r.items) {
		r.size++
	}
}

func (r *RecentRequestsJournal) RecordRequestResources(req *mcp.RequestResources) { // nolint:interfacer
	item := RecentRequestInfo{
		Time: time.Now(),
		Request: &JournaledRequest{
			Collection:    req.Collection,
			ResponseNonce: req.ResponseNonce,
			ErrorDetail:   req.ErrorDetail,
			SinkNode:      req.SinkNode,
		},
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
