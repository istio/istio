//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package provider

import (
	"istio.io/istio/galley/pkg/model/resource"
)

// Interface to be implemented by a source configuration provider.
type Interface interface {
	// Start the source interface. This returns a channel that the runtime will listen to for configuration
	// change events. The initial state of the underlying config store should be reflected as a series of
	// Added events, followed by a FullSync event.
	Start() (chan Event, error)

	// Stop the source interface. Upon return from this method, the channel should not be accumulating any
	// more events.
	Stop()

	// Get return the resource with the given key. The type information is self contained within the id.
	Get(id resource.Key) (resource.Entry, error)
}
