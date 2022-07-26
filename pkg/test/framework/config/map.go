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

package config

import (
	"fmt"

	"istio.io/istio/pkg/test/scopes"
)

type Map map[string]any

func (m Map) Map(key string) Map {
	nested, ok := m[key]
	if !ok {
		return nil
	}
	out, ok := nested.(Map)
	if !ok {
		return nil
	}
	return out
}

func (m Map) String(key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	str, ok := v.(string)
	if !ok {
		return fmt.Sprint(m[key])
	}
	return str
}

func (m Map) Slice(key string) []Map {
	v, ok := m[key].([]any)
	if !ok {
		return nil
	}
	var out []Map
	for i, imeta := range v {
		meta, ok := toMap(imeta)
		if !ok {
			scopes.Framework.Warnf("failed to parse item %d of %s, defaulting to empty: %v", i, key, imeta)
			return nil
		}
		out = append(out, meta)
	}
	return out
}

func toMap(orig any) (Map, bool) {
	// keys are strings, easily cast
	if cfgMeta, ok := orig.(Map); ok {
		return cfgMeta, true
	}
	// keys are interface{}, manually change to string keys
	mapInterface, ok := orig.(map[any]any)
	if !ok {
		// not a map at all
		return nil, false
	}
	mapString := make(map[string]any)
	for key, value := range mapInterface {
		mapString[fmt.Sprintf("%v", key)] = value
	}
	return mapString, true
}

func (m Map) Bool(key string) *bool {
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

func (m Map) Int(key string) int {
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
