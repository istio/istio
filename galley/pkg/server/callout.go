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

package server

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/authplugins"
	"istio.io/istio/pkg/mcp/source"
)

type callout struct {
	address  string
	so       *source.Options
	do       []grpc.DialOption
	cancel   context.CancelFunc
	pt       calloutPatchTable
	metadata []string
}

// Test override types
type dialFn func(string, ...grpc.DialOption) (*grpc.ClientConn, error)
type mcpClient interface {
	Run(context.Context)
}
type newClientFn func(mcp.ResourceSinkClient, *source.Options) mcpClient
type closeFn func(*grpc.ClientConn)

type calloutPatchTable struct {
	connClose       closeFn
	grpcDial        dialFn
	sourceNewClient newClientFn
}

func defaultCalloutPT() calloutPatchTable {
	// non-test defaults
	return calloutPatchTable{
		grpcDial:        grpc.Dial,
		sourceNewClient: func(c mcp.ResourceSinkClient, o *source.Options) mcpClient { return source.NewClient(c, o) },
		connClose:       func(c *grpc.ClientConn) { c.Close() },
	}
}

// newCallout initializes a callout struct. Address should be the
// "host:port" of the server to dial. Auth should be the name of an
// existing auth plugin under
// istio.io/istio/galley/pkg/authplugins. Metadata elements should be
// in the format of "key=value".
func newCallout(address, auth string, metadata []string,
	so *source.Options) (*callout, error) {
	return newCalloutPT(address, auth, metadata, so, defaultCalloutPT())
}

func newCalloutPT(address, auth string, metadata []string, so *source.Options,
	pt calloutPatchTable) (*callout, error) {
	auths := authplugins.AuthMap()

	f, ok := auths[auth]
	if !ok {
		return nil, fmt.Errorf("auth plugin %v not found", auth)
	}

	opts, err := f(nil)
	if err != nil {
		return nil, err
	}

	m := make([]string, 0)

	for _, v := range metadata {
		kv := strings.Split(v, "=")
		if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
			return nil, fmt.Errorf(
				"sinkMeta not in key=value format: %v", v)
		}
		m = append(m, kv[0], kv[1])
	}

	return &callout{
		address:  address,
		so:       so,
		do:       opts,
		pt:       pt,
		metadata: m,
	}, nil
}

func (c *callout) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	conn, err := c.pt.grpcDial(c.address, c.do...)
	if err != nil {
		scope.Errorf("Failed to connect to server: %v", err)
		return
	}
	defer c.pt.connClose(conn)

	client := mcp.NewResourceSinkClient(conn)

	mcpClient := c.pt.sourceNewClient(client, c.so)
	scope.Infof("Starting MCP Source Client connection to: %v", c.address)
	ctx = metadata.AppendToOutgoingContext(ctx, c.metadata...)
	mcpClient.Run(ctx)
}

func (c *callout) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}
