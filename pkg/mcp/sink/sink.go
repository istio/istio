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

package sink

import (
	"sync"

	"github.com/gogo/protobuf/proto"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/monitoring"
)

// Object contains a decoded versioned object with metadata received from the server.
type Object struct {
	TypeURL  string
	Metadata *mcp.Metadata
	Body     proto.Message
}

// changes is a collection of configuration objects of the same protobuf type.
type Change struct {
	Collection string

	// List of resources to add/update. The interpretation of this field depends
	// on the value of Incremental.
	//
	// When Incremental=True, the list only includes new/updated resources.
	//
	// When Incremental=False, the list includes the full list of resources.
	// Any previously received resources not in this list should be deleted.
	Objects []*Object

	// List of deleted resources by name. The resource name corresponds to the
	// resource's metadata name.
	//
	// Ignore when Incremental=false.
	Removed []string

	// When true, the set of changes represents an incremental resource update. The
	// `Objects` is a list of added/update resources and `Removed` is a list of delete
	// resources.
	//
	// When false, the set of changes represents a full-state update for the specified
	// type. Any previous resources not included in this update should be removed.
	Incremental bool
}

// Updater provides configuration changes in batches of the same protobuf message type.
type Updater interface {
	// Apply is invoked when the node receives new configuration updates
	// from the server. The caller should return an error if any of the provided
	// configuration resources are invalid or cannot be applied. The node will
	// propagate errors back to the server accordingly.
	Apply(*Change) error
}

// InMemoryUpdater is an implementation of Updater that keeps a simple in-memory state.
type InMemoryUpdater struct {
	items      map[string][]*Object
	itemsMutex sync.Mutex
}

var _ Updater = &InMemoryUpdater{}

// NewInMemoryUpdater returns a new instance of InMemoryUpdater
func NewInMemoryUpdater() *InMemoryUpdater {
	return &InMemoryUpdater{
		items: make(map[string][]*Object),
	}
}

// Apply the change to the InMemoryUpdater.
func (u *InMemoryUpdater) Apply(c *Change) error {
	u.itemsMutex.Lock()
	defer u.itemsMutex.Unlock()
	u.items[c.Collection] = c.Objects
	return nil
}

// Get current state for the given collection.
func (u *InMemoryUpdater) Get(collection string) []*Object {
	u.itemsMutex.Lock()
	defer u.itemsMutex.Unlock()
	return u.items[collection]
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

// Options contains options for configuring MCP sinks.
type Options struct {
	CollectionOptions []CollectionOptions
	Updater           Updater
	ID                string
	Metadata          map[string]string
	Reporter          monitoring.Reporter
}
