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

package components

import (
	"istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/mcp/monitoring"
)

// The patch table for external dependencies for code in components.
var (
	newInterfaces     = kube.NewInterfacesFromConfigFile
	mcpMetricReporter = func(prefix string) monitoring.Reporter { return monitoring.NewStatsContext(prefix) }

	meshcfgNewFS        = func(path string) (event.Source, error) { return mesh.NewMeshConfigFS(path) }
	processorInitialize = processor.Initialize
)

func resetPatchTable() {
	newInterfaces = kube.NewInterfacesFromConfigFile
	mcpMetricReporter = func(prefix string) monitoring.Reporter { return monitoring.NewStatsContext(prefix) }

	meshcfgNewFS = func(path string) (event.Source, error) { return mesh.NewMeshConfigFS(path) }
	processorInitialize = processor.Initialize
}
