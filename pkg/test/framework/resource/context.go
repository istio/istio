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

package resource

import (
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/util/yml"
)

// Context is the core context interface that is used by resources.
type Context interface {
	yml.FileWriter

	// TrackResource tracks a resource in this context. If the context is closed, then the resource will be
	// cleaned up.
	TrackResource(r Resource) ID

	// GetResource accepts either a *T or *[]*T where T implements Resource.
	// For a non-slice pointer, the value will be assigned to the first matching resource.
	// For a slice pointer, the matching resources from this scope and its parent(s) will be appended.
	// If ref is not a pointer, an error will be returned.
	// If there is no match for a non-slice pointer, an error will be returned.
	GetResource(ref interface{}) error

	// The Environment in which the tests run
	Environment() Environment

	// Clusters in this Environment. There will always be at least one.
	Clusters() cluster.Clusters

	// AllClusters in this Environment, including external control planes.
	AllClusters() cluster.Clusters

	// Settings returns common settings
	Settings() *Settings

	// Cleanup will trigger the provided cleanup function after the test context
	// completes. This is identical to CleanupStrategy(Always).
	// This function may not (safely) access the test context.
	Cleanup(fn func())

	// CleanupConditionally will trigger a cleanup operation the test context
	// completes, unless -istio.test.nocleanup is set. This is identical to
	// CleanupStrategy(Conditionally).
	// This function may not (safely) access the test context.
	CleanupConditionally(fn func())

	// CleanupStrategy runs the given cleanup function after the test context completes,
	// depending on the provided strategy.
	// This function may not (safely) access the test context.
	CleanupStrategy(strategy cleanup.Strategy, fn func())

	// CreateDirectory creates a new subdirectory within this context.
	CreateDirectory(name string) (string, error)

	// CreateTmpDirectory creates a new temporary directory within this context.
	CreateTmpDirectory(prefix string) (string, error)

	// ConfigKube returns a Context that writes config to the provided clusters. If
	// no clusters are provided, writes to all clusters in the mesh.
	ConfigKube(clusters ...cluster.Cluster) config.Factory

	// ConfigIstio returns a Context that writes config to all Istio config clusters.
	ConfigIstio() config.Factory

	// RecordTraceEvent records an event. This is later saved to trace.yaml for analysis
	RecordTraceEvent(key string, value interface{})
}
