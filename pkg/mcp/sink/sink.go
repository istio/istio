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
	"io"
	"sort"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/status"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

type perCollectionState struct {
	// tracks resource versions that we've successfully ACK'd
	versions map[string]string

	// determines when incremental delivery is enabled for this collection
	requestIncremental bool
}

// Sink implements the resource sink message exchange for MCP. It can be instantiated by client and server
// sink implementations to manage the MCP message exchange.
type Sink struct {
	mu    sync.Mutex
	state map[string]*perCollectionState

	nodeInfo *mcp.SinkNode
	updater  Updater
	journal  *RecentRequestsJournal
	metadata map[string]string
	reporter monitoring.Reporter
}

// New creates a new resource sink.
func New(options *Options) *Sink {
	nodeInfo := &mcp.SinkNode{
		Id:          options.ID,
		Annotations: options.Metadata,
	}

	state := make(map[string]*perCollectionState)
	for _, collection := range options.CollectionOptions {
		state[collection.Name] = &perCollectionState{
			versions:           make(map[string]string),
			requestIncremental: collection.Incremental,
		}
	}

	return &Sink{
		state:    state,
		nodeInfo: nodeInfo,
		updater:  options.Updater,
		metadata: options.Metadata,
		reporter: options.Reporter,
		journal:  NewRequestJournal(),
	}
}

// Probe point for test code to determine when the node is finished processing responses.
var handleResponseDoneProbe = func() {}

func (sink *Sink) sendNACKRequest(response *mcp.Resources, err error) *mcp.RequestResources {
	errorDetails, _ := status.FromError(err)

	scope.Errorf("MCP: sending NACK for nonce=%v: error=%q", response.Nonce, err)
	sink.reporter.RecordRequestNack(response.Collection, 0, errorDetails.Code())

	req := &mcp.RequestResources{
		SinkNode:      sink.nodeInfo,
		Collection:    response.Collection,
		ResponseNonce: response.Nonce,
		ErrorDetail:   errorDetails.Proto(),
	}
	return req
}

func (sink *Sink) handleResponse(resources *mcp.Resources) *mcp.RequestResources {
	if handleResponseDoneProbe != nil {
		defer handleResponseDoneProbe()
	}

	state, ok := sink.state[resources.Collection]
	if !ok {
		errDetails := status.Errorf(codes.Unimplemented, "unsupported collection %v", resources.Collection)
		return sink.sendNACKRequest(resources, errDetails)
	}

	change := &Change{
		Collection:        resources.Collection,
		Objects:           make([]*Object, 0, len(resources.Resources)),
		Removed:           resources.RemovedResources,
		Incremental:       resources.Incremental,
		SystemVersionInfo: resources.SystemVersionInfo,
	}

	for _, resource := range resources.Resources {
		var dynamicAny types.DynamicAny
		if err := types.UnmarshalAny(resource.Body, &dynamicAny); err != nil {
			return sink.sendNACKRequest(resources, err)
		}

		// TODO - use galley metadata to verify collection and type_url match?
		object := &Object{
			TypeURL:  resource.Body.TypeUrl,
			Metadata: resource.Metadata,
			Body:     dynamicAny.Message,
		}
		change.Objects = append(change.Objects, object)
	}

	if err := sink.updater.Apply(change); err != nil {
		errDetails := status.Error(codes.InvalidArgument, err.Error())
		return sink.sendNACKRequest(resources, errDetails)
	}

	// update version tracking if change is successfully applied
	sink.mu.Lock()
	internal.UpdateResourceVersionTracking(state.versions, resources)
	useIncremental := state.requestIncremental
	sink.mu.Unlock()

	// ACK
	sink.reporter.RecordRequestAck(resources.Collection, 0)
	req := &mcp.RequestResources{
		SinkNode:      sink.nodeInfo,
		Collection:    resources.Collection,
		ResponseNonce: resources.Nonce,
		Incremental:   useIncremental,
	}
	return req
}

