//  Copyright 2018 Istio Authors
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

package components

import (
	"testing"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

// Galley represents a deployed Galley instance.
type Galley interface {
	component.Instance

	// ApplyConfig applies the given config yaml file via Galley.
	ApplyConfig(yamlText string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// SetMeshConfig applies the given mesh config yaml file via Galley.
	SetMeshConfig(yamlText string) error

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error
}

// GetGalley from the repository
func GetGalley(e component.Repository, t testing.TB) Galley {
	t.Helper()
	return e.GetComponentOrFail(ids.Galley, t).(Galley)
}
