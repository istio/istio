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
	"context"
	"io"
	"time"

	"github.com/gogo/status"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/monitoring"
)

var (
	// try to re-establish the bi-directional grpc stream after this delay.
	reestablishStreamDelay = time.Second
)

// Client implements the client for the MCP source service. The client is the
// sink and receives configuration from the server.
type Client struct {
	// Client receives configuration using the ResourceSource RPC service
	stream mcp.ResourceSource_EstablishResourceStreamClient

	client mcp.ResourceSourceClient
	*Sink
	reporter monitoring.Reporter
}

func NewClient(client mcp.ResourceSourceClient, options *Options) *Client {
	return &Client{
		Sink:     New(options),
		reporter: options.Reporter,
		client:   client,
	}
}

var reconnectTestProbe = func() {}

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

			scope.Info("(re)trying to establish new MCP sink stream")
			stream, err := c.client.EstablishResourceStream(ctx)

			if reconnectTestProbe != nil {
				reconnectTestProbe()
			}

			if err == nil {
				c.reporter.RecordStreamCreateSuccess()
				scope.Info("New MCP sink stream created")
				c.stream = stream
				break
			}

			scope.Errorf("Failed to create a new MCP sink stream: %v", err)
		}

		err := c.processStream(c.stream)
		if err != nil && err != io.EOF {
			c.reporter.RecordRecvError(err, status.Code(err))
			scope.Errorf("Error receiving MCP response: %v", err)
		}
	}
}
