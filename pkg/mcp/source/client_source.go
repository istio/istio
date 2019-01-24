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
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/gogo/status"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/monitoring"
)

var (
	// try to re-establish the bi-directional grpc stream after this delay.
	reestablishStreamDelay = time.Second
)

// Client implements the client for the MCP sink service. The client is the
// source of configuration and sends configuration to the server.
type Client struct {
	// Client pushes configuration to a remove sink using the ResourceSink RPC service
	stream mcp.ResourceSink_EstablishResourceStreamClient

	client   mcp.ResourceSinkClient
	reporter monitoring.Reporter
	source   *Source
}

func NewClient(client mcp.ResourceSinkClient, options *Options) *Client {
	return &Client{
		source:   New(options),
		client:   client,
		reporter: options.Reporter,
	}
}

var reconnectTestProbe = func() {}

func (c *Client) sendDummyResponse(stream Stream) error {
	dummy := &mcp.Resources{
		Collection: "", // unimplemented collection
	}

	if err := stream.Send(dummy); err != nil {
		return fmt.Errorf("could not send dummy request %v", err)
	}

	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("could not receive expected nack response: %v", err)
	}

	if msg.ErrorDetail == nil {
		return fmt.Errorf("server should have nacked, did not get an error")
	}
	errCode := codes.Code(msg.ErrorDetail.Code)
	if errCode != codes.Unimplemented {
		return fmt.Errorf("server should have nacked with code=%v: got %v",
			codes.Unimplemented, errCode)
	}

	return nil
}

func (c *Client) Run(ctx context.Context) {
	// The first attempt is immediate.
	retryDelay := time.Nanosecond

	for {
		// connect w/retry
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
			}

			// slow subsequent reconnection attempts down
			retryDelay = reestablishStreamDelay

			scope.Info("(re)trying to establish new MCP source stream")
			stream, err := c.client.EstablishResourceStream(ctx)

			if reconnectTestProbe != nil {
				reconnectTestProbe()
			}

			if err != nil {
				scope.Errorf("Failed to create a new MCP source stream: %v", err)
				continue
			}
			c.reporter.RecordStreamCreateSuccess()
			scope.Info("New MCP source stream created")

			// Some scenarios requires the client to send the first message in a bi-directional stream to establish
			// the stream on the server. Send a dummy response which we expect the server to NACK.
			if err := c.sendDummyResponse(stream); err != nil {
				scope.Errorf("Failed to send fake response: %v", err)
				continue
			}

			c.stream = stream
			break
		}

		err := c.source.processStream(c.stream)
		if err != nil && err != io.EOF {
			c.reporter.RecordRecvError(err, status.Code(err))
			scope.Errorf("Error receiving MCP response: %v", err)
		}
	}
}
