// Copyright 2018 Istio Authors
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
	"istio.io/istio/pilot/pkg/networking/plugin/authz"
	"istio.io/istio/pilot/pkg/networking/plugin/health"
	"istio.io/istio/pilot/pkg/networking/plugin/mixer"
)

var availablePlugins = map[string]plugin.Plugin{
	plugin.Authn:  authn.NewPlugin(),
	plugin.Authz:  authz.NewPlugin(),
	plugin.Health: health.NewPlugin(),
	plugin.Mixer:  mixer.NewPlugin(),
}

// NewPlugins returns a slice of default Plugins.
func NewPlugins(in []string) []plugin.Plugin {
	var plugins []plugin.Plugin
	for _, pl := range in {
		if p, exist := availablePlugins[pl]; exist {
			plugins = append(plugins, p)
		}
	}
	return plugins
}
