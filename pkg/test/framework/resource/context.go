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
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/util/yml"
)

type ConfigOptions struct {
	NoCleanup bool
	Wait      bool
}

type ConfigOption func(o *ConfigOptions)

// NoCleanup does not delete the applied Config once it goes out of scope.
var NoCleanup ConfigOption = func(o *ConfigOptions) {
	o.NoCleanup = true
}

// Wait for the Config to be applied everywhere.
var Wait ConfigOption = func(o *ConfigOptions) {
	o.Wait = true
}

// ConfigFactory is a factory for creating new Config resources.
type ConfigFactory interface {
	// YAML adds YAML content to this config for the given namespace.
	YAML(ns string, yamlText ...string) Config

	// File reads the given YAML files and adds their content to this config
	// for the given namespace.
	File(ns string, paths ...string) Config

	// Eval the same as YAML, but it evaluates the template parameters.
	Eval(ns string, args interface{}, yamlText ...string) Config

	// EvalFile the same as File, but it evaluates the template parameters.
	EvalFile(ns string, args interface{}, paths ...string) Config
}

// Config builds a configuration that can be applied or deleted as a single
type Config interface {
	// ConfigFactory for appending to this Config
	ConfigFactory

	// Apply this config to all clusters within the ConfigManager
	Apply(opts ...ConfigOption) error
	ApplyOrFail(t test.Failer, opts ...ConfigOption)

	// Delete this config from all clusters within the ConfigManager
	Delete() error
	DeleteOrFail(t test.Failer)
}

// ConfigManager is an interface for applying/deleting yaml resources.
type ConfigManager interface {
	ConfigFactory

	// New empty Config.
	New() Config

	// WithFilePrefix sets the prefix used for intermediate files.
	WithFilePrefix(prefix string) ConfigManager
}

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

	// ConditionalCleanup runs the given function when the test context completes.
	// If -istio.test.nocleanup is set, this function will not be executed. To unconditionally cleanup, use Cleanup.
	// This function may not (safely) access the test context.
	ConditionalCleanup(fn func())

	// Cleanup runs the given function when the test context completes.
	// This function will always run, regardless of -istio.test.nocleanup. To run only when cleanup is enabled,
	// use WhenDone.
	// This function may not (safely) access the test context.
	Cleanup(fn func())

	// CreateDirectory creates a new subdirectory within this context.
	CreateDirectory(name string) (string, error)

	// CreateTmpDirectory creates a new temporary directory within this context.
	CreateTmpDirectory(prefix string) (string, error)

	// ConfigKube returns a ConfigManager that writes config to the provided clusers. If
	// no clusters are provided, writes to all clusters in the mesh.
	ConfigKube(clusters ...cluster.Cluster) ConfigManager

	// ConfigIstio returns a ConfigManager that writes config to all Istio config clusters.
	ConfigIstio() ConfigManager

	// RecordTraceEvent records an event. This is later saved to trace.yaml for analysis
	RecordTraceEvent(key string, value interface{})
}
