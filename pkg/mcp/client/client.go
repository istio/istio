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
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
)

// try to re-establish the bi-directional grpc stream after this delay.
var reestablishStreamDelay = time.Second

const (
	typeURLBase = "type.googleapis.com/"
)

// Object contains a decoded versioned object with metadata received from the server.
type Object struct {
	MessageName string
	Metadata    *mcp.Metadata
	Resource    proto.Message
	Version     string
}

// Change is a collection of configuration objects of the same protobuf message type.
type Change struct {
	MessageName string
	Objects     []*Object

	// TODO(ayj) add incremental add/remove enum when the mcp protocol supports it.
}

// Updater provides configuration changes in batches of the same protobuf message type.
type Updater interface {
	// Update is invoked when the client receives new configuration updates
	// from the server. The caller should return an error if any of the provided
	// configuration resources are invalid or cannot be applied. The client will
	// propagate errors back to the server accordingly.
	Update(*Change) error
}

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
	client     mcp.AggregatedMeshConfigServiceClient
	stream     mcp.AggregatedMeshConfigService_StreamAggregatedResourcesClient
	state      map[string]*perTypeState
	clientInfo *mcp.Client
	updater    Updater

	journal  recentRequestsJournal
	metadata map[string]string
}

// RecentRequestInfo is metadata about a request that the client has sent.
type RecentRequestInfo struct {
	Time    time.Time
	Request *mcp.MeshConfigRequest
}

// Acked indicates whether the message was an ack or not.
func (r RecentRequestInfo) Acked() bool {
	return r.Request.ErrorDetail != nil
}

// recentRequestsJournal captures debug metadata about the latest requests that was sent by this client.
type recentRequestsJournal struct {
	itemsMutex sync.Mutex
	items      []RecentRequestInfo
}

func (r *recentRequestsJournal) record(req *mcp.MeshConfigRequest) { // nolint:interfacer
	r.itemsMutex.Lock()
	defer r.itemsMutex.Unlock()

	item := RecentRequestInfo{
		Time:    time.Now(),
		Request: proto.Clone(req).(*mcp.MeshConfigRequest),
	}

	r.items = append(r.items, item)
	for len(r.items) > 20 {
		r.items = r.items[1:]
	}
}

func (r *recentRequestsJournal) snapshot() []RecentRequestInfo {
	r.itemsMutex.Lock()
	defer r.itemsMutex.Unlock()

	result := make([]RecentRequestInfo, len(r.items))
	copy(result, r.items)

	return result
}

// New creates a new instance of the MCP client for the specified message types.
func New(mcpClient mcp.AggregatedMeshConfigServiceClient, supportedMessageNames []string, updater Updater, id string, metadata map[string]string) *Client { // nolint: lll
	clientInfo := &mcp.Client{
		Id: id,
		Metadata: &types.Struct{
			Fields: map[string]*types.Value{},
		},
	}
	for k, v := range metadata {
		clientInfo.Metadata.Fields[k] = &types.Value{
			Kind: &types.Value_StringValue{StringValue: v},
		}
	}

	state := make(map[string]*perTypeState)
	for _, messageName := range supportedMessageNames {
		typeURL := typeURLBase + messageName
		state[typeURL] = &perTypeState{}
	}

	return &Client{
		client:     mcpClient,
		state:      state,
		clientInfo: clientInfo,
		updater:    updater,
		metadata:   metadata,
	}
}

// Probe point for test code to determine when the client is finished processing responses.
var handleResponseDoneProbe = func() {}

