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

package groups

import (
	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/istio/pkg/mcp/snapshot"
)

// Default group for MCP requests.
const Default = "default"

var _ snapshot.GroupIndexFn = IndexFunction

// IndexFunction is a snapshot.GroupIndexFn used internally by Galley.
func IndexFunction(collection string, _ *mcp.SinkNode) string {
	return Default
}
