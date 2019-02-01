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

package internal

import (
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

// UpdateResourceVersionTracking updates a map of resource versions indexed
// by name based on the MCP resources response message.
func UpdateResourceVersionTracking(versions map[string]string, resources *mcp.Resources) {
	if resources.Incremental {
		for _, e := range resources.Resources {
			name, version := e.Metadata.Name, e.Metadata.Version
			if prev, ok := versions[e.Metadata.Name]; ok {
				scope.Debugf("MCP: ACK UPDATE collection=%v name=%q version=%q (prev=%v_)",
					resources.Collection, name, version, prev)
			} else {
				scope.Debugf("MCP: ACK ADD collection=%v name=%q version=%q)",
					resources.Collection, name, version)
			}
			versions[name] = version
		}
		for _, name := range resources.RemovedResources {
			scope.Debugf("MCP: ACK REMOVE name=%q)", name)
			delete(versions, name)
		}
	} else {
		for name := range versions {
			delete(versions, name)
		}
		for _, e := range resources.Resources {
			name, version := e.Metadata.Name, e.Metadata.Version
			versions[name] = version
		}
	}
}
