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

package cluster

import (
	"fmt"

	"istio.io/istio/pkg/test/scopes"
)

type Kind string

const (
	Kubernetes Kind = "Kubernetes"
	Fake       Kind = "Fake"
	Aggregate  Kind = "Aggregate"
	StaticVM   Kind = "StaticVM"
	Unknown    Kind = "Unknown"
)

type Config struct {
	Kind               Kind       `yaml:"kind,omitempty"`
	Name               string     `yaml:"clusterName,omitempty"`
	Network            string     `yaml:"network,omitempty"`
	PrimaryClusterName string     `yaml:"primaryClusterName,omitempty"`
	ConfigClusterName  string     `yaml:"configClusterName,omitempty"`
	Meta               ConfigMeta `yaml:"meta,omitempty"`
}

type ConfigMeta map[string]interface{}

func (m ConfigMeta) String(key string) string {
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}

func (m ConfigMeta) Slice(key string) []ConfigMeta {
	v, ok := m[key].([]interface{})
	if !ok {
		scopes.Framework.Warnf("failed to parse key %q as slice, defaulting to empty", key)
		return nil
	}
	var out []ConfigMeta
	for i, imeta := range v {
		meta, ok := toConfigMeta(imeta)
		if !ok {
			scopes.Framework.Warnf("failed to parse item %d of %s, defaulting to empty: %v", i, key, imeta)
			return nil
		}
		out = append(out, meta)
	}
	return out
}

func toConfigMeta(orig interface{}) (ConfigMeta, bool) {
	// keys are strings, easily cast
	if cfgMeta, ok := orig.(ConfigMeta); ok {
		return cfgMeta, true
	}
	// keys are interface{}, manually change to string keys
	mapInterface, ok := orig.(map[interface{}]interface{})
	if !ok {
		// not a map at all
		return nil, false
	}
	mapString := make(map[string]interface{})
	for key, value := range mapInterface {
		mapString[fmt.Sprintf("%v", key)] = value
	}
	return mapString, true
}

func (m ConfigMeta) Bool(key string) *bool {
	if m[key] == nil {
		return nil
	}
	v, ok := m[key].(bool)
	if !ok {
		scopes.Framework.Warnf("failed to parse key of type %T value %q as bool, defaulting to false", m[key], key)
		return nil
	}
	return &v
}

func (m ConfigMeta) Int(key string) int {
	if m[key] == nil {
		return 0
	}
	v, ok := m[key].(int)
	if !ok {
		scopes.Framework.Warnf("failed to parse key of type %T value %q as int, defaulting to 0", m[key], key)
		return 0
	}
	return v
}
