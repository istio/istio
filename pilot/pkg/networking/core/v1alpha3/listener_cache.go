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

package v1alpha3

import (
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/hash"
)

// ListenerCache includes the variables that can influence a Listener Configuration.
// Implements XdsCacheEntry interface.
type ListenerCache struct {
	ListenerName    string
	Gateways        []*config.Config
	EnvoyFilterKeys []string
	WasmPlugins     []*config.Config
}

func (l *ListenerCache) Type() string {
	return model.LDSType
}

func (l *ListenerCache) Key() any {
	// nolint: gosec
	// Not security sensitive code
	h := hash.New()
	h.Write([]byte(l.ListenerName))
	h.Write(Separator)

	for _, gw := range l.Gateways {
		h.Write([]byte(gw.Name))
		h.Write(Slash)
		h.Write([]byte(gw.Namespace))
		h.Write(Separator)
	}
	h.Write(Separator)

	for _, efk := range l.EnvoyFilterKeys {
		h.Write([]byte(efk))
		h.Write(Separator)
	}
	h.Write(Separator)

	for _, plugin := range l.WasmPlugins {
		h.Write([]byte(plugin.Name))
		h.Write(Slash)
		h.Write([]byte(plugin.Namespace))
		h.Write(Separator)
	}
	h.Write(Separator)

	return h.Sum64()
}

func (l *ListenerCache) DependentConfigs() []model.ConfigHash {
	configs := make([]model.ConfigHash, 0)
	for _, gw := range l.Gateways {
		configs = append(configs, model.ConfigKey{Kind: kind.Gateway, Name: gw.Name, Namespace: gw.Namespace}.HashCode())
	}

	for _, efKey := range l.EnvoyFilterKeys {
		items := strings.Split(efKey, "/")
		configs = append(configs, model.ConfigKey{Kind: kind.EnvoyFilter, Name: items[1], Namespace: items[0]}.HashCode())
	}

	for _, plugin := range l.WasmPlugins {
		configs = append(configs, model.ConfigKey{Kind: kind.WasmPlugin, Name: plugin.Name, Namespace: plugin.Namespace}.HashCode())
	}
	return configs
}

func (l *ListenerCache) Cacheable() bool {
	return true
}
