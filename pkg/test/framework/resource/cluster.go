//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package resource

import (
	"fmt"

	"istio.io/istio/pkg/test"
)

// ClusterIndex is the index of a cluster within the Environment
type ClusterIndex int

type ConfigManager interface {
	// ApplyConfig applies the given config yaml text via Galley.
	ApplyConfig(ns string, yamlText ...string) error

	// ApplyConfigOrFail applies the given config yaml text via Galley.
	ApplyConfigOrFail(t test.Failer, ns string, yamlText ...string)

	// DeleteConfig deletes the given config yaml text via Galley.
	DeleteConfig(ns string, yamlText ...string) error

	// DeleteConfigOrFail deletes the given config yaml text via Galley.
	DeleteConfigOrFail(t test.Failer, ns string, yamlText ...string)

	// ApplyConfigDir recursively applies all the config files in the specified directory
	ApplyConfigDir(ns string, configDir string) error

	// DeleteConfigDir recursively deletes all the config files in the specified directory
	DeleteConfigDir(ns string, configDir string) error
}

// Cluster in a multicluster environment.
type Cluster interface {
	fmt.Stringer
	ConfigManager

	// Index of this Cluster within the Environment
	Index() ClusterIndex
}
