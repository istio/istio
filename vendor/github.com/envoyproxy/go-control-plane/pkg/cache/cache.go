// Copyright 2018 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

// Package cache defines a configuration cache for the server.
package cache

import (
	"context"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
)

// Request is an alias for the discovery request type.
type Request = v2.DiscoveryRequest

// ConfigWatcher requests watches for configuration resources by a node, last
// applied version identifier, and resource names hint. The watch should send
// the responses when they are ready. The watch can be cancelled by the
// consumer, in effect terminating the watch for the request.
// ConfigWatcher implementation must be thread-safe.
type ConfigWatcher interface {
	// CreateWatch returns a new open watch from a non-empty request.
	//
	// Value channel produces requested resources, once they are available.  If
	// the channel is closed prior to cancellation of the watch, an unrecoverable
	// error has occurred in the producer, and the consumer should close the
	// corresponding stream.
	//
	// Cancel is an optional function to release resources in the producer. If
	// provided, the consumer may call this function multiple times.
	CreateWatch(Request) (value chan Response, cancel func())
}

// Cache is a generic config cache with a watcher.
type Cache interface {
	ConfigWatcher

	// Fetch implements the polling method of the config cache using a non-empty request.
	Fetch(context.Context, Request) (*Response, error)

	// GetStatusInfo retrieves status information for a node ID.
	GetStatusInfo(string) StatusInfo

	// GetStatusKeys retrieves node IDs for all statuses.
	GetStatusKeys() []string
}

// Response is a pre-serialized xDS response.
type Response struct {
	// Request is the original request.
	Request v2.DiscoveryRequest

	// Version of the resources as tracked by the cache for the given type.
	// Proxy responds with this version as an acknowledgement.
	Version string

	// Resources to be included in the response.
	Resources []Resource
}
