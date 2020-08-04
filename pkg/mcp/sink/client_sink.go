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
	"context"
	"io"
	"time"

	"github.com/cenkalti/backoff"

	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/istio/pkg/mcp/status"
)

var (
	// try to re-establish the bi-directional grpc stream after this delay.
	reestablishStreamDelay = time.Second
)

// Client implements the client for the MCP source service. The client is the
// sink and receives configuration from the server.
type Client struct {
	client mcp.ResourceSourceClient
	*Sink
	// reconnectTestProbe is the function called on reconnect
	// This is used only for testing
	reconnectTestProbe func()
}

// NewClient returns a new instance of Client.
func NewClient(client mcp.ResourceSourceClient, options *Options) *Client {
	return &Client{
		Sink:   New(options),
		client: client,
	}
}

func (c *Client) Run(ctx context.Context) {
	var err error
	var stream Stream

	for {
		backoffPolicy := backoff.NewExponentialBackOff()
		backoffPolicy.InitialInterval = time.Nanosecond
		backoffPolicy.MaxElapsedTime = 0
		t := backoff.NewTicker(backoffPolicy)
		// connect w/retry
		for {
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}

			scope.Info("(re)trying to establish new MCP sink stream")
			stream, err = c.client.EstablishResourceStream(ctx)

			if c.reconnectTestProbe != nil {
				c.reconnectTestProbe()
			}

			if err == nil {
				c.reporter.RecordStreamCreateSuccess()
				scope.Info("New MCP sink stream created")
				break
			}

			scope.Errorf("Failed to create a new MCP sink stream: %v", err)
		}

		// stop the ticker
		t.Stop()

		err := c.ProcessStream(stream)
		if err != nil && err != io.EOF {
			c.reporter.RecordRecvError(err, status.Code(err))
			scope.Errorf("Error receiving MCP response: %v", err)
		}
	}

}
