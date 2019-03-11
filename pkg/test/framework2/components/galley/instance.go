//  Copyright 2019 Istio Authors
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

package galley

import (
	"testing"

	"istio.io/istio/pkg/test/framework2/components/environment"

	"istio.io/istio/pkg/test/framework2/components/environment/native"
	"istio.io/istio/pkg/test/framework2/resource"
)

// Instance of Galley
type Instance interface {
	// Address of the Galley MCP Server.
	Address() string

	// ApplyConfig applies the given config yaml file via Galley.
	ApplyConfig(yamlText string) error

	// ApplyConfigOrFail applies the given config yaml file via Galley.
	ApplyConfigOrFail(t *testing.T, yamlText string)

	// ApplyConfigDir recursively applies all the config files in the specified directory
	ApplyConfigDir(configDir string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// SetMeshConfig applies the given mesh config yaml file via Galley.
	SetMeshConfig(yamlText string) error

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error
}

// New returns a new Galley instance.
func New(c resource.Context) (Instance, error) {
	switch c.Environment().EnvironmentName() {
	case environment.Native:
		return newNative(c, c.Environment().(*native.Environment))
	default:
		return nil, environment.UnsupportedEnvironment(c.Environment().EnvironmentName())
	}
}

// NewOrFail returns a new Galley instance, or fails.
func NewOrFail(t *testing.T, c resource.Context) Instance {
	i, err := New(c)
	if err != nil {
		t.Fatalf("Error creating Galley: %v", err)
	}
	return i
}