func (c *Client) sendNACKRequest(response *mcp.MeshConfigResponse, version string, err error) *mcp.MeshConfigRequest {
	log.Errorf("MCP: sending NACK for version=%v nonce=%v: error=%q", version, response.Nonce, err)

	errorDetails, _ := status.FromError(err)
	req := &mcp.MeshConfigRequest{
		Client:        c.clientInfo,
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

	state, ok := c.state[response.TypeUrl]
	if !ok {
		errDetails := status.Error(codes.Unimplemented, "unsupported type_url: %v")
		return c.sendNACKRequest(response, "", errDetails)
	}

	responseMessageName := response.TypeUrl
	// extract the message name from the fully qualified type_url.
	if slash := strings.LastIndex(response.TypeUrl, "/"); slash >= 0 {
		responseMessageName = response.TypeUrl[slash+1:]
	}

	change := &Change{
		MessageName: responseMessageName,
		Objects:     make([]*Object, 0, len(response.Envelopes)),
	}
	for _, envelope := range response.Envelopes {
		var dynamicAny types.DynamicAny
		if err := types.UnmarshalAny(envelope.Resource, &dynamicAny); err != nil {
			return c.sendNACKRequest(response, state.version(), err)
		}

		if response.TypeUrl != envelope.Resource.TypeUrl {
			errDetails := status.Errorf(codes.InvalidArgument,
				"response type_url(%v) does not match resource type_url(%v)",
				response.TypeUrl, envelope.Resource.TypeUrl)
			return c.sendNACKRequest(response, state.version(), errDetails)
		}

		object := &Object{
			MessageName: responseMessageName,
			Metadata:    envelope.Metadata,
			Resource:    dynamicAny.Message,
			Version:     response.VersionInfo,
		}
		change.Objects = append(change.Objects, object)
	}

	if err := c.updater.Update(change); err != nil {
		errDetails := status.Error(codes.InvalidArgument, err.Error())
		return c.sendNACKRequest(response, state.version(), errDetails)
	}

	// ACK
	req := &mcp.MeshConfigRequest{
		Client:        c.clientInfo,
		TypeUrl:       response.TypeUrl,
		VersionInfo:   response.VersionInfo,
		ResponseNonce: response.Nonce,
	}
	return req
}

// Run starts the run loop for request and receiving configuration updates from
// the server. This function blocks and should typically be run in a goroutine.
// The client will continue to attempt to re-establish the stream with the server
// indefinitely. The function exits when the provided context is canceled.
//
// See https://godoc.org/google.golang.org/grpc#ClientConn.NewStream
// for rules to ensure stream resources are not leaked.
func (c *Client) Run(ctx context.Context) {
	initRequests := make([]*mcp.MeshConfigRequest, 0, len(c.state))
	for typeURL, _ := range c.state {
		initRequests = append(initRequests, &mcp.MeshConfigRequest{
			Client:  c.clientInfo,
			TypeUrl: typeURL,
		})
	}

	for {
		retry := time.After(time.Nanosecond)
		for {
			select {
			case <-ctx.Done():
				return
			case <-retry:
			}

			log.Info("(re)trying to establish new MCP stream")
			var err error
			if c.stream, err = c.client.StreamAggregatedResources(ctx); err == nil {
				log.Info("New MCP stream created")
				break
			}

			log.Errorf("Failed to create a new MCP stream: %v", err)
			retry = time.After(reestablishStreamDelay)
		}

		var nextInitRequest int

		// The version and nonce information is reset for each new
		// stream. Begin the request/response loop by re-sending empty
		// requests to starts watches for each supported
		// type_url. Subsequent requests for a given stream are
		// generated in response to a received response from the
		// server.
		for {
			var req *mcp.MeshConfigRequest
			var version string

			// Send the entire batch of initial requests before trying
			// to receive responses.
			if nextInitRequest < len(initRequests) {
				req = initRequests[nextInitRequest]
				nextInitRequest++
			} else {
				response, err := c.stream.Recv()
				if err != nil {
					if err != io.EOF {
						log.Errorf("Error receiving MCP response: %v", err)
					}
					break
				}

				version = response.VersionInfo
				req = c.handleResponse(response)
			}

			c.journal.record(req)

			if err := c.stream.Send(req); err != nil {
				log.Errorf("Error sending MCP request: %v", err)

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
				if req.ErrorDetail == nil && req.TypeUrl != "" {
					if state, ok := c.state[req.TypeUrl]; ok {
						state.setVersion(version)
					}
				}
			}
		}
	}
}

// SnapshotRequestInfo returns a snapshot of the last known set of request results.
func (c *Client) SnapshotRequestInfo() []RecentRequestInfo {
	return c.journal.snapshot()
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
	return c.clientInfo.Id
}

// SupportedTypeURLs returns the TypeURLs that this client requests.
func (c *Client) SupportedTypeURLs() []string {
	result := make([]string, 0, len(c.state))

	for k := range c.state {
		result = append(result, k)
	}
	sort.Strings(result)

	return result
}
