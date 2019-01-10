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
	"io"
	"sort"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/monitoring"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

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

// Stream is for sending RequestResources messages and receiving Resource messages.
type Stream interface {
	Send(*mcp.RequestResources) error
	Recv() (*mcp.Resources, error)
}

type perCollectionState struct {
	versions map[string]string
	updated  bool
}

// Sink implements the resource sink message exchange for MCP. It can be instantiated by client and server
// sink implementations to manage the MCP message exchange.
type Sink struct {
	mu    sync.Mutex
	state map[string]*perCollectionState

	nodeInfo *mcp.SinkNode
	updater  Updater
	journal  *recentRequestsJournal
	metadata map[string]string
	reporter monitoring.Reporter
}

type Options struct {
	Collections []string
	Updater     Updater
	ID          string
	Metadata    map[string]string
	Reporter    monitoring.Reporter
}

// NewSink creates a new resource sink.
func NewSink(options *Options) *Sink { // nolint: lll
	nodeInfo := &mcp.SinkNode{
		Id:          options.ID,
		Annotations: options.Metadata,
	}

	state := make(map[string]*perCollectionState)
	for _, collection := range options.Collections {
		state[collection] = &perCollectionState{
			versions: make(map[string]string),
		}
	}

	return &Sink{
		state:    state,
		nodeInfo: nodeInfo,
		updater:  options.Updater,
		metadata: options.Metadata,
		reporter: options.Reporter,
		journal:  newRequestJournal(),
	}
}

// ResetResourceVersions resets the cached resource versions for all supported types. This function may be used
// to effectively force full state delivery when a stream is reestablished.
func (sink *Sink) ResetResourceVersions() {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, state := range sink.state {
		state.versions = make(map[string]string)
		state.updated = false
	}
}

func (sink *Sink) createInitialRequests() []*mcp.RequestResources {
	sink.mu.Lock()
	initialRequests := make([]*mcp.RequestResources, 0, len(sink.state))
	for collection, state := range sink.state {
		copyVersions := make(map[string]string, len(state.versions))
		for name, version := range state.versions {
			copyVersions[name] = version
		}

		req := &mcp.RequestResources{
			SinkNode:                sink.nodeInfo,
			Collection:              collection,
			InitialResourceVersions: copyVersions,
		}
		initialRequests = append(initialRequests, req)
	}
	sink.mu.Unlock()

	return initialRequests
}

// processStream implements the MCP message exchange for the resource sink. It accepts the sink
// stream interface and returns when a send or receive error occurs. The caller is responsible for handling gRPC
// client/server specific error handling.
func (sink *Sink) processStream(stream Stream) error {
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
			req = sink.processResources(resources)
		}

		sink.journal.record(req)

		if err := stream.Send(req); err != nil {
			sink.reporter.RecordSendError(err, status.Code(err))
			scope.Errorf("Error sending MCP request: %v", err)
			return err
		}
	}
}

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

// Probe point for test code to determine when the node is finished processing responses.
var handleResponseDoneProbe = func() {}

func (sink *Sink) processResources(resources *mcp.Resources) *mcp.RequestResources {
	if handleResponseDoneProbe != nil {
		defer handleResponseDoneProbe()
	}

	state, ok := sink.state[resources.Collection]
	if !ok {
		errDetails := status.Errorf(codes.Unimplemented, "unsupported collection %v", resources.Collection)
		return sink.sendNACKRequest(resources, errDetails)
	}

	var added []*Object
	if len(resources.Resources) > 0 {
		added = make([]*Object, 0, len(resources.Resources))
	}
	for _, envelope := range resources.Resources {
		var dynamicAny types.DynamicAny
		if err := types.UnmarshalAny(envelope.Body, &dynamicAny); err != nil {
			return sink.sendNACKRequest(resources, err)
		}

		// TODO - use galley metadata to verify collection and type_url match?
		object := &Object{
			TypeURL:  envelope.Body.TypeUrl,
			Metadata: envelope.Metadata,
			Body:     dynamicAny.Message,
		}
		added = append(added, object)
	}

	change := &Change{
		Collection: resources.Collection,
		Objects:    added,
		Removed:    resources.RemovedResources,
	}

	sink.mu.Lock()
	if state.updated {
		change.Incremental = true
	}
	sink.mu.Unlock()

	if err := sink.updater.Apply(change); err != nil {
		errDetails := status.Error(codes.InvalidArgument, err.Error())
		return sink.sendNACKRequest(resources, errDetails)
	}

	// update version tracking if change is successfully applied
	sink.mu.Lock()
	resourceVersions := state.versions
	for _, removed := range resources.RemovedResources {
		delete(resourceVersions, removed)
	}
	for _, envelope := range resources.Resources {
		resourceVersions[envelope.Metadata.Name] = envelope.Metadata.Version
	}
	state.updated = true
	sink.mu.Unlock()

	// ACK
	sink.reporter.RecordRequestAck(resources.Collection, 0)
	req := &mcp.RequestResources{
		SinkNode:      sink.nodeInfo,
		Collection:    resources.Collection,
		ResponseNonce: resources.Nonce,
	}
	return req
}

// SnapshotRequestInfo returns a snapshot of the last known set of request results.
func (sink *Sink) SnapshotRequestInfo() []RecentRequestInfo {
	return sink.journal.snapshot()
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