func (sink *Sink) createInitialRequests() []*mcp.RequestResources {
	sink.mu.Lock()

	initialRequests := make([]*mcp.RequestResources, 0, len(sink.state))
	for collection, state := range sink.state {
		var initialResourceVersions map[string]string

		if state.requestIncremental {
			initialResourceVersions = make(map[string]string, len(state.versions))
			for name, version := range state.versions {
				initialResourceVersions[name] = version
			}
		}

		req := &mcp.RequestResources{
			SinkNode:                sink.nodeInfo,
			Collection:              collection,
			InitialResourceVersions: initialResourceVersions,
			Incremental:             state.requestIncremental,
		}
		initialRequests = append(initialRequests, req)
	}
	sink.mu.Unlock()

	return initialRequests
}

// ProcessStream implements the MCP message exchange for the resource sink. It accepts the sink
// stream interface and returns when a send or receive error occurs. The caller is responsible for
// handling gRPC client/server specific error handling.
func (sink *Sink) ProcessStream(stream Stream) error {
	// send initial requests for each supported type
	initialRequests := sink.createInitialRequests()
	for {
		var req *mcp.RequestResources

		if len(initialRequests) > 0 {
			req = initialRequests[0]
			initialRequests = initialRequests[1:]
		} else {
			resources, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					sink.reporter.RecordRecvError(err, status.Code(err))
					scope.Errorf("Error receiving MCP resource: %v", err)
				}
				return err
			}
			req = sink.handleResponse(resources)
		}

		sink.journal.RecordRequestResources(req)

		if err := stream.Send(req); err != nil {
			sink.reporter.RecordSendError(err, status.Code(err))
			scope.Errorf("Error sending MCP request: %v", err)
			return err
		}
	}
}

// SnapshotRequestInfo returns a snapshot of the last known set of request results.
func (sink *Sink) SnapshotRequestInfo() []RecentRequestInfo {
	return sink.journal.Snapshot()
}

// Metadata that is originally supplied when creating this sink.
func (sink *Sink) Metadata() map[string]string {
	r := make(map[string]string, len(sink.metadata))
	for k, v := range sink.metadata {
		r[k] = v
	}
	return r
}

// ID is the node id for this sink.
func (sink *Sink) ID() string {
	return sink.nodeInfo.Id
}

// Collections returns the resource collections that this sink requests.
func (sink *Sink) Collections() []string {
	result := make([]string, 0, len(sink.state))

	for k := range sink.state {
		result = append(result, k)
	}
	sort.Strings(result)

	return result
}

// Object contains a decoded versioned object with metadata received from the server.
type Object struct {
	TypeURL  string
	Metadata *mcp.Metadata
	Body     proto.Message
}

// Change is a collection of configuration objects of the same protobuf type.
type Change struct {
	Collection string

	// List of resources to add/update. The interpretation of this field depends
	// on the value of Incremental.
	//
	// When Incremental=True, the list only includes new/updateReceivedForStream resources.
	//
	// When Incremental=False, the list includes the full list of resources.
	// Any previously received resources not in this list should be deleted.
	Objects []*Object

	// List of deleted resources by name. The resource name corresponds to the
	// resource's metadata name (namespace/name).
	//
	// Ignore when Incremental=false.
	Removed []string

	// When true, the set of changes represents an requestIncremental resource update. The
	// `Objects` is a list of added/update resources and `Removed` is a list of delete
	// resources.
	//
	// When false, the set of changes represents a full-state update for the specified
	// type. Any previous resources not included in this update should be removed.
	Incremental bool

	// SystemVersionInfo is the version of the response data (used for debugging purposes only).
	SystemVersionInfo string
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

	// When true, the sink requests incremental updates from the source. Incremental
	// updates are requested when this option is true. Incremental updates are only
	// used if the sink requests it (per request) and the source decides to make use of it.
	Incremental bool
}

// CollectionOptionsFromSlice returns a slice of collection options from
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

// Stream is for sending RequestResources messages and receiving Resource messages.
type Stream interface {
	Send(*mcp.RequestResources) error
	Recv() (*mcp.Resources, error)
}
