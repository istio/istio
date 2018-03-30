// Copyright 2018 Istio Authors.
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

// Package registry represents a registry of plugins that can be used by a config generator.
//
// This lives in a subpackage and not in package `plugin` itself to avoid cyclic dependencies.
package registry

import (
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
)

// NewPlugins returns a list of plugin instance handles. Each plugin implements the plugin.Callbacks interfaces
func NewPlugins() []plugin.Callbacks {
	plugins := make([]plugin.Callbacks, 0)
	plugins = append(plugins, authn.NewPlugin())
	// plugins = append(plugins, mixer.NewPlugin())
	// plugins = append(plugins, apim.NewPlugin())
	return plugins
}
