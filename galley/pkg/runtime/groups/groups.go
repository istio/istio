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

package groups

import (
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/pkg/mcp/snapshot"
)

const (
	// Default group for MCP requests.
	Default = "default"

	// SyntheticServiceEntry is the group used for the SynetheticServiceEntry collection.
	SyntheticServiceEntry = "syntheticServiceEntry"
)

var _ snapshot.GroupIndexFn = IndexFunction

// IndexFunction is a snapshot.GroupIndexFn used internally by Galley.
// If the request is for the collection metadata.SyntheticServiceEntry,
// the SyntheticServiceEntry snapshot is used. Otherwise the Default
// snapshot is used.
func IndexFunction(collection string, _ *mcp.SinkNode) string {
	switch collection {
	case metadata.IstioNetworkingV1alpha3SyntheticServiceentries.Collection.String():
		return SyntheticServiceEntry
	default:
		return Default
	}
}
