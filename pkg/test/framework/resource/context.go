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
	"istio.io/istio/pkg/test/util/yml"
)

// ConfigManager is an interface for applying/deleting yaml resources.
type ConfigManager interface {
	// ApplyYAML applies the given config yaml text via Galley.
	ApplyYAML(ns string, yamlText ...string) error

	// ApplyYAMLOrFail applies the given config yaml text via Galley.
	ApplyYAMLOrFail(t test.Failer, ns string, yamlText ...string)

	// DeleteYAML deletes the given config yaml text via Galley.
	DeleteYAML(ns string, yamlText ...string) error

	// DeleteYAMLOrFail deletes the given config yaml text via Galley.
	DeleteYAMLOrFail(t test.Failer, ns string, yamlText ...string)

	// ApplyYAMLDir recursively applies all the config files in the specified directory
	ApplyYAMLDir(ns string, configDir string) error

	// DeleteYAMLDir recursively deletes all the config files in the specified directory
	DeleteYAMLDir(ns string, configDir string) error

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
	Clusters() Clusters

	// Settings returns common settings
	Settings() *Settings

	// CreateDirectory creates a new subdirectory within this context.
	CreateDirectory(name string) (string, error)

	// CreateTmpDirectory creates a new temporary directory within this context.
	CreateTmpDirectory(prefix string) (string, error)

	// Config returns a ConfigManager that writes config to the provide clusers. If
	// no clusters are provided, writes to all clusters.
	Config(clusters ...Cluster) ConfigManager
}
