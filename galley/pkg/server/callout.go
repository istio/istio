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

	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/authplugin"
	"istio.io/istio/galley/pkg/authplugins"
	"istio.io/istio/pkg/mcp/source"
)

type callout struct {
	address string
	so      *source.Options
	do      []grpc.DialOption
	cancel  context.CancelFunc
}

// Test override types
type dialFn func(string, ...grpc.DialOption) (*grpc.ClientConn, error)
type mcpClient interface {
	Run(context.Context)
}
type newClientFn func(mcp.ResourceSinkClient, *source.Options) mcpClient
type closeFn func(*grpc.ClientConn)

// Test override variables
var (
	connClose       closeFn
	grpcDial        dialFn
	sourceNewClient newClientFn
)

func init() {
	// Setup non-test defaults
	grpcDial = grpc.Dial
	sourceNewClient = func(c mcp.ResourceSinkClient, o *source.Options) mcpClient { return source.NewClient(c, o) }
	connClose = func(c *grpc.ClientConn) { c.Close() }
}

func newCallout(sa *Args, so *source.Options) (*callout, error) {
	auths := getAuth()

	f, ok := auths[sa.CalloutAuth]
	if !ok {
		return nil, fmt.Errorf("auth plugin %v not found", sa.CalloutAuth)
	}

	opts, err := f(nil)
	if err != nil {
		return nil, err
	}

	return &callout{
		address: sa.CalloutAddress,
		so:      so,
		do:      opts,
	}, nil
}

func (c *callout) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	conn, err := grpcDial(c.address, c.do...)
	if err != nil {
		scope.Fatalf("Failed to connect to server: %v", err)
		return
	}
	defer connClose(conn)

	client := mcp.NewResourceSinkClient(conn)

	mcpClient := sourceNewClient(client, c.so)
	scope.Infof("Starting MCP Source Client connection to: %v", c.address)
	mcpClient.Run(ctx)
}

func (c *callout) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}

func getAuth() map[string]authplugin.AuthFn {
	m := make(map[string]authplugin.AuthFn)
	for _, g := range authplugins.Inventory() {
		i := g()
		m[i.Name] = i.GetAuth
	}
	return m
}
