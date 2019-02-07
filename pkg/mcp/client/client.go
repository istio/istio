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

package client

import (
	"context"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/sink"
)

var (
	// try to re-establish the bi-directional grpc stream after this delay.
	reestablishStreamDelay = time.Second

	scope = log.RegisterScope("mcp", "mcp debugging", 0)
)

type perTypeState struct {
	sync.Mutex
	lastVersion string
}

func (s *perTypeState) setVersion(v string) {
	s.Lock()
	defer s.Unlock()
	s.lastVersion = v
}

func (s *perTypeState) version() string {
	s.Lock()
	defer s.Unlock()
	return s.lastVersion
}

// Client is a client implementation of the Mesh Configuration Protocol (MCP). It is responsible
// for the following:
//
// - Maintaining the bidirectional grpc stream with the server. The stream will be reestablished
//   on transient network failures. The provided grpc connection (mcpClient) is assumed to handle
//   (re)dialing the server.
//
// - Handling all aspects of the MCP exchange for the supported message types, e.g. request/response,
//   ACK/NACK, nonce, versioning,
//
// - Decoding the received configuration updates and providing them to the user via a batched set of changes.
type Client struct {
	client   mcp.AggregatedMeshConfigServiceClient
	stream   mcp.AggregatedMeshConfigService_StreamAggregatedResourcesClient
	state    map[string]*perTypeState
	nodeInfo *mcp.SinkNode
	updater  sink.Updater

	journal  *sink.RecentRequestsJournal
	metadata map[string]string
	reporter monitoring.Reporter
}

// New creates a new instance of the MCP client for the specified message types.
func New(mcpClient mcp.AggregatedMeshConfigServiceClient, options *sink.Options) *Client { // nolint: lll
	nodeInfo := &mcp.SinkNode{
		Id:          options.ID,
		Annotations: map[string]string{},
	}
	for k, v := range options.Metadata {
		nodeInfo.Annotations[k] = v
	}

	state := make(map[string]*perTypeState)
	for _, collection := range options.CollectionOptions {
		state[collection.Name] = &perTypeState{}
	}

	return &Client{
		client:   mcpClient,
		state:    state,
		nodeInfo: nodeInfo,
		updater:  options.Updater,
		metadata: options.Metadata,
		reporter: options.Reporter,
		journal:  sink.NewRequestJournal(),
	}
}

// Probe point for test code to determine when the client is finished processing responses.
var handleResponseDoneProbe = func() {}

func (c *Client) sendNACKRequest(response *mcp.MeshConfigResponse, version string, err error) *mcp.MeshConfigRequest {
	errorDetails, _ := status.FromError(err)

	scope.Errorf("MCP: sending NACK for version=%v nonce=%v: error=%q", version, response.Nonce, err)

	c.reporter.RecordRequestNack(response.TypeUrl, 0, errorDetails.Code())

	req := &mcp.MeshConfigRequest{
		SinkNode:      c.nodeInfo,
		TypeUrl:       response.TypeUrl,
		VersionInfo:   version,
		ResponseNonce: response.Nonce,
		ErrorDetail:   errorDetails.Proto(),
	}
	return req
}

func (c *Client) handleResponse(response *mcp.MeshConfigResponse) *mcp.MeshConfigRequest {
	if handleResponseDoneProbe != nil {
		defer handleResponseDoneProbe()
	}

	collection := response.TypeUrl

	state, ok := c.state[collection]
	if !ok {
		errDetails := status.Errorf(codes.Unimplemented, "unsupported collection: %v", collection)
		return c.sendNACKRequest(response, "", errDetails)
	}

	change := &sink.Change{
		Collection:        collection,
		Objects:           make([]*sink.Object, 0, len(response.Resources)),
		SystemVersionInfo: response.VersionInfo,
	}
	for _, resource := range response.Resources {
		var dynamicAny types.DynamicAny
		if err := types.UnmarshalAny(resource.Body, &dynamicAny); err != nil {
			return c.sendNACKRequest(response, state.version(), err)
		}

		object := &sink.Object{
			TypeURL:  resource.Body.TypeUrl,
			Metadata: resource.Metadata,
			Body:     dynamicAny.Message,
		}
		change.Objects = append(change.Objects, object)
	}

	if err := c.updater.Apply(change); err != nil {
		errDetails := status.Error(codes.InvalidArgument, err.Error())
		return c.sendNACKRequest(response, state.version(), errDetails)
	}

	c.reporter.RecordRequestAck(collection, 0)

	req := &mcp.MeshConfigRequest{
		SinkNode:      c.nodeInfo,
		TypeUrl:       collection,
		VersionInfo:   response.VersionInfo,
		ResponseNonce: response.Nonce,
	}
	return req
}

