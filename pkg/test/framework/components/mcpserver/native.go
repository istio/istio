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

package mcpserver

import (
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

type native struct {
	id resource.ID
	l  net.Listener
	s  *grpc.Server
	u  *sink.InMemoryUpdater
}

var _ Instance = &native{}
var _ resource.Resource = &native{}
var _ io.Closer = &native{}

func newSinkNative(ctx resource.Context, cfg SinkConfig) (*native, error) {
	n := &native{}
	n.id = ctx.TrackResource(n)
	u := sink.NewInMemoryUpdater()

	so := sink.Options{
		ID:                "mcpserver.sink",
		CollectionOptions: sink.CollectionOptionsFromSlice(cfg.Collections, false),
		Updater:           u,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}

	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}

	s := grpc.NewServer()
	srv := sink.NewServer(&so, &sink.ServerOptions{
		AuthChecker: &server.AllowAllChecker{},
		RateLimiter: rate.NewRateLimiter(time.Millisecond, 1000).Create(),
	})

	mcp.RegisterResourceSinkServer(s, srv)

	go func() {
		if err := s.Serve(l); err != nil {
			scopes.Framework.Errorf("mcpserver.Serve: %v", err)
		}
	}()

	n.l = l
	n.s = s
	n.u = u

	return n, nil
}

// ID implements resource.Resource
func (n *native) ID() resource.ID {
	return n.id
}

// Address implements Instance
func (n *native) Address() string {
	return n.l.Addr().String()
}

// GetCollectionStateOrFail implements Instance
func (n *native) GetCollectionStateOrFail(t *testing.T, collection string) []*sink.Object {
	return n.u.Get(collection)
}

// Close implements io.Closer
func (n *native) Close() error {
	n.s.Stop()
	return nil
}
