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
	"testing"

	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance is a new mcpserver instance. MCP Server is a generic MCP server implementation for testing purposes.
type Instance interface {
	Address() string
	GetCollectionStateOrFail(t *testing.T, collection string) []*sink.Object
}

// SinkConfig is configuration for the mcpserver for sink mode.
type SinkConfig struct {
	Collections []string
}

// NewSink returns a new instance of MCP Server in Sink mode.
func NewSink(ctx resource.Context, cfg SinkConfig) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i, err = newSinkNative(ctx, cfg)
	})
	return
}

// NewSinkOrFail returns a new instance of MCP server in Sink mode or fails.
func NewSinkOrFail(t *testing.T, c resource.Context, cfg SinkConfig) Instance {
	i, err := NewSink(c, cfg)
	if err != nil {
		t.Fatalf("mcpserver.NewOrFail: %v", err)
	}
	return i
}
