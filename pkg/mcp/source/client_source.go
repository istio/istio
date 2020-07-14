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

package source

import (
	"context"
	"io"
	"time"

	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/status"
)

var (
	// try to re-establish the bi-directional grpc stream after this delay.
	reestablishStreamDelay = time.Second
	triggerCollection      = "$triggerCollection"
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

// NewClient returns a new instance of Client.
func NewClient(client mcp.ResourceSinkClient, options *Options) *Client {
	return &Client{
		source:   New(options),
		client:   client,
		reporter: options.Reporter,
	}
}

var reconnectTestProbe = func() {}

// Some scenarios requires the client to send the first message in a
// bi-directional stream to establish the stream on the server. Send a
// trigger response which we expect the server to NACK.
func (c *Client) sendTriggerResponse(stream Stream) error {
	trigger := &mcp.Resources{
		Collection: triggerCollection,
	}

	if err := stream.Send(trigger); err != nil {
		return status.Errorf(status.Code(err), "could not send trigger request %v", err)
	}

	return nil
}

// isTriggerResponse checks whether the given RequestResources object is an expected NACK response to a previous
// trigger message.
func isTriggerResponse(msg *mcp.RequestResources) bool {
	return msg.Collection == triggerCollection && msg.ErrorDetail != nil && codes.Code(msg.ErrorDetail.Code) == codes.Unimplemented
}

// Run implements mcpClient
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

			if reconnectTestProbe != nil {
				reconnectTestProbe()
			}

			scope.Info("(re)trying to establish new MCP source stream")
			stream, err := c.client.EstablishResourceStream(ctx)

			if err != nil {
				scope.Errorf("Failed to create a new MCP source stream: %v", err)
				continue
			}
			c.reporter.RecordStreamCreateSuccess()
			scope.Info("New MCP source stream created")

			if err := c.sendTriggerResponse(stream); err != nil {
				scope.Errorf("Failed to send fake response: %v", err)
				continue
			}

			c.stream = stream
			break
		}

		err := c.source.ProcessStream(c.stream)
		if err != nil && err != io.EOF {
			c.reporter.RecordRecvError(err, status.Code(err))
			scope.Errorf("Error receiving MCP response: %v", err)
		}
	}
}