// Run starts the run loop for request and receiving configuration updates from
// the server. This function blocks and should typically be run in a goroutine.
// The client will continue to attempt to re-establish the stream with the server
// indefinitely. The function exits when the provided context is canceled.
func (c *Client) Run(ctx context.Context) {

	// See https://godoc.org/google.golang.org/grpc#ClientConn.NewStream
	// for rules to ensure stream resources are not leaked.

	initRequests := make([]*mcp.MeshConfigRequest, 0, len(c.state))
	for collection := range c.state {
		initRequests = append(initRequests, &mcp.MeshConfigRequest{
			SinkNode: c.nodeInfo,
			TypeUrl:  collection,
		})
	}

	// The first attempt is immediate.
	retryDelay := time.Nanosecond

	for {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
			}

			// slow subsequent reconnection attempts down
			retryDelay = reestablishStreamDelay

			scope.Info("(re)trying to establish new MCP stream")
			var err error
			if c.stream, err = c.client.StreamAggregatedResources(ctx); err == nil {
				c.reporter.RecordStreamCreateSuccess()
				scope.Info("New MCP stream created")
				break
			}

			scope.Errorf("Failed to create a new MCP stream: %v", err)
		}

		var nextInitRequest int

		// The client begins each new stream by sending an empty
		// request for each supported type. The server sends a
		// response when resources are available. After processing a
		// response, the client sends a new request specifying the
		// last version applied and nonce provided by the server.
		for {
			var req *mcp.MeshConfigRequest
			var version string

			if nextInitRequest < len(initRequests) {
				// Send the entire batch of initial requests before
				// trying to receive responses.
				req = initRequests[nextInitRequest]
				nextInitRequest++
			} else {
				response, err := c.stream.Recv()
				if err != nil {
					if err != io.EOF {
						c.reporter.RecordRecvError(err, status.Code(err))
						scope.Errorf("Error receiving MCP response: %v", err)
					}
					break
				}

				version = response.VersionInfo
				req = c.handleResponse(response)
			}

			c.journal.RecordMeshConfigRequest(req)

			if err := c.stream.Send(req); err != nil {
				c.reporter.RecordSendError(err, status.Code(err))
				scope.Errorf("Error sending MCP request: %v", err)

				// (from https://godoc.org/google.golang.org/grpc#ClientConn.NewStream)
				//
				// SendMsg is generally called by generated code. On error, SendMsg aborts
				// the stream. If the error was generated by the client, the status is
				// returned directly; otherwise, io.EOF is returned and the status of
				// the stream may be discovered using RecvMsg.
				if err != io.EOF {
					break
				}
			} else {
				collection := req.TypeUrl
				if req.ErrorDetail == nil && collection != "" {
					if state, ok := c.state[collection]; ok {
						state.setVersion(version)
					}
				}
			}
		}
	}
}

// SnapshotRequestInfo returns a snapshot of the last known set of request results.
func (c *Client) SnapshotRequestInfo() []sink.RecentRequestInfo {
	return c.journal.Snapshot()
}

// Metadata that is originally supplied when creating this client.
func (c *Client) Metadata() map[string]string {
	r := make(map[string]string, len(c.metadata))
	for k, v := range c.metadata {
		r[k] = v
	}

	return r
}

// ID is the node id for this client.
func (c *Client) ID() string {
	return c.nodeInfo.Id
}

// Collections returns the collections that this client requests.
func (c *Client) Collections() []string {
	result := make([]string, 0, len(c.state))

	for k := range c.state {
		result = append(result, k)
	}
	sort.Strings(result)

	return result
}
