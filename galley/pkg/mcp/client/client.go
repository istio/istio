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
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
)

const (
	// try to re-establish the bi-directional grpc stream after this delay.
	reestablishStreamDelay = time.Second

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
			&types.Value_StringValue{v},
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
	}
}

// Probe point for test code to determine when the client is finished processing responses.
var handleResponseDoneProbe = func() {}

func (c *Client) sendNACKRequest(response *mcp.MeshConfigResponse, version string, err error) error {
	log.Errorf("MCP: sending NACK for version=%v nonce=%v: error=%q", version, response.Nonce, err)

	errorDetails, _ := status.FromError(err)
	req := &mcp.MeshConfigRequest{
		Client:        c.clientInfo,
		TypeUrl:       response.TypeUrl,
		VersionInfo:   version,
		ResponseNonce: response.Nonce,
		ErrorDetail:   errorDetails.Proto(),
	}
	return c.stream.Send(req)
}

func (c *Client) handleResponse(response *mcp.MeshConfigResponse) error {
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
		var message proto.Message

		// "istio.io/api" protobufs are compiled with a mix of
		// golang/protobuf and gogo/protobuf (see
		// https://github.com/istio/api/issues/543). gogo/protobuf
		// packages can only decode gogo-compiled
		// protos. golang/protobuf packages can only decode
		// golang-compiled protos. Use the existence of the protobuf's
		// via the message name to differentiate which is which.
		if proto.MessageType(responseMessageName) != nil {
			// gogo proto
			var dynamicAny types.DynamicAny
			if err := types.UnmarshalAny(envelope.Resource, &dynamicAny); err != nil {
				return c.sendNACKRequest(response, state.version(), err)
			}
			message = dynamicAny.Message
		} else {
			// golang proto
			var dynamicAny ptypes.DynamicAny
			if err := types.UnmarshalAny(envelope.Resource, &dynamicAny); err != nil {
				return c.sendNACKRequest(response, state.version(), err)
			}
			message = dynamicAny.Message
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
			Resource:    message,
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
	if err := c.stream.Send(req); err != nil {
		return err
	}
	state.setVersion(response.VersionInfo)
	return nil
}

// openStream should only be called from `Run()` methods for/select loop.
func (c *Client) openStream(ctx context.Context) error {
	retry := time.After(time.Nanosecond)

tryAgain:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-retry:
			log.Info("Trying to establish new MCP stream")
			stream, err := c.client.StreamAggregatedResources(ctx)
			if err != nil {
				log.Errorf("Failed to create a new MCP stream: %v", err)
				retry = time.After(reestablishStreamDelay)
				continue tryAgain
			}
			log.Info("New MCP stream created")
			c.stream = stream

			for typeURL, state := range c.state {
				req := &mcp.MeshConfigRequest{
					Client:  c.clientInfo,
					TypeUrl: typeURL,
				}
				if err := c.stream.Send(req); err != nil {
					log.Errorf("Failed to send initial MCP request for %q: %v", typeURL, err)

					// Close the send-side of the stream on send errors and
					// drain receive until we receive EOF.
					//
					// See RecvMsg and SendMsg grpc comments in Run() below for rationale.
					if err := c.stream.CloseSend(); err != nil {
						log.Errorf("Error closing MCP stream after initial send failure: %v", err)
					}
					for {
						if _, err := c.stream.Recv(); err != nil {
							if err == io.EOF {
								break
							}
							log.Errorf("Recv() error while waiting for MCP stream to close: %v", err)
						}
					}
					retry = time.After(reestablishStreamDelay)
					continue tryAgain
				}
				state.setVersion("")
			}
			return nil
		}
	}
}

// Run starts the run loop for request and receiving configuration updates from
// the server. This function blocks and should typically be run in a goroutine.
// The client will continue to attempt to re-establish the stream with the server
// indefinitely. The function exits when the provided context is canceled.
func (c *Client) Run(ctx context.Context) {
	responseC := make(chan *mcp.MeshConfigResponse)
	var responseError error
	receive := func() {
		for {
			// On non-nil error the stream is closed or aborted. In either case,
			// close the receive channel to signal to the for/select loop to re-establish
			// the stream.
			//
			// (from https://godoc.org/google.golang.org/grpc#Stream)
			//
			// RecvMsg blocks until it receives a message or the stream is done. On
			// client side, it returns io.EOF when the stream is done. On any other
			// error, it aborts the stream and returns an RPC status.
			response, err := c.stream.Recv()
			if err != nil {
				responseError = err
				close(responseC)
				return
			}
			responseC <- response
		}
	}

	// establish the bidirectional grpc stream and start a dedicated
	// goroutine to convert the synchronous stream.Recv() to a channel
	// of responses.
	if err := c.openStream(ctx); err != nil {
		return
	}
	go receive()

	for {
		select {
		case response, more := <-responseC:
			if !more {
				log.Errorf("Stream receive error: %v", responseError)

				// re-establish the stream and start a new receive goroutine
				// on any kind of error until the context is canceled.
				if err := c.openStream(ctx); err != nil {
					return
				}
				// reopen the stream and start receiving again
				responseC = make(chan *mcp.MeshConfigResponse)
				go receive()
				continue
			}

			if err := c.handleResponse(response); err != nil {
				log.Errorf("Error sending MCP request: %v", err)
				if err := c.stream.CloseSend(); err != nil {
					log.Errorf("Error closing MCP stream after send failure: %v", err)
				}

				// Don't immediately reconnect - wait for stream.Recv() to indicate the stream is closed.
				//
				// (from https://godoc.org/google.golang.org/grpc#Stream)
				//
				// Stream.SendMsg() may return a non-nil error when something wrong happens sending
				// the request. The returned error indicates the status of this sending, not the final
				// status of the RPC. Always call Stream.RecvMsg() to drain the stream and get the final
				// status, otherwise there could be leaked resources.
			}
		case <-ctx.Done():
			return
		}
	}
}
