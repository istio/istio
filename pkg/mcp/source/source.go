// Copyright 2019 Istio Authors
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
	"time"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/monitoring"
)

const DefaultRetryPushDelay = 10 * time.Millisecond

// Request is a temporary abstraction for the MCP client request which can
// be used with the mcp.MeshConfigRequest and mcp.RequestResources. It can
// be removed once we fully cutover to mcp.RequestResources.
type Request struct {
	Collection  string
	VersionInfo string
	SinkNode    *mcp.SinkNode
}

// WatchResponse contains a versioned collection of pre-serialized resources.
type WatchResponse struct {
	Collection string

	// Version of the resources in the response for the given
	// type. The client responses with this version in subsequent
	// requests as an acknowledgment.
	Version string

	// Resourced resources to be included in the response.
	Resources []*mcp.Resource
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
	Watch(*Request, PushResponseFunc) CancelWatchFunc
}

// CollectionOptions configures the per-collection updates.
type CollectionOptions struct {
	// Name of the collection, e.g. istio/networking/v1alpha3/VirtualService
	Name string
}

// CollectionsOptionsFromSlice returns a slice of collection options from
// a slice of collection names.
func CollectionOptionsFromSlice(names []string) []CollectionOptions {
	options := make([]CollectionOptions, 0, len(names))
	for _, name := range names {
		options = append(options, CollectionOptions{
			Name: name,
		})
	}
	return options
}

// Options contains options for configuring MCP sources.
type Options struct {
	Watcher            Watcher
	CollectionsOptions []CollectionOptions
	Reporter           monitoring.Reporter

	// Controls the delay for re-retrying a configuration push if the previous
	// attempt was not possible, e.errgrp. the lower-level serving layer was busy. This
	// should typically be set fairly small (order of milliseconds).
	RetryPushDelay time.Duration
}
