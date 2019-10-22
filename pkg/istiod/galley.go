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

package istiod

import (
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/pkg/log"
)

// GalleyServer component is the main config processing component that will listen to a config source and publish
// resources through an MCP server.

// This is a simplified startup for galley, specific for hyperistio/combined:
// - callout removed - standalone galley supports it, and should be used
// - acl removed - envoy and Istio RBAC should handle it
// - listener removed - common grpc server for all components, using Pilot's listener

var scope = log.RegisterScope("server", "", 0)

const versionMetadataKey = "config.source.version"

// NewGalleyServer is the equivalent of the Galley CLI. No attempt  to optimize or reuse -
// for Pilot we plan to use a 'direct path', bypassing the gRPC layer. This provides max compat
// and less risks with existing galley.
func NewGalleyServer(a *settings.Args) *server.Server {
	s := server.New(a)

	return s
}

// Start implements process.Component
func (s *Server) StartGalley() (err error) {
	if err := s.Galley.Start(); err != nil {
		log.Fatalf("Error creating server: %v", err)
	}
	return nil
}
