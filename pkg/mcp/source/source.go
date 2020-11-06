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

package source

import (
	mcp "istio.io/api/mcp/v1alpha1"
)

// Request is a temporary abstraction for the MCP node request which can
// be used with the mcp.MeshConfigRequest and mcp.RequestResources. It can
// be removed once we fully cutover to mcp.RequestResources.
type Request struct {
	Collection string

	// Most recent version was that ACK/NACK'd by the sink
	VersionInfo string
	SinkNode    *mcp.SinkNode
}

// WatchResponse contains a versioned collection of pre-serialized resources.
type WatchResponse struct {
	Collection string

	// Version of the resources in the response for the given
	// type. The node responses with this version in subsequent
	// requests as an acknowledgment.
	Version string

	// Resourced resources to be included in the response.
	Resources []*mcp.Resource

	// The original request for triggered this response
	Request *Request
}

type (
	// CancelWatchFunc allows the consumer to cancel a previous watch,
	// terminating the watch for the request.
	CancelWatchFunc func()

	// PushResponseFunc allows the consumer to push a response for the
	// corresponding watch.
	PushResponseFunc func(*WatchResponse)
)

// Watcher requests watches for configuration resources by node, last
// applied version, and type. The watch should send the responses when
// they are ready. The watch can be canceled by the consumer.
type Watcher interface {
	// Watch returns a new open watch for a non-empty request.
	//
	// Cancel is an optional function to release resources in the
	// producer. It can be called idempotently to cancel and release resources.
	Watch(*Request, PushResponseFunc, string) CancelWatchFunc
}
